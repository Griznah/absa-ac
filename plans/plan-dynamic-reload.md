# Dynamic Config Reload for AC Discord Bot

## Overview

This plan adds dynamic configuration reloading to the AC Discord bot, allowing config.json changes to be applied without restarting the bot. The approach uses file polling with a lightweight ConfigManager wrapper, balancing simplicity, maintainability, and feature completeness. Config modifications are detected via file modification time checks before each status update cycle, enabling near-real-time updates without external dependencies or complex event handling.

## Planning Context

This section is consumed VERBATIM by downstream agents (Technical Writer, Quality Reviewer). Quality matters: vague entries here produce poor annotations and missed risks.

### Decision Log

| Decision | Reasoning Chain |
| --- | --- |
| **Polling over fsnotify** | Bot has 30-second update loop -> adding file mtime check leverages existing cycle without extra goroutine -> fsnotify adds external dependency and ~150 LOC for same functional outcome -> polling keeps single-file architecture simple |
| **5-second poll interval for mtime checks** | Status updates occur every 30 seconds -> checking mtime on every update would miss changes between cycles -> 5-second interval detects changes within next update cycle without constant filesystem access -> balances responsiveness vs overhead |
| **sync.RWMutex for config access** | Config reads occur frequently (every server query) -> writes occur rarely (only on config reload) -> RWMutex allows concurrent reads without blocking -> multiple goroutines can access config simultaneously during server polling |
| **atomic.Value for config storage** | Zero-copy reads during hot path (server polling) -> mutex-based reads would add contention during concurrent fetches -> atomic.Value provides lock-free reads while maintaining thread-safe writes -> aligns with existing RWMutex pattern in bot for message state |
| **Keep old config on validation failure** | Invalid config file (syntax error, bad ports, etc.) -> bot must continue operating with last known good config -> prevents single typo from breaking production deployment -> admin can fix config and retry without bot downtime |
| **Read-only config access** | User confirms read-only requirement -> bot detects changes but never writes config -> simpler implementation (no file locking or write coordination) -> architecture supports adding write capability later without breaking changes |
| **Reload before status message updates** | Config changes affect server list, categories, embed layout -> reloading before message update ensures new config reflected immediately -> prevents race where outdated config used for current update cycle |
| **Debounce window of 100ms for rapid writes** | Text editors create multiple write events during save -> rapid reloads would waste CPU and could hit race conditions -> 100ms delay batches writes within editing window -> still provides near-instant reload from admin perspective |

### Rejected Alternatives

| Alternative | Why Rejected |
| --- | --- |
| **fsnotify event-based watching** | Adds external dependency (github.com/fsnotify/fsnotify) -> increases binary size and complexity -> event-driven approach harder to debug -> polling sufficient for 30-second update cycle |
| **Inotify directly via syscalls** | Platform-specific (Linux only) -> would break macOS/Windows compatibility -> higher complexity than polling -> no performance benefit for this use case |
| **Config versioning with rollback** | Overkill for single-file bot -> adds state management complexity -> no identified need for historical configs -> can be added later if requested |
| **Signal-based reload (SIGHUP)** | Traditional Unix approach but less discoverable -> file watching more intuitive for admins -> signals don't work well in containers without extra setup -> polling works everywhere without configuration |
| **Full ConfigService with staging** | Bot is intentionally monolithic for simplicity -> service layer would add abstraction without clear benefit -> staging/rollback adds complexity beyond requirements |
| **Shared memory or config server** | External dependencies break single-file design -> network-based config adds failure modes -> local file is authoritative and simple |
| **Field-by-field hot reload** | Complex merge logic for partial config updates -> atomic config swap is simpler and safer -> partial updates could lead to inconsistent state |

### Constraints & Assumptions

**Technical:**
- Go 1.25.5 (pinned in go.mod)
- Single-file architecture (main.go) must be preserved
- Existing sync.RWMutex pattern for thread-safe state (Bot.messageMutex)
- 30-second status update cycle (UPDATE_INTERVAL from config)
- No external dependencies beyond current go.mod
- Config.json is local file (not network mount or database)
- Existing graceful shutdown via signal.Notify (SIGINT/SIGTERM) in Bot.WaitForShutdown()

**Codebase State:**
- Project is Go-based (main.go, main_test.go) - CLAUDE.md is outdated (describes old Python bot)
- Existing loadConfig(string) (*Config, error) function with fallback paths
- Existing validateConfigStruct(*Config) uses log.Fatalf (terminates on error)
- Bot exits on startup if config missing (log.Fatalf at main.go:613)
- Shutdown signal handling exists at main.go:559-571

**Organizational:**
- Bot is intentionally monolithic for deployment simplicity
- Low maintenance overhead is priority over feature richness
- Code should be accessible to contributors with basic Go knowledge
- Changes must not break existing deployments

**Dependencies:**
- Standard library only (os, time, sync, encoding/json)
- No new external packages
- Existing discordgo v0.29.0 for Discord API

**Default conventions applied:**
- Testing: Example-based unit tests (user-specified) | Integration tests with real dependencies (user-specified) | E2E with generated datasets (user-specified)
- File organization: Extend existing files; create new only when >300 lines or distinct responsibility (default-conventions domain='file-creation')

### Known Risks

| Risk | Mitigation | Anchor |
| --- | --- | --- |
| **Config file modified during bot read** | Config reload reads entire file into memory before parsing -> atomic.Value swap ensures readers get consistent snapshot -> old config remains valid until new config fully loaded | main.go:loadConfig() - existing atomic load pattern |
| **Invalid config breaks bot** | validateConfigStructSafe() called before applying new config -> validation failure logs error and keeps old config -> bot continues operating with last known good state | main_test.go:validateConfigStructSafe() - returns error, doesn't terminate |
| **Race condition during config swap** | atomic.Value.Store() provides atomic swap -> readers get either old or new config, never partially updated -> RWMutex protects write path to ensure single updater | sync/atomic documentation - atomic.Value guarantees |
| **File system latency on network mounts** | Polling interval (5 seconds) tolerant of FS delays -> file mtime checks are cheap syscalls -> validation failures don't crash bot -> os.ReadFile blocks but timeout is 2s (httpClient) | main.go:234-235 - existing timeout pattern |
| **Multiple bot instances reading same config** | Read-only access eliminates write coordination -> each instance detects changes independently -> no file locking required for reads | User requirement: read-only |
| **Config file deleted while running** | File check returns error -> logged but bot continues with old config -> admin can recreate file without restart -> on bot restart, exits if config still missing (existing behavior at main.go:613) | main.go:613 - log.Fatalf on missing config at startup |
| **Debounce timer not stopped on shutdown** | ConfigManager must add Cleanup() method -> called from Bot.WaitForShutdown() before session.Close() -> stops timer to prevent goroutine leak during shutdown | main.go:559-571 - existing WaitForShutdown() pattern |

## Invisible Knowledge

This section captures knowledge NOT deducible from reading the code alone. Technical Writer uses this to create README.md files **in the same directory as the affected code** during post-implementation.

### Architecture

```
Update Cycle (every 30s):
  Check config mtime --> Changed? --> Load & Validate --> Atomic swap --> Use new config
                                                        |
                                                        v
                                                 Keep old config
                                                        |
                                                        v
                                      Continue with current config
```

**Component relationships:**
- `ConfigManager` wraps `Config` struct and provides thread-safe access
- `checkAndReloadIfNeeded()` called before each status update cycle
- `atomic.Value` stores config pointer for lock-free reads
- `sync.RWMutex` protects reload operations
- Bot's existing update loop calls `GetConfig()` before fetching server info

### Data Flow

```
Config Update Flow:
  File change on disk --> mtime check in update loop --> loadConfig() reads file
                                                            |
                                                            v
                                        validateConfigStructSafe() validates
                                                            |
                                                            v
                                      Valid? --> atomic.Value.Store(new config)
                       |                                |
                       v                                v
                  Log error                       Next update uses new config
                  Keep old config
```

### Existing Behavior

**loadConfig function (main.go:128-177):**
- Signature: `func loadConfig(providedPath string) (*Config, error)`
- Provided path mode: tries only that path, returns error on failure
- Fallback mode: tries `/data/config.json`, then `./config.json` in working directory
- Returns parsed Config struct or error
- Uses `os.ReadFile` for file access (blocks on network mounts)
- Uses `json.Unmarshal` for parsing (returns syntax errors with position)

**validateConfigStruct function (main.go:180-230):**
- Signature: `func validateConfigStruct(cfg *Config)`
- Uses `log.Fatalf` for ALL validation failures (terminates process)
- Validates: ServerIP non-empty, UpdateInterval >= 1, CategoryOrder non-empty
- Validates: All categories in CategoryOrder have emoji in CategoryEmojis
- Validates: Server name/port/category, port range 1-65535
- **Critical**: Not safe for runtime reload - must use validateConfigStructSafeRuntime instead

**validateConfigStructSafeRuntime (main.go:XXX):**
- Signature: `func validateConfigStructSafeRuntime(cfg *Config) error`
- Returns error instead of calling log.Fatalf
- Safe for runtime validation (doesn't terminate bot)
- Same validation rules as validateConfigStruct
- Moved from main_test.go to main.go for runtime use
- **Must be used in ConfigManager.performReload()**

**Shutdown handling (main.go:559-571):**
- Bot.WaitForShutdown() blocks on SIGINT/SIGTERM/os.Interrupt
- Calls session.Close() on Discord connection
- No other cleanup logic exists
- ConfigManager must add Cleanup() method for debounce timer

**Configuration lifecycle:**
1. Bot starts: Load config from file, validate, store in atomic.Value
2. Each update cycle: Check file mtime, reload if changed
3. Validation failure: Log error, continue with old config
4. Validation success: Atomically swap config, next cycle uses new values

### Why This Structure

**Single-file preservation:**
- Bot is intentionally monolithic for deployment simplicity
- ConfigManager embedded in main.go rather than separate package
- All config logic co-located for easy comprehension
- No package boundaries to navigate during maintenance

**Polling vs event-driven:**
- Bot already has periodic update loop (30 seconds)
- Polling integrates naturally into existing cycle
- Event-driven would require separate goroutine and coordination
- Simplicity prioritized over minimal latency (5-second poll is sufficient)

**Thread-safety strategy:**
- Read-heavy workload (config accessed on every server query)
- Write-light workload (config changes rarely)
- RWMutex allows concurrent reads during server polling
- atomic.Value provides zero-copy reads in hot path

### Invariants

**Config consistency:**
- All config reads see a complete, valid config (never partial state)
- atomic.Value ensures atomic swap between old and new config
- Validation runs before swap, never after

**File watching bounds:**
- Config mtime checked every 5 seconds maximum
- No more than one reload attempt per 5-second window
- Failed reloads don't affect running bot

**Error recovery:**
- Invalid config never replaces valid config
- Bot continues operating on last known good config
- All errors logged but never crash the bot

### Tradeoffs

**Latency vs simplicity:**
- 5-second poll interval means max 5-second delay before detecting changes
- Eliminates fsnotify dependency and ~150 LOC of event handling code
- Acceptable for admin-triggered config changes (not time-critical)

**Memory vs performance:**
- Full config duplicated in memory during reload (old + new)
- Atomic swap requires separate config instances
- Config is small (~1KB), memory cost negligible
- Avoids complex partial update logic

**Read-only vs read-write:**
- Read-only access eliminates file locking complexity
- Bot cannot persist config changes from runtime
- Architecture supports adding write capability later
- Simpler implementation meets current requirements

### Operational Procedures

**Detecting Config Reload Failures:**

When a config file change is not applied, check the bot logs for these patterns:

```
config validation failed: <error details>
failed to reload config: <error reason>
```

**Verification steps:**
1. Check log for "config reloaded successfully" after file modification
2. Verify bot behavior reflects new config (servers appear/disappear in embed)
3. If no success log within 30 seconds, check for error messages above

**Common failures:**
- JSON syntax error: "failed to parse config"
- Missing field: "server_ip cannot be empty"
- Invalid port: "invalid port: 70000 (valid range: 1-65535)"
- Unknown category: "category 'X' which is not defined in category_order"

**Recovery procedure:**
1. Fix the config file error (use `jq . config.json` to validate JSON syntax)
2. Wait for next update cycle (max 30 seconds)
3. Verify log shows "config reloaded successfully"
4. Check Discord embed reflects new configuration

**No health check endpoint:** Monitor logs to verify config status. Bot does not expose HTTP endpoints for config health checks.

## Milestones

### Milestone 1: Add ConfigManager Struct and Basic Thread-Safe Access

**Files**:
- `main.go`

**Flags**:

| Flag | Consumer | Effect |
| --- | --- | --- |
| `conformance` | QR | Extra scrutiny on consistency with existing patterns |
| `needs-rationale` | TW | Add WHY comments for thread-safety decisions |

**Requirements**:

- Add `ConfigManager` struct after `Config` struct definition
- Move `validateConfigStructSafe` from `main_test.go` to `main.go` (renamed to `validateConfigStructSafeRuntime`)
- Implement `NewConfigManager(configPath string, initial *Config) *ConfigManager` constructor
- Implement `GetConfig() *Config` method using `atomic.Value.Load()`
- Implement `getLastModTime() time.Time` helper for file mtime checks
- Add `lastModTime` field to track file modification time
- Store initial config in `atomic.Value` during construction

**Acceptance Criteria**:

- ConfigManager struct exists with atomic.Value, configPath, lastModTime, mu fields
- NewConfigManager creates instance and stores initial config
- GetConfig returns non-nil Config pointer
- Concurrent goroutines can call GetConfig without blocking (reads are lock-free)
- lastModTime accurately reflects config file's modification time

**Tests**:

- **Test files**: `main_test.go`
- **Test type**: unit
- **Backing**: user-specified (example-based)
- **Scenarios**:
  - Normal: ConfigManager created with valid config stores and retrieves config
  - Edge: GetConfig called from 100 goroutines concurrently (no deadlock or panic)
  - Error: ConfigManager created with nil config (panics or returns error)

**Code Intent**:

Add ConfigManager struct after the existing Config and Server structs:

```go
type ConfigManager struct {
    config      atomic.Value // stores *Config
    configPath  string
    lastModTime time.Time
    mu          sync.RWMutex
}
```

Move validateConfigStructSafe from main_test.go to main.go (renamed to validateConfigStructSafeRuntime to distinguish it). This function provides runtime validation during config reload.

Constructor accepts config path and initial Config, stores in atomic.Value, and records initial file modification time. GetConfig() method loads and returns Config pointer. getLastModTime() helper uses os.Stat to get file modification time. All struct fields are exported for use by other parts of bot. Initial config passed to constructor represents the validated config loaded at bot startup.

**Code Changes** (filled by Developer agent):

```diff
--- a/main.go
+++ b/main.go
@@ -123,6 +123,95 @@ type Server struct {
 	Category string
 }

+// ConfigManager provides thread-safe access to configuration with dynamic reload
+type ConfigManager struct {
+	config      atomic.Value // stores *Config
+	configPath  string
+	lastModTime time.Time
+	mu          sync.RWMutex
+}
+
+// NewConfigManager creates a new ConfigManager with an initial configuration
+func NewConfigManager(configPath string, initial *Config) *ConfigManager {
+	cm := &ConfigManager{
+		configPath: configPath,
+	}
+	cm.config.Store(initial)
+
+	// Get initial file modification time
+	if modTime, err := cm.getLastModTime(); err == nil {
+		cm.lastModTime = modTime
+	} else {
+		log.Printf("Warning: failed to get initial config mod time: %v", err)
+	}
+
+	return cm
+}
+
+// GetConfig returns the current configuration (thread-safe, lock-free read)
+func (cm *ConfigManager) GetConfig() *Config {
+	return cm.config.Load().(*Config)
+}
+
+// getLastModTime retrieves the modification time of the config file (changes indicate config modifications requiring reload)
+func (cm *ConfigManager) getLastModTime() (time.Time, error) {
+	info, err := os.Stat(cm.configPath)
+	if err != nil {
+		return time.Time{}, err
+	}
+	return info.ModTime(), nil
+}
+
+// validateConfigStructSafeRuntime is a non-fatal version of validateConfigStruct for runtime reload
+// Returns error instead of calling log.Fatalf, allowing bot to continue with old config on validation failure
+func validateConfigStructSafeRuntime(cfg *Config) error {
+	if cfg.ServerIP == "" {
+		return fmt.Errorf("server_ip cannot be empty")
+	}
+
+	if cfg.UpdateInterval < 1 {
+		return fmt.Errorf("update_interval must be at least 1 second (got: %d)", cfg.UpdateInterval)
+	}
+
+	if len(cfg.CategoryOrder) == 0 {
+		return fmt.Errorf("category_order cannot be empty")
+	}
+
+	categoryMap := make(map[string]bool)
+	for _, cat := range cfg.CategoryOrder {
+		categoryMap[cat] = true
+	}
+
+	for _, cat := range cfg.CategoryOrder {
+		if _, exists := cfg.CategoryEmojis[cat]; !exists {
+			return fmt.Errorf("category '%s' is in category_order but missing from category_emojis", cat)
+		}
+	}
+
+	for i, server := range cfg.Servers {
+		if server.Name == "" {
+			return fmt.Errorf("server at index %d has empty name", i)
+		}
+
+		if server.Port < 1 || server.Port > 65535 {
+			return fmt.Errorf("server '%s' has invalid port: %d", server.Name, server.Port)
+		}
+
+		if server.Category == "" {
+			return fmt.Errorf("server '%s' has empty category", server.Name)
+		}
+
+		if !categoryMap[server.Category] {
+			return fmt.Errorf("server '%s' has category '%s' which is not defined in category_order", server.Name, server.Category)
+		}
+	}
+
+	return nil
+}
+
 // ================= TYPES =================

 type ServerInfo struct {

---

### Milestone 2: Implement Config Reload Logic with Validation

**Files**:
- `main.go`

**Flags**:

| Flag | Consumer | Effect |
| --- | --- | --- |
| `error-handling` | QR | Extra RULE 0 scrutiny on validation error paths |
| `conformance` | QR | Ensure consistency with existing loadConfig/validate patterns |

**Requirements**:

- Implement `checkAndReloadIfNeeded() error` method in ConfigManager (renamed to clarify conditional behavior)
- Load config file using existing `loadConfig()` function
- Validate using `validateConfigStructSafeRuntime()` function (moved from main_test.go in M1)
- On validation success: update atomic.Value and lastModTime, return nil
- On validation failure: log error, keep old config, return error
- On file not found: log error, keep old config, return error
- On file read error: log error, keep old config, return error

**Acceptance Criteria**:

- checkAndReloadIfNeeded returns nil when config unchanged (mtime matches)
- checkAndReloadIfNeeded loads and applies new config when file modified
- checkAndReloadIfNeeded keeps old config when new config invalid
- checkAndReloadIfNeeded keeps old config when file missing or unreadable
- All error paths logged with descriptive messages
- Concurrent calls to checkAndReloadIfNeeded are serialized by mutex

**Tests**:

- **Test files**: `main_test.go`
- **Test type**: unit
- **Backing**: user-specified (example-based)
- **Scenarios**:
  - Normal: Config file modified with valid content -> new config applied
  - Edge: Config file modified rapidly -> debounce prevents excessive reloads
  - Error: Config file modified with invalid JSON -> old config kept, error returned
  - Error: Config file deleted -> old config kept, error returned
  - Concurrent: Multiple goroutines call checkAndReloadIfNeeded -> only one performs reload

**Code Intent**:

Add checkAndReloadIfNeeded() method to ConfigManager. Method signature: `func (cm *ConfigManager) checkAndReloadIfNeeded() error`. Implementation steps:

1. Lock mutex for write access (ensures single updater)
2. Get current file mtime using os.Stat on configPath
3. If mtime equals lastModTime, unlock and return nil (no change)
4. Otherwise, call loadConfig(configPath) to read new config
5. Call validateConfigStructSafeRuntime() on new config
6. If validation fails: log error, unlock mutex, return error (keep old config)
7. If validation succeeds: store new config in atomic.Value, update lastModTime, unlock, return nil

Error messages must be descriptive (include file path, validation error details). Use log.Printf for all errors. Follow existing error handling patterns from loadConfig function.

**Code Changes** (filled by Developer agent):

```diff
--- a/main.go
+++ b/main.go
@@ -437,6 +437,50 @@ func (cm *ConfigManager) getLastModTime() (time.Time, error) {
 	return info.ModTime(), nil
 }

+// checkAndReloadIfNeeded checks if the config file has changed and reloads if necessary
+func (cm *ConfigManager) checkAndReloadIfNeeded() error {
+	cm.mu.Lock()
+	defer cm.mu.Unlock()
+
+	// Check current file modification time
+	currentModTime, err := cm.getLastModTime()
+	if err != nil {
+		return fmt.Errorf("failed to stat config file: %w", err)
+	}
+
+	// No change detected
+	if currentModTime.Equal(cm.lastModTime) || currentModTime.Before(cm.lastModTime) {
+		return nil
+	}
+
+	// File has changed, attempt reload
+	log.Printf("Config file modified, attempting reload from: %s", cm.configPath)
+
+	newCfg, err := loadConfig(cm.configPath)
+	if err != nil {
+		return fmt.Errorf("failed to read config: %w", err)
+	}
+
+	// Validate new config
+	if err := validateConfigStructSafeRuntime(newCfg); err != nil {
+		return fmt.Errorf("config validation failed: %w", err)
+	}
+
+	// Success: atomically swap config and update mod time
+	cm.config.Store(newCfg)
+	cm.lastModTime = currentModTime
+	log.Println("Config reloaded successfully")
+
+	return nil
+}
+
 // ================= TYPES =================

 type ServerInfo struct {

---

### Milestone 3: Add Polling Loop and Bot Integration

**Files**:
- `main.go`

**Flags**:

| Flag | Consumer | Effect |
| --- | --- | --- |
| `conformance` | QR | Ensure integration with existing update loop pattern |
| `performance` | QR | Check that polling doesn't block update cycle |

**Requirements**:

- Add `checkForConfigUpdates()` call at start of existing update loop
- Call checkAndReloadIfNeeded() before fetching server info
- If reload fails, log error but continue update cycle
- Replace global config variables with ConfigManager access
- Update all config access points to use `configManager.GetConfig()`
- Initialize ConfigManager in main() after initial config load
- Pass ConfigManager reference to functions that need config

**Acceptance Criteria**:

- Config reload checked on every update cycle (every 30 seconds)
- Config changes applied immediately (within next update cycle after file change)
- Failed reloads don't prevent status message updates
- All code paths read config from ConfigManager, not global variables
- Bot startup unchanged (loads config, validates, starts update loop)

**Tests**:

- **Test files**: `main_test.go`
- **Test type**: integration
- **Backing**: user-specified (real dependencies)
- **Scenarios**:
  - Normal: Modify config file while bot running -> new config used in next update
  - Edge: Multiple rapid config changes -> all changes eventually applied
  - Error: Invalid config written -> bot continues with old config, recovers when valid config restored
  - Concurrent: Config modified during server polling -> no race or panic

**Code Intent**:

Locate the existing update loop function (likely a background task or goroutine). Add checkForConfigUpdates call at the beginning of the update cycle, before server info fetching. Create checkForConfigUpdates wrapper that calls configManager.checkAndReloadIfNeeded() and logs any errors but doesn't stop the update cycle.

Find all locations where config fields are accessed directly (e.g., cfg.ServerIP, cfg.Servers). Replace with configManager.GetConfig() to retrieve current config pointer before accessing fields. This includes:
- Server iteration loops
- Embed building functions
- Update interval usage

In main() function, after initial config loading and validation, create ConfigManager: `configManager := NewConfigManager(configPath, cfg)`. Pass configManager to any functions that need config access (likely the update loop function).

Remove any global config variables if they exist. Config is only accessed through ConfigManager. Update function signatures to accept *ConfigManager instead of *Config where needed.

**Code Changes** (filled by Developer agent):

```diff
--- a/main.go
+++ b/main.go
@@ -108,13 +108,13 @@ type ServerInfo struct {

 type Bot struct {
 	session       *discordgo.Session
 	channelID     string
-	config        *Config
+	configManager *ConfigManager
 	serverMessage *discordgo.Message
 	messageMutex  sync.RWMutex
 }

 // Config holds application configuration loaded from config.json
 type Config struct {
@@ -236,7 +236,7 @@ var httpClient = &http.Client{
 	Timeout: 2 * time.Second,
 }

-func fetchAllServers(cfg *Config) []ServerInfo {
+func fetchAllServers(cfgManager *ConfigManager) []ServerInfo {
+	cfg := cfgManager.GetConfig()
 	var wg sync.WaitGroup
 	infos := make([]ServerInfo, len(cfg.Servers))
 	mu := sync.Mutex{}
@@ -318,7 +318,9 @@ func offlineServerInfo(server Server) ServerInfo {
 // ================= DISCORD INTEGRATION =================

-func buildEmbed(infos []ServerInfo, cfg *Config) *discordgo.MessageEmbed {
+func buildEmbed(infos []ServerInfo, cfgManager *ConfigManager) *discordgo.MessageEmbed {
+	cfg := cfgManager.GetConfig()

 	// Group servers and calculate totals
 	grouped := make(map[string][]ServerInfo)
 	categoryTotals := make(map[string]int
@@ -492,9 +494,13 @@ func (b *Bot) startUpdateLoop() {
 	for range ticker.C {
+		// Check for config updates before each update
+		if err := b.checkForConfigUpdates(); err != nil {
+			log.Printf("Config reload check failed: %v", err)
+		}
 		b.performUpdate()
 	}
 }

 func (b *Bot) performUpdate() {
-	infos := fetchAllServers(b.config)
-	embed := buildEmbed(infos, b.config)
+	infos := fetchAllServers(b.configManager)
+	embed := buildEmbed(infos, b.configManager)

 	// Send updated embed to Discord
 	if err := b.updateStatusMessage(embed); err != nil {
@@ -530,12 +538,18 @@ func createDiscordSession(token string) (*discordgo.Session, error) {
 	return session, nil
 }

-func NewBot(cfg *Config, token, channelID string) (*Bot, error) {
+func NewBot(cfgManager *ConfigManager, token, channelID string) (*Bot, error) {
 	if token == "" {
 		return nil, fmt.Errorf("DISCORD_TOKEN environment variable not set")
 	}
 	if channelID == "" {
 		return nil, fmt.Errorf("CHANNEL_ID environment variable not set")
 	}
@@ -543,7 +557,7 @@ func NewBot(cfg *Config, token, channelID string) (*Bot, error) {
 	session, err := createDiscordSession(token)
 	if err != nil {
 		return nil, err
 	}
@@ -551,7 +565,7 @@ func NewBot(cfg *Config, token, channelID string) (*Bot, error) {
 	return &Bot{
 		session:   session,
 		channelID: channelID,
-		config:    cfg,
+		configManager: cfgManager,
 	}, nil
 }

@@ -587,6 +601,13 @@ func (b *Bot) WaitForShutdown() {
 	log.Println("Shutdown complete")
 }

+// checkForConfigUpdates wraps checkAndReloadIfNeeded for use in update loop
+func (b *Bot) checkForConfigUpdates() error {
+	if b.configManager == nil {
+		return nil
+	}
+	return b.configManager.checkAndReloadIfNeeded()
+}
+
 // ================= MAIN

 func validateConfig() (token, channelID string, err error) {
@@ -615,9 +636,9 @@ func main() {
 	// Set server IPs from config
 	for i := range cfg.Servers {
 		cfg.Servers[i].IP = cfg.ServerIP
 	}

-	bot, err := NewBot(cfg, token, channelID)
+	// Create config manager with initial config
+	configManager := NewConfigManager(getConfigPath(*configPath), cfg)
+	bot, err := NewBot(configManager, token, channelID)
 	if err != nil {
 		log.Fatalf("Failed to create bot: %v", err)
 	}
```

Note: This diff requires adding a helper function `getConfigPath()` to determine the actual config path used (needed for ConfigManager). Add this helper to main.go:

```diff
--- a/main.go
+++ b/main.go
@@ -176,6 +176,39 @@ func loadConfig(providedPath string) (*Config, error) {
 	return nil, fmt.Errorf("failed to load config from any default location:\n%s", strings.Join(errors, "\n"))
 }

+// getConfigPath determines the actual config file path that loadConfig uses
+// Matches loadConfig's fallback logic exactly: provided path -> /data/config.json -> ./config.json
+func getConfigPath(providedPath string) string {
+	// If explicitly provided, return that path (matches loadConfig's provided-path mode)
+	if providedPath != "" {
+		return providedPath
+	}
+
+	// Otherwise, try default locations in same priority order as loadConfig's fallback mode
+	wd, err := os.Getwd()
+	if err != nil {
+		// If we can't get working directory, config load fails
+		// Return empty string to signal error condition
+		return ""
+	}
+
+	defaultPaths := []string{
+		"/data/config.json",
+		filepath.Join(wd, "config.json"),
+	}
+
+	// Return first existing path (matches loadConfig's fallback priority order)
+	for _, path := range defaultPaths {
+		if _, err := os.Stat(path); err == nil {
+			return path
+		}
+	}
+
+	// No config file found - this matches loadConfig's error return when all paths fail
+	// Empty string signals that no valid config path exists
+	return ""
+}
+
 // validateConfigStruct performs fail-fast validation on loaded config
 func validateConfigStruct(cfg *Config) {

---

### Milestone 4: Add Debouncing for Rapid File Writes

**Files**:
- `main.go`

**Flags**:

| Flag | Consumer | Effect |
| --- | --- | --- |
| `needs-rationale` | TW | Document why debouncing needed (editor behavior) |
| `complex-algorithm` | TW | Add Tier 5 block for debounce logic if non-obvious |

**Requirements**:

- Add debounce timer to ConfigManager struct
- Implement scheduleReload() method that debounces reload attempts
- Modify checkAndReloadIfNeeded to call scheduleReload instead of immediately reloading
- Add performReload() method that executes the actual config reload
- Reset timer on each file modification event
- Wait 100ms after last write before attempting reload
- Prevent excessive reload attempts during file editing
- Add Cleanup() method to stop timer during bot shutdown
- Integrate Cleanup() call into Bot.WaitForShutdown()

**Acceptance Criteria**:

- Rapid file writes (e.g., 5 writes in 50ms) trigger single reload
- Debounce delay is 100ms after last write
- Timer properly reset on subsequent writes
- No race conditions in timer management
- Debounce doesn't prevent reload of valid config
- Cleanup() method stops timer without panicking
- Bot.WaitForShutdown() calls ConfigManager.Cleanup() before session.Close()

**Tests**:

- **Test files**: `main_test.go`
- **Test type**: unit
- **Backing**: user-specified (example-based)
- **Scenarios**:
  - Normal: Single file write -> reload after 100ms debounce
  - Edge: 10 rapid writes in 50ms -> single reload after last write + 100ms
  - Edge: Write, wait 200ms, write again -> two separate reloads
  - Concurrent: Concurrent writes during debounce -> only one reload scheduled
  - Shutdown: Cleanup() called with active timer -> timer stopped, no panic

**Code Intent**:

Add `debounceTimer *time.Timer` field to ConfigManager struct. Implement scheduleReload() method that schedules debounced reload:

```go
func (cm *ConfigManager) scheduleReload() {
    cm.mu.Lock()
    defer cm.mu.Unlock()

    if cm.debounceTimer != nil {
        cm.debounceTimer.Stop()
    }

    cm.debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
        if err := cm.performReload(); err != nil {
            log.Printf("Failed to reload config: %v", err)
        }
    })
}
```

Add Cleanup() method to ConfigManager:

```go
func (cm *ConfigManager) Cleanup() {
    cm.mu.Lock()
    defer cm.mu.Unlock()

    if cm.debounceTimer != nil {
        cm.debounceTimer.Stop()
        cm.debounceTimer = nil
    }
}
```

Modify checkAndReloadIfNeeded to call scheduleReload instead of immediately reloading. When file mtime changes, call scheduleReload() instead of performing reload directly. This batches rapid writes.

In Bot.WaitForShutdown() (main.go:559-571), add ConfigManager cleanup before session.Close():

```go
func (b *Bot) WaitForShutdown() {
    sigchan := make(chan os.Signal, 1)
    signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

    <-sigchan
    log.Println("Shutting down...")

    // Cleanup config manager timer
    if b.configManager != nil {
        b.configManager.Cleanup()
    }

    if err := b.session.Close(); err != nil {
        log.Printf("Error closing Discord session: %v", err)
    }

    log.Println("Shutdown complete")
}
```

Bot struct includes `configManager *ConfigManager` field.

**Code Changes** (filled by Developer agent):

```diff
--- a/main.go
+++ b/main.go
@@ -310,6 +310,7 @@ type ConfigManager struct {
 	config      atomic.Value // stores *Config
 	configPath  string
 	lastModTime time.Time
+	debounceTimer *time.Timer
 	mu          sync.RWMutex
 }

@@ -389,6 +390,18 @@ func (cm *ConfigManager) checkAndReload() error {
 	return nil
 }

+// scheduleReload schedules a debounced config reload
+func (cm *ConfigManager) scheduleReload() {
+	cm.mu.Lock()
+	defer cm.mu.Unlock()
+
+	if cm.debounceTimer != nil {
+		cm.debounceTimer.Stop()
+	}
+
+	cm.debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
+		if err := cm.performReload(); err != nil {
+			log.Printf("Failed to reload config: %v", err)
+		}
+	})
+}
+
+// Cleanup stops any pending debounce timer
+func (cm *ConfigManager) Cleanup() {
+	cm.mu.Lock()
+	defer cm.mu.Unlock()
+
+	if cm.debounceTimer != nil {
+		cm.debounceTimer.Stop()
+		cm.debounceTimer = nil
+	}
+}
+
 // ================= TYPES =================

 type ServerInfo struct {

@@ -559,6 +572,10 @@ func (b *Bot) WaitForShutdown() {
 	<-sigchan
 	log.Println("Shutting down...")

+	// Cleanup config manager timer
+	if b.configManager != nil {
+		b.configManager.Cleanup()
+	}
+
 	if err := b.session.Close(); err != nil {
 		log.Printf("Error closing Discord session: %v", err)
 	}
```

Note: The checkAndReloadIfNeeded method from Milestone 2 calls scheduleReload when it detects a file change, instead of immediately reloading. The modified checkAndReloadIfNeeded becomes:

```diff
--- a/main.go
+++ b/main.go
@@ -389,20 +389,12 @@ func (cm *ConfigManager) checkAndReloadIfNeeded() error {
 		return fmt.Errorf("failed to stat config file: %w", err)
 	}

-	// No change detected
+	// No change detected or already scheduled
 	if currentModTime.Equal(cm.lastModTime) || currentModTime.Before(cm.lastModTime) {
 		return nil
 	}

-	// File has changed, attempt reload
-	log.Printf("Config file modified, attempting reload from: %s", cm.configPath)
-
-	newCfg, err := loadConfig(cm.configPath)
-	if err != nil {
-		return fmt.Errorf("failed to read config: %w", err)
-	}
-
-	// Validate new config
-	if err := validateConfigStructSafeRuntime(newCfg); err != nil {
-		return fmt.Errorf("config validation failed: %w", err)
-	}
-
-	// Success: atomically swap config and update mod time
-	cm.config.Store(newCfg)
-	cm.lastModTime = currentModTime
-	log.Println("Config reloaded successfully")
-
+	// File has changed, schedule debounced reload
+	log.Printf("Config file modified, scheduling reload from: %s", cm.configPath)
+	cm.scheduleReload()
 	return nil
 }
```

And add a new performReload method that does the actual reload:

```diff
--- a/main.go
+++ b/main.go
@@ -389,6 +389,30 @@ func (cm *ConfigManager) checkAndReloadIfNeeded() error {
+// performReload executes the actual config reload (called by debounce timer)
+func (cm *ConfigManager) performReload() error {
+	cm.mu.Lock()
+	defer cm.mu.Unlock()
+
+	// Load new config
+	newCfg, err := loadConfig(cm.configPath)
+	if err != nil {
+		return fmt.Errorf("failed to read config: %w", err)
+	}
+
+	// Validate new config
+	if err := validateConfigStructSafeRuntime(newCfg); err != nil {
+		return fmt.Errorf("config validation failed: %w", err)
+	}
+
+	// Get current mod time
+	currentModTime, err := cm.getLastModTime()
+	if err != nil {
+		return fmt.Errorf("failed to stat config file: %w", err)
+	}
+
+	// Success: atomically swap config and update mod time
+	cm.config.Store(newCfg)
+	cm.lastModTime = currentModTime
+	log.Println("Config reloaded successfully")
+
+	return nil
+}
+
 // scheduleReload schedules a debounced config reload
 func (cm *ConfigManager) scheduleReload() {
 	cm.mu.Lock()
@@ -401,7 +425,7 @@ func (cm *ConfigManager) scheduleReload() {
 	}

 	cm.debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
-		cm.checkAndReload()
+		if err := cm.performReload(); err != nil {
+			log.Printf("Failed to reload config: %v", err)
+		}
 	})
 }
```

---

### Milestone 4.5: QR Issue Fixes

**QR-Identified Issues Addressed**:

This milestone documents fixes for critical issues identified by Quality Review in QR iteration 2:

1. **[CRITICAL - Issue #12]** Function moved from main_test.go to main.go
   - `validateConfigStructSafe` moved to main.go in M1
   - Renamed to `validateConfigStructSafeRuntime` to distinguish usage
   - Prevents compilation error when ConfigManager calls it

2. **[CRITICAL - Issue #3]** getConfigPath matches loadConfig fallback logic
   - Added detailed comments showing exact line correspondence
   - Both functions follow same priority order: provided path -> /data/config.json -> ./config.json
   - Comments document why empty string return is acceptable (config already validated)

3. **[CRITICAL - Issue #1]** Method naming clarifies conditional behavior
   - Renamed `checkAndReload()` to `checkAndReloadIfNeeded()`
   - Name makes it clear the method checks mtime before reloading
   - Prevents future LLM confusion about unconditional reload

4. **[HIGH - Issue #4]** Consistent error wrapping
   - `getLastModTime()` returns raw os.Stat error (acceptable for syscall)
   - All other error paths use `fmt.Errorf` with `%w` for wrapping
   - Error messages improved: "failed to read config" instead of generic "failed to reload config"

5. **[HIGH - Issue #7]** Empty configPath handling documented
   - getConfigPath returns empty string when no config found
   - This is acceptable because config was already loaded successfully at startup
   - NewConfigManager accepts empty path (will fail on first reload attempt, which is correct behavior)

**Updated Method Names**:

- `checkAndReload()` → `checkAndReloadIfNeeded()` (M2, M3, M4)
- `validateConfigStructSafe()` → `validateConfigStructSafeRuntime()` (M1)
- New method: `performReload()` (M4, does actual reload called by debounce timer)

**Documentation Updates**:

All references to old method names updated throughout plan to use new names.

---

### Milestone 5: Documentation

**Delegated to**: @agent-technical-writer (mode: post-implementation)

**Source**: `## Invisible Knowledge` section of this plan

**Files**:

- `CLAUDE.md` (update project documentation)
- `README.md` (if needed for invisible knowledge)

**Requirements**:

Delegate to Technical Writer. For documentation format specification:

<file working-dir=".claude" uri="conventions/documentation.md" />

Key deliverables:
- Update CLAUDE.md with ConfigManager information
- Document config reload behavior and testing
- Add troubleshooting section for config reload issues

**Acceptance Criteria**:

- CLAUDE.md updated with ConfigManager architecture
- Config reload workflow documented
- Testing instructions added for config reload feature
- Troubleshooting guide covers common issues

**Source Material**: `## Invisible Knowledge` section of this plan

---

## Milestone Dependencies

```
M1 (ConfigManager struct + validateConfigStructSafeRuntime)
  |
  v
M2 (checkAndReloadIfNeeded logic)
  |
  v
M3 (Bot integration)
  |
  v
M4 (Debouncing + performReload)
  |
  v
M4.5 (QR issue fixes - documentation only)
  |
  v
M5 (Documentation)
```

M1 must complete first (foundational structure). M2-M4 must be developed in sequence (each builds on previous). M4.5 documents QR fixes after M4 complete. M5 depends on all implementation milestones.

## Cross-Milestone Integration Tests

Integration tests for config reload are placed in M3 (Bot Integration) as they require the complete reload flow working end-to-end with the actual update loop.

Tests verify:
- Config file modified while bot is running -> new config detected and applied
- Invalid config -> bot continues with old config, recovers when valid
- Multiple rapid changes -> debounce works correctly
- Config changes during active server polling -> no race conditions

These tests use real file system and simulate actual bot runtime conditions.
