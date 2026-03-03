# Simplify Config Path to /data/config.json Only

## Overview

**Problem:** Current implementation has fallback logic trying `/data/config.json` then `./config.json`. This adds complexity for local development but container deployments always use `/data/`. Users outside containers can use `-c` flag for custom paths.

**Approach:** Remove `./config.json` fallback from `loadConfig` and `getConfigPath` functions. Default path is `/data/config.json` only. Explicit `-c`/`--config` flag behavior unchanged.

## Decisions

| ID | Decision | Reasoning |
|----|----------|-----------|
| DL-001 | Remove ./config.json fallback, use only /data/config.json as default | Container deployments use /data/ mount point -> ./config.json fallback adds complexity for non-container use -> Users outside containers can use -c flag -> Simplifies logic and reduces cognitive load |
| DL-002 | Keep getConfigPath function synchronized with loadConfig | getConfigPath determines which file to watch for hot-reload -> Must match loadConfig fallback logic exactly -> Removing ./config.json fallback from both maintains consistency |

## Rejected Alternatives

| ID | Alternative | Reason Rejected |
|----|-------------|-----------------|
| RA-001 | Add constant for default path /data/config.json | Overkill for single usage - the path is only referenced in two functions and adding a constant adds indirection without clarity benefit |
| RA-002 | Keep fallback but log deprecation warning | Adds complexity for transitional period that is not needed - this is an internal tool with controlled deployments |

## Constraints

- MUST: Keep -c and --config CLI flags working exactly as before
- MUST: Default path is /data/config.json only (no fallback to ./config.json)
- MUST: Maintain no-config-at-startup behavior (nil config on missing file)
- MUST-NOT: Change behavior when explicit path provided via -c/--config

## Invariants

- `ConfigManager.GetConfig()` returns nil when no config loaded (no-config-at-startup)
- Missing config file returns nil, not error (graceful handling)
- Hot-reload polling continues to work when config appears

## Tradeoffs

- Chose simplicity over flexibility: single default path reduces cognitive load
- Non-container users must use `-c` flag for custom paths (acceptable tradeoff for container-first design)

---

## Milestone M-001: Simplify config path fallback logic

### Files

- `main.go`
- `main_test.go`

### Requirements

1. Default config path is `/data/config.json` only (no `./config.json` fallback)
2. `-c` and `--config` CLI flags continue to work exactly as before
3. No-config-at-startup behavior preserved (nil config on missing file)

### Acceptance Criteria

- **AC-001:** `loadConfig("")` tries only /data/config.json, not ./config.json
- **AC-002:** `getConfigPath("")` returns "/data/config.json" without checking file existence
- **AC-003:** `loadConfig("/custom/path")` loads from explicit path unchanged
- **AC-004:** All existing tests pass after changes

---

## Code Changes

### CC-M-001-001: loadConfig - Remove ./config.json fallback

**File:** `main.go`
**Function:** `loadConfig`
**Intent:** Remove `filepath.Join(wd, "config.json")` from `defaultPaths` slice. Only try `/data/config.json` when no explicit path provided. Update log message to reflect single search location.

```diff
--- a/main.go
+++ b/main.go
@@ -819,30 +819,18 @@ func loadConfig(providedPath string) (*Config, error) {
 		return &cfg, nil
 	}

-	// Otherwise, try default locations in priority order
-	wd, err := os.Getwd()
-	if err != nil {
-		return nil, fmt.Errorf("failed to get working directory: %w", err)
-	}
-
-	defaultPaths := []string{
-		"/data/config.json",
-		filepath.Join(wd, "config.json"),
-	}
-
-	var errors []string
-	for _, path := range defaultPaths {
-		log.Printf("Attempting to load config from: %s", path)
-
-		data, err := os.ReadFile(path)
-		if err != nil {
-			errors = append(errors, fmt.Sprintf("  %s: %v", path, err))
-			continue
-		}
+	// Default path is /data/config.json only
+	defaultPath := "/data/config.json"
+	log.Printf("Attempting to load config from: %s", defaultPath)

-		var cfg Config
-		if err := json.Unmarshal(data, &cfg); err != nil {
-			return nil, fmt.Errorf("failed to parse config from %s: %w", path, err)
-		}
+	data, err := os.ReadFile(defaultPath)
+	if err != nil {
+		if os.IsNotExist(err) {
+			log.Printf("Config file not found at %s, starting without config", defaultPath)
+			return nil, nil
+		}
+		return nil, fmt.Errorf("failed to read config from %s: %w", defaultPath, err)
+	}

-		log.Printf("Successfully loaded config from: %s", path)
-		return &cfg, nil
+	var cfg Config
+	if err := json.Unmarshal(data, &cfg); err != nil {
+		return nil, fmt.Errorf("failed to parse config from %s: %w", defaultPath, err)
 	}

-	// No config file found in any default location
-	log.Printf("Config file not found at any default location. Starting without config.")
-	log.Printf("Searched locations: /data/config.json, ./config.json")
+	log.Printf("Successfully loaded config from: %s", defaultPath)
+	return &cfg, nil
-	log.Printf("Waiting for config to be created or provided via API...")
-	return nil, nil
 }
```

### CC-M-001-002: getConfigPath - Simplify to return /data/config.json directly

**File:** `main.go`
**Function:** `getConfigPath`
**Intent:** Remove `filepath.Join(wd, "config.json")` from `defaultPaths` slice. Return `/data/config.json` directly when no explicit path provided (no stat check needed since loadConfig handles missing file gracefully).

```diff
--- a/main.go
+++ b/main.go
@@ -856,32 +856,11 @@ func loadConfig(providedPath string) (*Config, error) {
 // getConfigPath determines the actual config file path that loadConfig uses
 // Matches loadConfig's fallback logic exactly: provided path -> /data/config.json -> ./config.json
 func getConfigPath(providedPath string) string {
 	// If explicitly provided, return that path (matches loadConfig's provided-path mode)
 	if providedPath != "" {
 		return providedPath
 	}

-	// Otherwise, try default locations in same priority order as loadConfig's fallback mode
-	wd, err := os.Getwd()
-	if err != nil {
-		// If we can't get working directory, config load fails
-		// Return empty string to signal error condition
-		return ""
-	}
-
-	defaultPaths := []string{
-		"/data/config.json",
-		filepath.Join(wd, "config.json"),
-	}
-
-	// Return first existing path (matches loadConfig's fallback priority order)
-	for _, path := range defaultPaths {
-		if _, err := os.Stat(path); err == nil {
-			return path
-		}
-	}
-
-	// No config file found - this matches loadConfig's error return when all paths fail
-	// Empty string signals that no valid config path exists
-	return ""
+	// Default path is /data/config.json only (matches loadConfig behavior)
+	return "/data/config.json"
 }
```

### CC-M-001-003: TestGetConfigPath - Update test expectations

**File:** `main_test.go`
**Function:** `TestGetConfigPath`
**Intent:** Update test cases to reflect new behavior: remove test for ./config.json fallback, update expected paths to /data/config.json only.

```diff
--- a/main_test.go
+++ b/main_test.go
@@ -1083,68 +1083,28 @@ func TestGetConfigPath(t *testing.T) {
 	testCases := []struct {
 		name         string
 		providedPath string
 		setupFunc    func(t *testing.T) func()
 		validateFunc func(t *testing.T, result string)
 	}{
 		{
 			name:         "explicit path is returned",
 			providedPath: "/custom/config.json",
 			setupFunc:    func(t *testing.T) func() { return func() {} },
 			validateFunc: func(t *testing.T, result string) {
 				if result != "/custom/config.json" {
 					t.Errorf("Expected path '/custom/config.json', got: %s", result)
 				}
 			},
 		},
 		{
-			name:         "uses ./config.json when /data/config.json doesn't exist",
+			name:         "returns /data/config.json when no path provided",
 			providedPath: "",
 			setupFunc: func(t *testing.T) func() {
-				tmpDir := t.TempDir()
-				configPath := filepath.Join(tmpDir, "config.json")
-				os.WriteFile(configPath, []byte("{}"), 0644)
-
-				origWd, _ := os.Getwd()
-				os.Chdir(tmpDir)
-
-				return func() {
-					os.Chdir(origWd)
-				}
+				return func() {}
 			},
 			validateFunc: func(t *testing.T, result string) {
-				// Should return the working directory config.json
-				if !strings.Contains(result, "config.json") {
-					t.Errorf("Expected path containing 'config.json', got: %s", result)
+				if result != "/data/config.json" {
+					t.Errorf("Expected '/data/config.json', got: %s", result)
 				}
 			},
 		},
-		{
-			name:         "returns empty string when no config found",
-			providedPath: "",
-			setupFunc: func(t *testing.T) func() {
-				tmpDir := t.TempDir()
-				origWd, _ := os.Getwd()
-				os.Chdir(tmpDir)
-
-				return func() {
-					os.Chdir(origWd)
-				}
-			},
-			validateFunc: func(t *testing.T, result string) {
-				if result != "" {
-					t.Errorf("Expected empty string when no config found, got: %s", result)
-				}
-			},
-		},
 	}
```

---

## Implementation Notes

1. **loadConfig()** now has single default path - no loop needed
2. **getConfigPath()** returns hardcoded path - no stat check needed since loadConfig handles missing file gracefully
3. **No-config-at-startup** behavior preserved: missing /data/config.json returns nil config, not error
4. **Hot-reload** continues to work: ConfigManager polls /data/config.json mtime
