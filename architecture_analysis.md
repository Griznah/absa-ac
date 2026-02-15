# Architecture Code Smells Analysis

**Category:** Architecture
**Scope:** Entire Codebase
**Analysis Date:** 2026-02-13
**Reference:** 06-module-and-dependencies.md:1248-1288

## Executive Summary

This analysis reveals significant architecture code smells in the codebase, primarily centered around violations of the Single Responsibility Principle (SRP) and improper component boundaries. The main binary (`main.go`) acts as a central orchestrator handling Discord bot functionality, API server, configuration management, and web frontend serving, creating a tightly coupled monolithic architecture that violates clean design principles.

## Detailed Findings

### 1. Severe SRP Violations (Critical)

#### 1.1 Main Bot Binary as God Object

**Location:** `/home/bombom/repos/absa-ac-webfront/main.go`

**Issue:** The `Bot` struct violates SRP by managing multiple orthogonal concerns:
- Discord bot functionality (message updates, event handlers)
- Configuration management (loading, reloading, validation)
- API server creation and lifecycle
- Web frontend serving
- File I/O operations (config file handling)
- HTTP client operations for server polling

**Evidence (lines 753-767):**
```go
type Bot struct {
    session       *discordgo.Session
    channelID     string
    configManager *ConfigManager
    serverMessage *discordgo.Message
    messageMutex  sync.RWMutex

    // API server (optional - nil if disabled)
    apiServer *api.Server
    apiCancel context.CancelFunc

    // Webfront server (optional - nil if disabled)
    webfrontServer *http.Server
    webfrontCancel context.CancelFunc
}
```

**Impact:** Changes to any single domain (e.g., Discord API changes, API endpoint modifications, web routing changes) require modifications to the main binary, increasing risk and reducing maintainability.

#### 1.2 Configuration Manager Scope Creep

**Location:** `/home/bombom/repos/absa-ac-webfront/main.go` (lines 182-493)

**Issue:** The `ConfigManager` handles multiple responsibilities beyond basic config management:
- File watching and reloading logic
- Atomic file writing and backup rotation
- Environment variable handling
- Config validation (two different validation functions)
- Deep merge operations
- Security considerations (atomic writes, backup rotation)

**Evidence:** The ConfigManager spans 311 lines of code with methods like `atomicWrite()`, `createBackup()`, `touchConfigFile()` - functions that should belong to a separate file service or utilities package.

#### 1.3 Static File Serving in Main Package

**Location:** `/home/bombom/repos/absa-ac-webfront/main.go` (lines 1367-1426)

**Issue:** The `ServeWebfront` method implements SPA routing logic within the main binary, a concern that belongs in a dedicated web server package.

**Evidence:** SPA routing logic with filesystem checks and fallback to `index.html`:
```go
mux.Handle("/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
    // Try to serve the requested file
    path := r.URL.Path
    fullPath := filepath.Join(distPath, filepath.Clean(path))
    if _, err := os.Stat(fullPath); err == nil {
        // File exists, serve it
        fileServer.ServeHTTP(w, r)
        return
    }
    // File doesn't exist, serve index.html for SPA routing
    http.ServeFile(w, r, filepath.Join(distPath, "index.html"))
}))
```

### 2. Wrong Component Boundaries (Major)

#### 2.1 API Package Dependency on Interface

**Location:** `/home/bombom/repos/absa-ac-webfront/api/server.go` (lines 30-36)

**Issue:** The API package defines a `ConfigManager` interface instead of using the main package's implementation, creating an artificial boundary that requires adapter methods.

**Evidence:** Interface definition forcing awkward abstractions:
```go
type ConfigManager interface {
    GetConfigAny() any
    WriteConfigAny(any) error
    UpdateConfig(map[string]interface{}) error
}
```

**Impact:** The need for `anyToConfig()` and `GetConfigAny()` adapter methods (lines 668-686) demonstrates how component boundaries force unnecessary complexity.

#### 2.2 Configuration Cross-Cutting Concerns

**Location:** `/home/bombom/repos/absa-ac-webfront/main.go` throughout

**Issue:** Configuration-related concerns are scattered across multiple layers:
- Config struct definition (lines 770-776)
- Config loading logic (lines 778-828)
- Config validation (lines 863-914)
- Config management (lines 182-493)
- Config update logic (lines 336-384)

**Impact:** Changes to configuration schema require changes in multiple files, violating the DRY principle and making the system harder to maintain.

### 3. Cross-Cutting Changes for Single-Domain Features (Major)

#### 3.1 Web Frontend Integration

**Issue:** Adding web frontend functionality requires changes to the main binary:
- New Bot fields (`webfrontServer`, `webfrontCancel`)
- New environment variables (`WEBFRONT_ENABLED`, `WEBFRONT_PORT`)
- New methods (`ServeWebfront`)
- Modified constructor (`NewBot`)
- Modified start/stop logic

**Evidence (lines 1277-1284):**
```go
// Initialize webfront server struct if enabled (server started in Start())
if webfrontEnabled {
    bot.webfrontServer = &http.Server{} // Placeholder, configured in ServeWebfront
    log.Printf("Webfront server enabled on port %s", webfrontPort)
}
```

#### 3.2 API Server Integration

**Issue:** The API server is conditionally integrated, but its configuration is handled within the main package, creating tight coupling.

**Evidence (lines 1262-1278):**
```go
// Create API server if enabled
if apiEnabled {
    if apiBearerToken == "" {
        return nil, fmt.Errorf("API_ENABLED=true but API_BEARER_TOKEN is not set")
    }
    // CORS configuration...
    bot.apiServer = api.NewServer(cfgManager, apiPort, apiBearerToken, corsOrigins, apiTrustedProxies, log.Default())
}
```

### 4. Single Points of Failure without Fallback/Retry (High)

#### 4.1 Server Polling with No Retry

**Location:** `/home/bombom/repos/absa-ac-webfront/main.go` (lines 953-1016)

**Issue:** The `fetchServerInfo` function makes HTTP requests with only a 2-second timeout and no retry mechanism. If a server is temporarily unavailable, it's marked as offline without attempting recovery.

**Evidence:** Simple HTTP request with no retry logic:
```go
func fetchServerInfo(server Server) ServerInfo {
    url := fmt.Sprintf("http://%s:%d/info", server.IP, server.Port)
    ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
    defer cancel()
    // ... single request attempt
}
```

#### 4.2 Discord Connection with No Fallback

**Issue:** The Discord bot connects to a single channel with no fallback mechanism. If Discord API fails or the channel becomes unavailable, the entire system fails without recovery options.

#### 4.3 Config File with No Backup Strategy

**Issue:** While backup rotation exists for config files, there's no recovery mechanism if the config file becomes corrupted during writing. The atomic write helps, but there's no validation that the written config is actually valid.

### 5. Architectural Recommendations

#### 5.1 Microservice Separation

Recommended architecture split:
```
discord-bot/     - Pure Discord bot functionality
api-server/     - Standalone API server
web-server/     - Dedicated web frontend server
config-service/ - Centralized configuration management
```

#### 5.2 Domain-Driven Design Approach

Define clear domain boundaries:
- **Discord Domain**: Bot behavior, message formatting, event handling
- **Config Domain**: Configuration loading, validation, persistence
- **API Domain**: HTTP endpoints, middleware, request/response handling
- **Web Domain**: Static file serving, SPA routing
- **Domain Services**: Cross-cutting concerns (monitoring, health checks)

#### 5.3 Event-Driven Architecture

Implement an event bus for decoupling:
- Config change events
- Server status change events
- Discord message events
- Health check events

#### 5.4 Retry Mechanisms

Add retry policies for:
- Server polling (exponential backoff)
- Discord reconnection (with rate limiting)
- Config file operations (with validation)

#### 5.5 Configuration Management Strategy

Create a dedicated configuration service with:
- Configuration repository pattern
- Event-driven notification system
- Validation pipeline
- Backup and recovery mechanisms

### 6. Implementation Priority

1. **Critical**: Extract configuration management into separate service
2. **High**: Separate Discord bot functionality from main binary
3. **High**: Extract web serving into dedicated server package
4. **Medium**: Implement retry mechanisms for external services
5. **Medium**: Create event-driven architecture for better decoupling
6. **Low**: Add comprehensive health checks and monitoring

## Conclusion

The current architecture suffers from classic God object and cross-cutting concerns issues that make the system difficult to maintain and test. The recommended refactoring would significantly improve the system's maintainability, testability, and scalability by establishing clear boundaries between concerns.