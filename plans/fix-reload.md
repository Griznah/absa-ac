# Fix Config Reload Bug - Server IP Initialization

## Overview

When config.json is manually modified during runtime, all servers appear offline in Discord until bot restart. Root cause: `performReload()` loads and swaps config without applying post-load modifications that `main()` performs (setting `Server.IP = cfg.ServerIP`). This causes HTTP requests to fail with malformed URLs.

Chosen approach: Extract shared `initializeServerIPs()` function called by both `main()` and `performReload()`. This eliminates duplication, ensures consistent behavior, and provides a single source of truth for IP initialization logic.

## Planning Context

This section is consumed VERBATIM by downstream agents (Technical Writer, Quality Reviewer). Quality matters: vague entries here produce poor annotations and missed risks.

### Decision Log

| Decision | Reasoning Chain |
| ---------- | ------------ |
| Extract shared function over inline fix | Code duplication in main() and performReload() -> DRY violation creates maintenance burden -> shared function provides single source of truth with minimal complexity overhead |
| Function takes pointer to Config | Pass-by-pointer avoids copying entire struct on each call -> atomic.Value already stores pointer -> consistent with existing ConfigManager.GetConfig() return type |
| Function placement before ConfigManager methods | initializeServerIPs() operates on Config type -> placing before ConfigManager keeps related config logic together -> follows existing file organization pattern |
| Unit tests for function (not property-based) | Function has simple deterministic behavior (for loop over slice) -> example-based tests sufficient and more readable -> property-based tests would be overkill for this straightforward logic |

### Rejected Alternatives

| Alternative | Why Rejected |
| ------------------- | ------------------------------------------------------------------- |
| Inline fix in performReload() | Duplicates IP-setting logic from main() -> two locations to maintain -> violates DRY principle with no benefit |
| Remove Server.IP field entirely | Breaking change to config schema and function signatures -> higher risk scope for simple bug fix -> Server.IP field may be used elsewhere in codebase |
| Set IPs in config.json manually | Shifts burden to admin -> redundant data (ServerIP already exists) -> potential for inconsistency between ServerIP and individual server IPs |

### Constraints & Assumptions

- **Technical**: Go 1.22+, atomic.Value for thread-safe config storage, existing test patterns use temp directories and mock configs
- **Organizational**: Minimal scope fix preferred over architectural changes
- **Dependencies**: No external dependencies affected
- **Default conventions applied**: Testing follows existing project patterns (integration tests with real file operations, unit tests for pure functions)

### Known Risks

| Risk | Mitigation | Anchor |
| --------------- | --------------------------------------------- | ------------------------------------------ |
| Regression in existing config reload tests | Add new test case to existing TestIntegration_* suite | main_test.go:117-143 (existing integration test pattern) |
| IP initialization logic may have other callers | Verify no other code path depends on manual IP setting | main.go:865-867 (only existing location) |

## Invisible Knowledge

This section captures knowledge NOT deducible from reading the code alone.

### Architecture

```
main()
  |
  v
loadConfig() -> validateConfigStruct() -> initializeServerIPs()
                                              |
                                              v
                                    NewConfigManager(cfg)
                                              |
                                              v
                                        (atomic.Value stores *Config)

ConfigManager.performReload() (background goroutine)
  |
  v
loadConfig() -> validateConfigStructSafeRuntime() -> initializeServerIPs()
                                                       |
                                                       v
                                                 config.Store(newCfg)
```

### Data Flow

```
Config File (JSON)
  |
  v
loadConfig() parses JSON -> Config struct
  |
  v
initializeServerIPs() sets cfg.Servers[i].IP = cfg.ServerIP
  |
  v
ConfigManager stores config (atomic.Value)
  |
  v
fetchAllServers() reads config -> fetchServerInfo() uses server.IP to build URL
```

### Why This Structure

The ConfigManager uses `atomic.Value` for lock-free reads during frequent server polling (every N seconds). This design choice is critical for performance:

- **Write path**: Rare (config reload), uses mutex to serialize reload operations
- **Read path**: Frequent (every polling interval), uses atomic.Value.Load() for zero-copy access without mutex contention
- **Debouncing**: Text editors perform multiple writes during save; 100ms debounce prevents excessive reloads

The `initializeServerIPs()` function must execute **before** storing config in atomic.Value, ensuring swapped config is fully initialized before any reader sees it.

### Invariants

- All `Server.IP` fields must equal `Config.ServerIP` at all times
- Config stored in atomic.Value must be fully initialized before being visible to readers
- IP initialization happens exactly once per config load (not deferred or lazy)

### Tradeoffs

- **Duplication vs. Function**: Accepting slight overhead of function call for maintainability gain (DRY principle)
- **Function placement**: Before ConfigManager keeps config logic grouped, but could be inline with Config - chosen to minimize diff scope
- **Test scope**: Unit tests for function only, not full integration test with real HTTP - sufficient since existing integration tests already cover reload behavior

## Milestones

### Milestone 1: Create Shared Initialization Function

**Files**: `main.go`

**Flags**: `conformance` (follows existing Go patterns), `needs-rationale` (explain why this function exists)

**Requirements**:

- Add `initializeServerIPs(cfg *Config)` function that iterates over `cfg.Servers` and sets each server's IP field to `cfg.ServerIP`
- Function must handle empty server slice gracefully (no-op)
- Function must be placed before `NewConfigManager` to maintain logical ordering

**Acceptance Criteria**:

- Function compiles without errors
- Function correctly sets IP for all servers in config
- Function handles edge case of zero servers without panic

**Tests**:

- **Test files**: `main_test.go`
- **Test type**: unit
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Config with 3 servers, all IPs set correctly
  - Edge: Config with zero servers, no panic occurs
  - Edge: Config with ServerIP already set, function is idempotent

**Code Intent**:

Add new function `initializeServerIPs(cfg *Config)` between config validation and ConfigManager:

```go
// initializeServerIPs sets the IP field for each server to the global ServerIP value.
// This is called after config load to populate server IPs from the centralized ServerIP setting,
// avoiding redundancy in the config file while maintaining per-server IP fields for URL construction.
func initializeServerIPs(cfg *Config) {
	// Pointer avoids copying entire Config struct; atomic.Value already stores pointer
	for i := range cfg.Servers {
		cfg.Servers[i].IP = cfg.ServerIP
	}
}
```

Rationale: Centralized IP setting eliminates duplication between main() and performReload() (Decision: "Extract shared function over inline fix").

**Code Changes** (filled by Developer agent):

```diff
--- a/main.go
+++ b/main.go
@@ -453,6 +453,16 @@ func validateConfigStruct(cfg *Config) {
 	log.Printf("Configuration validated: %d servers across %d categories", len(cfg.Servers), len(cfg.CategoryOrder))
 }

+// initializeServerIPs sets the IP field for each server to the global ServerIP value.
+// This is called after config load to populate server IPs from the centralized ServerIP setting,
+// avoiding redundancy in the config file while maintaining per-server IP fields for URL construction.
+func initializeServerIPs(cfg *Config) {
+	// Pointer avoids copying entire Config struct; atomic.Value already stores pointer
+	for i := range cfg.Servers {
+		cfg.Servers[i].IP = cfg.ServerIP
+	}
+}
+
 // ================= HTTP CLIENT =================

 var httpClient = &http.Client{
```

### Milestone 2: Update main() to Use Shared Function

**Files**: `main.go`

**Flags**: `conformance` (maintains existing behavior)

**Requirements**:

- Replace inline IP-setting loop (lines 865-867) with call to `initializeServerIPs(cfg)`
- Preserve existing behavior: IPs are set after config load and validation

**Acceptance Criteria**:

- Bot starts successfully with existing config.json
- All server IPs are correctly set on startup
- No change to observable behavior (bot functions identically to before)

**Tests**:

- **Test type**: integration (existing)
- **Backing**: doc-derived
- **Scenarios**:
  - Normal: Bot startup loads config and initializes IPs (existing TestConfigManager behavior validates this)

**Code Intent**:

Replace the inline loop in `main()` that sets server IPs with a call to `initializeServerIPs(cfg)`. Maintains existing execution order: config validation, then IP initialization, then ConfigManager creation.

**Code Changes** (filled by Developer agent):

```diff
--- a/main.go
+++ b/main.go
@@ -860,9 +860,7 @@ func main() {
 	cfg, err := loadConfig(*configPath)
 	if err != nil {
 		log.Fatalf("Failed to load config: %v", err)
 	}
 	validateConfigStruct(cfg)

-	// Set server IPs from config
-	for i := range cfg.Servers {
-		cfg.Servers[i].IP = cfg.ServerIP
-	}
+	initializeServerIPs(cfg)

 	// Create config manager with initial config
 	configManager := NewConfigManager(getConfigPath(*configPath), cfg)
```

### Milestone 3: Update performReload() to Initialize IPs

**Files**: `main.go`

**Flags**: `conformance` (maintains existing reload behavior), `error-handling` (config validation and reload errors)

**Requirements**:

- Add call to `initializeServerIPs(newCfg)` in `performReload()` after successful config validation
- IP initialization must happen before `cm.config.Store(newCfg)` to ensure readers see fully-initialized config
- Preserve existing error handling and logging

**Acceptance Criteria**:

- Config reload (manual file modification) correctly sets server IPs
- Servers display correct online/offline status after reload without restart
- Existing integration tests for config reload still pass
- No race conditions introduced (atomic swap still happens with fully-initialized config)

**Tests**:

- **Test files**: `main_test.go`
- **Test type**: integration
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Modify config file during runtime, verify IPs are set and servers are reachable
  - Edge: Reload with same ServerIP value (idempotent)
  - Error: Reload with invalid config, verify original IPs remain intact (config not swapped)

**Code Intent**:

In `performReload()`, add call to `initializeServerIPs(newCfg)` after successful validation and before atomic swap. This ensures reloaded config has fully-initialized server IPs before becoming visible to polling goroutines via atomic.Value.

Rationale: Config reload must maintain same initialization as initial load to prevent servers appearing offline (Decision: "Extract shared function over inline fix").

**Code Changes** (filled by Developer agent):

```diff
--- a/main.go
+++ b/main.go
@@ -210,9 +210,11 @@ func (cm *ConfigManager) performReload() error {
 		return fmt.Errorf("config validation failed: %w", err)
 	}

+	// Initialize server IPs from global ServerIP setting.
+	// Must complete before atomic swap; readers see config via atomic.Value without locks.
+	initializeServerIPs(newCfg)
+
 	// Success: atomically swap config and update mod time
 	cm.config.Store(newCfg)
 	cm.lastModTime = currentModTime
 	log.Println("Config reloaded successfully")

 	return nil
```

### Milestone 4: Documentation

**Delegated to**: @agent-technical-writer (mode: post-implementation)

**Source**: `## Invisible Knowledge` section of this plan

**Files**:

- `CLAUDE.md` (index updates for config reload behavior)
- `README.md` (if Invisible Knowledge section has content)

**Requirements**:

Update CLAUDE.md with reference to config reload behavior and IP initialization. This is a simple bug fix with minimal architectural changes, so documentation updates are light.

**Acceptance Criteria**:

- CLAUDE.md accurately reflects current behavior after fix
- Any relevant architectural details from Invisible Knowledge are captured

**Source Material**: `## Invisible Knowledge` section of this plan

## Milestone Dependencies

```
M1 (create function) --> M2 (update main())
                     |
                     v
                   M3 (update performReload())
                     |
                     v
                   M4 (documentation)
```

Milestones execute sequentially due to dependencies: M1 must complete before M2/M3 can call the function, and M4 documents the final implementation.
