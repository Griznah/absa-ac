# Implementation Plan: Fix Web Frontend Integration Bugs

**Plan ID:** webfront-fix-001
**Created:** 2026-02-13
**Status:** Ready for Implementation

---

## Overview

### Problem
Critical bugs in webfront integration prevent the Svelte SPA from being served:
- **Self-assignment bug** in NewBot constructor returns zero-value webfrontServer
- **Goroutine error silencing** makes failures undetectable
- **Missing dist directory** causes silent failure at runtime
- **Constructor parameter count** (10) creates maintenance burden

### Approach
Fix critical bugs with minimal disruption:
1. Correct self-assignment in NewBot
2. Add error channel for goroutine failure propagation
3. Align webfront server pattern with existing api.Server architecture
4. Add config structs to reduce parameter count
5. Maintain backward compatibility with existing environment variables

---

## Decisions

| ID | Decision | Reasoning |
|----|----------|-----------|
| DL-001 | Use error channel pattern for goroutine error propagation | Start() spawns goroutines for webfront -> goroutine errors only logged -> failures undetectable -> error channel exposes failures to caller |
| DL-002 | Align webfront server with api.Server pattern | api.Server has mature pattern: Start(ctx) blocks until context done, stores cancel func, uses sync.WaitGroup -> reuse proven architecture |
| DL-003 | Add config structs to reduce constructor parameters | NewBot has 10 parameters -> each feature adds more -> extract APIConfig and WebfrontConfig structs |
| DL-004 | Keep global variables but populate from config struct | Global webfrontEnabled/webfrontPort used in multiple places -> full removal is larger refactor -> pass config struct to NewBot |
| DL-005 | Validate dist directory at startup, not per-request | Missing dist directory causes silent failure at runtime -> fail fast with clear error message |

### Rejected Alternatives

| ID | Alternative | Reason |
|----|-------------|--------|
| RA-001 | Complete architecture refactor to separate services | Too disruptive for bug fix scope |
| RA-002 | Add compression/caching middleware | Deferred - current priority is fixing critical bugs |
| RA-003 | Extract webfront to separate package | Premature - fix bugs first |

---

## Constraints

- **MUST** fix self-assignment bug in NewBot constructor (webfrontServer always nil)
- **MUST** propagate goroutine errors so failures are detectable
- **MUST** maintain backward compatibility with existing environment variables
- **MUST NOT** break existing API server or Discord bot functionality
- **SHOULD** reduce constructor parameter count from 10 to ~5 via config structs

---

## Risks

| ID | Risk | Mitigation |
|----|------|------------|
| R-001 | Error channel consumer may not read errors fast enough | Use buffered channel (size 2) to allow both API and webfront errors without blocking |
| R-002 | Config struct changes may affect test mocks | Update test mocks to construct config structs from parameters |

---

## Milestones

### Wave 1: Critical Bug Fixes

#### M-001: Fix NewBot self-assignment bug

**Files:** `main.go`

**Requirements:**
- NewBot returns Bot with correct webfrontServer reference
- Remove double server initialization (placeholder in NewBot, real in ServeWebfront)

**Acceptance Criteria:**
- webfrontServer is non-nil when webfrontEnabled=true
- NewBot returns the same instance that has webfrontServer populated (not zero-value copy)
- Single initialization point for webfrontServer

**Implementation:**

```go
// main.go - NewBot function
// BEFORE:
return &Bot{
    session:       session,
    channelID:     channelID,
    configManager: cfgManager,
    apiServer:     bot.apiServer,      // BUG: bot is zero-value local var
    webfrontServer: bot.webfrontServer, // BUG: This is nil!
}

// AFTER:
// Return the local bot variable directly, which already has all fields populated
// Previous code created a new struct with zero-value fields, losing server references
return bot, nil
```

---

#### M-002: Add config structs for constructor

**Files:** `main.go`

**Requirements:**
- Define APIConfig struct with Port, BearerToken, CorsOrigins, TrustedProxies fields
- Define WebfrontConfig struct with Enabled, Port fields
- NewBot accepts config structs instead of 10 individual parameters
- Backward compatible: old main() call site constructs structs from env vars

**Acceptance Criteria:**
- NewBot signature has ~5 parameters instead of 10
- All existing functionality preserved
- go build succeeds

**Implementation:**

```go
// main.go - Add new types after Config struct

// APIConfig holds REST API server configuration
type APIConfig struct {
    Port           string
    BearerToken    string
    CorsOrigins    []string
    TrustedProxies []string
}

// WebfrontConfig holds web frontend server configuration
type WebfrontConfig struct {
    Enabled bool
    Port    string
}
```

```go
// main.go - Update NewBot signature
// BEFORE:
func NewBot(cfgManager *ConfigManager, token, channelID string, apiEnabled bool, apiPort, apiBearerToken, apiCorsOrigins string, apiTrustedProxies []string, webfrontEnabled bool) (*Bot, error)

// AFTER:
func NewBot(cfgManager *ConfigManager, token, channelID string, apiCfg *APIConfig, webfrontCfg *WebfrontConfig) (*Bot, error)
```

```go
// main.go - Populate global variables in NewBot for backward compatibility
if webfrontCfg != nil {
    webfrontEnabled = webfrontCfg.Enabled
    webfrontPort = webfrontCfg.Port
}
```

```go
// main.go - Update call site in main()
apiCfg := &APIConfig{
    Port:           apiPort,
    BearerToken:    apiBearerToken,
    CorsOrigins:    parseStringSlice(apiCorsOrigins),
    TrustedProxies: apiTrustedProxies,
}

webfrontCfg := &WebfrontConfig{
    Enabled: webfrontEnabled,
    Port:    webfrontPort,
}

bot, err := NewBot(cfgManager, discordToken, channelID, apiCfg, webfrontCfg)
```

---

### Wave 2: Error Propagation & Server Alignment

#### M-003: Add error channel for goroutine failure propagation

**Files:** `main.go`

**Requirements:**
- Bot struct has Errors field: chan error (buffered, size 2)
- Start() returns error channel for caller to monitor
- API server and webfront server send startup/runtime errors to channel
- WaitForShutdown drains error channel before exit

**Acceptance Criteria:**
- Start() returns <-chan error
- Webfront dist missing sends error to channel
- API server bind failure sends error to channel

**Implementation:**

```go
// main.go - Add Errors channel to Bot struct
type Bot struct {
    // ... existing fields ...

    // Webfront server (optional - nil if disabled)
    webfrontServer *http.Server
    webfrontCancel context.CancelFunc

    // Errors channel for goroutine failure propagation (buffered for API+webfront)
    Errors chan error
}
```

```go
// main.go - Initialize error channel in NewBot
// Initialize error channel for goroutine failure propagation
// Buffered size 2 allows both API and webfront to send errors without blocking
bot.Errors = make(chan error, 2)
```

```go
// main.go - Update Start() signature
// BEFORE:
func (b *Bot) Start() error

// AFTER:
func (b *Bot) Start() (<-chan error, error)
```

```go
// main.go - Send errors to channel in goroutines
// API server goroutine:
go func() {
    if err := b.apiServer.Start(ctx); err != nil {
        log.Printf("API server error: %v", err)
        select {
        case b.Errors <- fmt.Errorf("API server: %w", err):
        default:
        }
    }
}()

// Webfront server goroutine:
go func() {
    if err := b.ServeWebfront(ctx, webfrontPort); err != nil && err != context.Canceled {
        log.Printf("Webfront server error: %v", err)
        select {
        case b.Errors <- fmt.Errorf("Webfront server: %w", err):
        default:
        }
    }
}()

// Return error channel
return b.Errors, nil
```

```go
// main.go - Update main() to handle error channel
errChan, err := bot.Start()
if err != nil {
    log.Fatalf("Failed to start bot: %v", err)
}

// Log errors from goroutines in background
go func() {
    for err := range errChan {
        log.Printf("Server error: %v", err)
    }
}()
```

---

#### M-004: Align webfront with api.Server pattern

**Files:** `main.go`

**Requirements:**
- ServeWebfront uses sync.WaitGroup for goroutine tracking
- ServeWebfront stores cancel func for Stop() to call
- Remove triple-nested goroutine pattern
- Validate dist directory at NewBot time, not in ServeWebfront

**Acceptance Criteria:**
- Bot struct has webfrontWg sync.WaitGroup field
- ServeWebfront calls webfrontWg.Add(1) before ListenAndServe
- Stop() calls webfrontWg.Wait() after context cancellation
- Missing dist directory fails at NewBot, not at runtime

**Implementation:**

```go
// main.go - Add webfrontWg to Bot struct
type Bot struct {
    // ... existing fields ...

    // Webfront server (optional - nil if disabled)
    webfrontServer *http.Server
    webfrontCancel context.CancelFunc
    webfrontWg     sync.WaitGroup

    // Errors channel
    Errors chan error
}
```

```go
// main.go - Validate dist directory in NewBot (fail-fast)
if webfrontCfg != nil && webfrontCfg.Enabled {
    distPath := "webfront/dist"
    if _, err := os.Stat(distPath); err != nil {
        return nil, fmt.Errorf("webfront dist directory not found: %s", distPath)
    }
    log.Printf("Webfront dist directory validated: %s", distPath)
}
```

```go
// main.go - Refactored ServeWebfront (keep goroutine for proper WaitGroup tracking)
func (b *Bot) ServeWebfront(ctx context.Context, port string) error {
    distPath := filepath.Join(".", "webfront", "dist")

    // Create mux and configure handlers...

    b.webfrontServer = &http.Server{
        Addr:    ":" + port,
        Handler: mux,
    }

    // Track server goroutine for graceful shutdown
    b.webfrontWg.Add(1)

    // Start server in background goroutine
    go func() {
        defer b.webfrontWg.Done()
        log.Printf("Webfront server starting on http://0.0.0.0:%s", port)
        if err := b.webfrontServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
            log.Printf("Webfront server error: %v", err)
            select {
            case b.Errors <- fmt.Errorf("Webfront server: %w", err):
            default:
            }
        }
    }()

    // Wait for context cancellation
    <-ctx.Done()
    log.Println("Shutting down webfront server...")

    // Graceful shutdown with 10 second timeout
    shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
    defer cancel()

    if err := b.webfrontServer.Shutdown(shutdownCtx); err != nil {
        return fmt.Errorf("webfront server shutdown failed: %w", err)
    }

    log.Println("Webfront server stopped")
    return nil
}
```

---

### Wave 3: Test Coverage

#### M-005: Add webfront tests

**Files:** `main_test.go`

**Requirements:**
- Test NewBot with webfront enabled returns non-nil webfrontServer
- Test error channel receives webfront startup errors
- Test SPA routing handler behavior
- Test backward compatibility for API server and Discord bot functionality

**Acceptance Criteria:**
- `go test -v -run TestWebfront` passes
- `go test -v -run TestNewBot_MissingDistDirectory` passes
- `go test -v -run TestStart_ErrorChannel` passes
- `go test -v -run TestNewBot_BackwardCompatibility` passes

**Implementation:**

```go
// main_test.go - TestNewBot_WebfrontEnabled
func TestNewBot_WebfrontEnabled(t *testing.T) {
    // Create temp dist directory to satisfy validation
    tmpDir := t.TempDir()
    distPath := filepath.Join(tmpDir, "webfront", "dist")
    if err := os.MkdirAll(distPath, 0755); err != nil {
        t.Fatalf("Failed to create dist directory: %v", err)
    }

    origWd, _ := os.Getwd()
    if err := os.Chdir(tmpDir); err != nil {
        t.Fatalf("Failed to change directory: %v", err)
    }
    defer os.Chdir(origWd)

    cm := NewConfigManager("", &Config{})

    webfrontCfg := &WebfrontConfig{
        Enabled: true,
        Port:    "8080",
    }

    // Integration test - requires DISCORD_TOKEN
    t.Skip("requires DISCORD_TOKEN - integration test")
}
```

```go
// main_test.go - TestNewBot_MissingDistDirectory
func TestNewBot_MissingDistDirectory(t *testing.T) {
    tmpDir := t.TempDir()

    origWd, _ := os.Getwd()
    if err := os.Chdir(tmpDir); err != nil {
        t.Fatalf("Failed to change directory: %v", err)
    }
    defer os.Chdir(origWd)

    cm := NewConfigManager("", &Config{})

    webfrontCfg := &WebfrontConfig{
        Enabled: true,
        Port:    "8080",
    }

    _, err := NewBot(cm, "test-token", "test-channel", nil, webfrontCfg)
    if err == nil {
        t.Error("Expected error when webfront/dist directory missing")
    }
}
```

```go
// main_test.go - TestStart_ErrorChannel
func TestStart_ErrorChannel(t *testing.T) {
    cm := NewConfigManager("", &Config{})

    bot := &Bot{
        session:       nil,
        channelID:     "test",
        configManager: cm,
        Errors:        make(chan error, 2),
    }

    if bot.Errors == nil {
        t.Error("Errors channel should not be nil")
    }

    if cap(bot.Errors) != 2 {
        t.Errorf("Errors channel capacity should be 2, got %d", cap(bot.Errors))
    }

    // Verify non-blocking send works
    select {
    case bot.Errors <- fmt.Errorf("test error"):
        // Success
    default:
        t.Error("Non-blocking send to Errors channel failed")
    }
}
```

```go
// main_test.go - TestNewBot_BackwardCompatibility
func TestNewBot_BackwardCompatibility(t *testing.T) {
    tmpDir := t.TempDir()
    distPath := filepath.Join(tmpDir, "webfront", "dist")
    if err := os.MkdirAll(distPath, 0755); err != nil {
        t.Fatalf("Failed to create dist directory: %v", err)
    }

    origWd, _ := os.Getwd()
    if err := os.Chdir(tmpDir); err != nil {
        t.Fatalf("Failed to change directory: %v", err)
    }
    defer os.Chdir(origWd)

    cm := NewConfigManager("", &Config{})

    apiCfg := &APIConfig{
        Port:           "3001",
        BearerToken:    strings.Repeat("a", 32),
        CorsOrigins:    []string{"https://example.com"},
        TrustedProxies: []string{},
    }

    webfrontCfg := &WebfrontConfig{
        Enabled: false,
        Port:    "8080",
    }

    t.Skip("requires DISCORD_TOKEN - integration test")
}
```

---

## Verification

After implementing all milestones:

```bash
# Build verification
go build -o bot .

# Unit tests
go test -v -run TestNewBot ./...
go test -v -run TestStart_ErrorChannel ./...

# Integration tests (requires DISCORD_TOKEN)
go test -v -run TestNewBot_WebfrontEnabled ./...
go test -v -run TestNewBot_BackwardCompatibility ./...
```

---

## Invariants

- Bot.apiServer and Bot.webfrontServer are nil when disabled
- Discord session must open before API/webfront servers start
- Graceful shutdown order: cancel contexts -> wait for servers -> close Discord session
- Error channel is buffered (size 2) and non-blocking for goroutine startup

---

## Tradeoffs

- Config structs add indirection but reduce parameter explosion
- Error channel requires consumer but enables failure detection
- Dist validation at startup adds init time but prevents silent runtime failures
