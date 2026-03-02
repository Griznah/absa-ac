# No-Config-at-Startup Implementation Plan

## Overview

**Problem:** Bot currently exits with `log.Fatalf` if config.json doesn't exist at startup. This prevents containerized deployments where config may be mounted/created later or provided via API at runtime.

**Approach:** Modify startup flow to gracefully handle missing config: allowing the bot to start and wait for config to appear via file creation or API call. When config appears, begin normal operation.

## Decisions

| ID | Decision | Reasoning |
|----|----------|-----------|
| DL-001 | Return nil config instead of os.Exit(1) on startup | Current behavior: `loadConfig()` returns error, causing `log.Fatalf` in `main()` -> Container orchestration needs config mounted after bot starts -> Implementation: Return nil from `loadConfig`, log warning, create ConfigManager with nil config -> Hot-reload: Existing polling mechanism continues to work when config appears |
| DL-002 | Update loop logs and skips when config missing | User preference: Log + Skip on each cycle -> Provides visibility into bot state -> Simpler than state machine for tracking 'already logged' |
| DL-003 | GetConfig returns nil pointer, all callers must check | `atomic.Value` stores `*Config` pointer -> Type assertion on nil panics -> Store nil pointer explicitly -> `GetConfig` returns nil -> Callers must check `cfg != nil` before field access |

## Rejected Alternatives

| ID | Alternative | Reason Rejected |
|----|-------------|-----------------|
| RA-001 | Create minimal config with hardcoded defaults | Hardcoded defaults don't match flexible runtime use case. Users expect containerized deployments where config may be mounted at runtime via secrets or init containers. |
| RA-002 | Require config file to exist, return error and recreate placeholder config | Requiring file to exist breaks the hot-reload pattern. API can still write config via `WriteConfig`/`UpdateConfig`. Adds complexity. |

## Constraints

- Must maintain single-file architecture. No new modules or packages.

## Risks

| ID | Risk | Mitigation | Anchor |
|----|------|------------|--------|
| R-001 | API cannot write config if file doesn't exist | `checkAndReloadIfNeeded` already attempts reload via `os.Stat`. Log warning and skip update. ConfigManager holds nil config. | `main.go:checkAndReloadIfNeeded` |

## Invariants

- `ConfigManager.GetConfig()` always returns valid config (or nil if no config loaded)
- Missing config: bot stays running, waiting for config
- Invalid config: never replaces valid config

## Tradeoffs

- Chose simplicity over performance: Polling mtime every 30s is acceptable for admin-triggered config changes. Avoided complexity of event-driven file watching.

---

## Milestone M-001: Graceful Config Loading

### Files

- `main.go`
- `main_test.go`

### Requirements

1. Bot must start without crashing when config.json doesn't exist
2. Bot must log warning and continue running when config is missing
3. Bot must start normal operation when valid config appears (file or API)
4. Update loop must skip server updates when config is missing or invalid

### Acceptance Criteria

- **AC-001:** Bot starts successfully with no config.json file
- **AC-002:** Log contains 'Config file not found, starting without config. Waiting for config...' message
- **AC-003:** When config.json created, bot logs 'Config loaded successfully' and starts normal operation
- **AC-004:** When invalid config provided via API, bot logs validation error and continues with old config
- **AC-005:** Update loop logs 'Skipping update: no valid config' when config missing/invalid
- **AC-006:** API returns config via PATCH/PUT, config is written to file and hot-reload triggers
- **AC-007:** File deletion during runtime is logged but bot continues running

### Tests

| Test Name | Description |
|-----------|-------------|
| `TestNoConfigStart_NotFatal` | Verify bot starts without config, doesn't crash |
| `TestNoConfigWaitAndLoad` | Verify bot waits and loads config when file appears |
| `TestConfigFileDeleted` | Verify bot handles file deletion gracefully |
| `TestNoConfigAPIUpdate` | Verify config can be provided via API |

---

## Code Changes

### CC-M-001-001: loadConfig - Return nil on missing file

**File:** `main.go`
**Function:** `loadConfig`
**Intent:** Return nil and warning when config file doesn't exist, instead of returning error that causes `log.Fatalf`.

```diff
--- a/main.go
+++ b/main.go
@@ -776,15 +776,20 @@ type Config struct {
 }

 // loadConfig reads and parses config.json with fallback logic
 func loadConfig(providedPath string) (*Config, error) {
 	// If explicitly provided, only try that path
 	if providedPath != "" {
 		log.Printf("Loading config from provided path: %s", providedPath)
 		data, err := os.ReadFile(providedPath)
 		if err != nil {
-			return nil, fmt.Errorf("failed to read config from %s: %w", providedPath, err)
+			if os.IsNotExist(err) {
+				log.Printf("Config file not found at %s, starting without config", providedPath)
+				return nil, nil
+			}
+			return nil, fmt.Errorf("failed to read config from %s: %w", providedPath, err)
 		}

 		var cfg Config
 		if err := json.Unmarshal(data, &cfg); err != nil {
@@ -820,7 +825,12 @@ func loadConfig(providedPath string) (*Config, error) {
 		return &cfg, nil
 	}

-	return nil, fmt.Errorf("failed to load config from any default location:\n%s", strings.Join(errors, "\n"))
+	// No config file found in any default location
+	log.Printf("Config file not found at any default location. Starting without config.")
+	log.Printf("Searched locations: /data/config.json, ./config.json")
+	log.Printf("Waiting for config to be created or provided via API...")
+	return nil, nil
 }
```

### CC-M-001-002: NewConfigManager - Accept nil config

**File:** `main.go`
**Function:** `NewConfigManager`
**Intent:** Accept nil config parameter. Initialize with nil config stored in `atomic.Value`. Set configPath to intended file path.

```diff
--- a/main.go
+++ b/main.go
@@ -188,10 +188,16 @@ type ConfigManager struct {
 }

 // NewConfigManager creates a new ConfigManager with an initial configuration
 // Stores initial config in atomic.Value for lock-free access
 // Records initial file modification time to detect future changes
 func NewConfigManager(configPath string, initial *Config) *ConfigManager {
 	cm := &ConfigManager{
 		configPath: configPath,
 	}
 	cm.config.Store(initial)

-	// Get initial file modification time
-	if modTime, err := cm.getLastModTime(); err == nil {
-		cm.lastModTime = modTime
-	} else {
-		log.Printf("Warning: failed to get initial config mod time: %v", err)
+	// Get initial file modification time (only if config exists)
+	if initial != nil {
+		if modTime, err := cm.getLastModTime(); err == nil {
+			cm.lastModTime = modTime
+		} else {
+			log.Printf("Warning: failed to get initial config mod time: %v", err)
+		}
 	}

 	return cm
 }
```

### CC-M-001-003: GetConfig - Type-safe nil handling

**File:** `main.go`
**Function:** `ConfigManager.GetConfig`
**Intent:** Return nil when no config loaded. Use type-safe pattern to prevent panic on nil type assertion.

```diff
--- a/main.go
+++ b/main.go
@@ -206,9 +206,14 @@ func NewConfigManager(configPath string, initial *Config) *ConfigManager {

 // GetConfig returns the current configuration (thread-safe, lock-free read)
 // atomic.Value.Load() provides zero-copy access without mutex contention
 // Multiple goroutines can call this simultaneously during server polling
 func (cm *ConfigManager) GetConfig() *Config {
-	return cm.config.Load().(*Config)
+	val := cm.config.Load()
+	if val == nil {
+		return nil
+	}
+	return val.(*Config)
 }
```

### CC-M-001-004: checkAndReloadIfNeeded - Handle missing config

**File:** `main.go`
**Function:** `ConfigManager.checkAndReloadIfNeeded`
**Intent:** When config is nil, check if file exists. Skip reload on file not found. Log warnings.

```diff
--- a/main.go
+++ b/main.go
@@ -226,6 +226,11 @@ func (cm *ConfigManager) getLastModTime() (time.Time, error) {
 // Holds the lock during the entire operation to prevent race conditions.
 func (cm *ConfigManager) checkAndReloadIfNeeded() error {
 	cm.mu.Lock()
 	defer cm.mu.Unlock()

+	// If no config currently loaded, check if file exists now
+	if cm.config.Load() == nil {
+		log.Printf("No config loaded, checking if config file exists...")
+	}
+
 	// Check current file modification time
 	currentModTime, err := cm.getLastModTime()
 	if err != nil {
-		return fmt.Errorf("failed to stat config file: %w", err)
+		if os.IsNotExist(err) {
+			log.Printf("Config file not found, skipping reload")
+			return nil
+		}
+		return fmt.Errorf("failed to stat config file: %w", err)
 	}

 	// No change detected
@@ -261,6 +270,11 @@ func (cm *ConfigManager) checkAndReloadIfNeeded() error{
 		return fmt.Errorf("failed to read config: %w", err)
 	}

+	// If loadConfig returned nil (file not found), skip reload
+	if newCfg == nil {
+		log.Printf("Config file not found during reload attempt")
+		return nil
+	}
+
 	// Validate new config
 	if err := validateConfigStructSafeRuntime(newCfg); err != nil {
 		return fmt.Errorf("config validation failed: %w", err)
```

### CC-M-001-005: startUpdateLoop - Use default interval when no config

**File:** `main.go`
**Function:** `startUpdateLoop`
**Intent:** Use default interval if no config loaded. Log warning about using default.

```diff
--- a/main.go
+++ b/main.go
@@ -1189,10 +1189,22 @@ func (b *Bot) registerHandlers() {

 // ================= UPDATE LOOP =================

 func (b *Bot) startUpdateLoop() {
-	cfg := b.configManager.GetConfig()
-	ticker := time.NewTicker(time.Duration(cfg.UpdateInterval) * time.Second)
+	// Use default interval if no config loaded
+	defaultInterval := 30 * time.Second
+	cfg := b.configManager.GetConfig()
+	interval := defaultInterval
+	if cfg != nil {
+		interval = time.Duration(cfg.UpdateInterval) * time.Second
+	} else {
+		log.Printf("No config loaded, using default update interval: %v", defaultInterval)
+	}
+	ticker := time.NewTicker(interval)
 	defer ticker.Stop()

 	// Immediate first update
 	b.performUpdate()
```

### CC-M-001-006: performUpdate - Skip when no config

**File:** `main.go`
**Function:** `performUpdate`
**Intent:** When config is nil, log warning and return early without performing Discord operations.

```diff
--- a/main.go
+++ b/main.go
@@ -1206,6 +1206,12 @@ func (b *Bot) startUpdateLoop() {
 }

 func (b *Bot) performUpdate() {
+	cfg := b.configManager.GetConfig()
+	if cfg == nil {
+		log.Printf("Skipping update: no valid config loaded. Waiting for config...")
+		return
+	}
+
 	// Fetch all server info concurrently
 	infos := fetchAllServers(b.configManager)

 	// Build embed
```

### CC-M-001-007: main - Skip validation when config nil

**File:** `main.go`
**Function:** `main`
**Intent:** Skip `validateConfigStruct` call when config is nil (it panics on nil). Log appropriate message.

```diff
--- a/main.go
+++ b/main.go
@@ -1526,13 +1526,27 @@ func main() {
 	}

 	// Load and validate config.json
 	cfg, err := loadConfig(*configPath)
 	if err != nil {
 		log.Fatalf("Failed to load config: %v", err)
 	}
-	validateConfigStruct(cfg)
-
-	// Initialize server IPs before ConfigManager creation (required for lock-free readers via atomic.Value)
-	initializeServerIPs(cfg)
+	if cfg == nil {
+		log.Printf("Config file not found, starting without config. Waiting for config...")
+	} else {
+		validateConfigStruct(cfg)
+
+		// Initialize server IPs before ConfigManager creation (required for lock-free readers via atomic.Value)
+		initializeServerIPs(cfg)
+	}

 	// Create config manager with initial config (may be nil)
 	configManager := NewConfigManager(getConfigPath(*configPath), cfg)
```

---

## Implementation Notes

1. **All callers of GetConfig() must check for nil** before accessing any config fields
2. **fetchAllServers()** already uses GetConfig() and should check for nil
3. **buildEmbed()** already uses GetConfig() and should check for nil
4. The existing hot-reload mechanism (mtime polling) continues to work automatically
5. API endpoints can create config via WriteConfig/UpdateConfig when file doesn't exist
