# REST API for AC Discord Bot Config Management

## Overview

Add a REST API to the AC Discord Bot that enables dynamic configuration updates through HTTP endpoints while preserving all existing Discord bot functionality. The API will integrate with the existing ConfigManager's dynamic reload system, allowing administrators to update config.json via HTTP requests that trigger atomic file writes with backup protection.

**Chosen approach**: Create a separate `api/` package using Go stdlib `net/http` with Bearer token authentication, atomic file writes with backup, and configurable port via environment variable. This maintains the project's minimal-dependency philosophy while providing clean separation of concerns between Discord integration and HTTP API.

## Planning Context

This section is consumed VERBATIM by downstream agents (Technical Writer, Quality Reviewer). Quality matters: vague entries here produce poor annotations and missed risks.

### Decision Log

| Decision | Reasoning Chain |
| --- | --- |
| Separate `api/` package over main.go addition | Single file already 600+ lines -> HTTP API would add ~500 LOC -> mixing Discord + HTTP concerns violates single responsibility -> separate package enables isolated testing and cleaner maintenance |
| Stdlib `net/http` over gorilla/mux | Project has minimal-dependency philosophy (only discordgo) -> stdlib provides all needed routing (Go 1.22+ method-based routing) -> adding gorilla/mux increases binary size for features this simple API doesn't need (3-5 endpoints, no complex routing) |
| Bearer token over API key header | Standard OAuth2 RFC 6750 pattern -> wider ecosystem support -> easier to integrate with existing auth systems -> `Authorization: Bearer <token>` is more discoverable than custom `X-API-Key` header |
| Atomic write + backup over direct write | Config file is source of truth for running bot -> corruption requires manual recovery -> backup before write provides rollback path -> atomic rename prevents partial writes during crash/power loss |
| Configurable API_PORT over hardcoded port | Different environments may have port conflicts (dev, staging, prod) -> hardcoded port requires code change for each deployment -> environment variable enables runtime configuration without recompilation |
| Extend ConfigManager rather than duplicate | Existing ConfigManager has proven thread-safe patterns (atomic.Value + RWMutex) -> duplicating would violate DRY and risk inconsistent behavior -> adding write methods reuses validation and reload logic |
| Integration tests with real deps over mocks | ConfigManager integration is complex (file I/O, atomic swaps, debouncing) -> mocks would need to replicate this behavior -> tests using real filesystem and ConfigManager catch actual integration bugs -> property-based unit tests cover pure functions |
| Rate limiting middleware | Public-facing API could be abused -> unlimited requests enable DoS attacks -> token bucket rate limiting provides backpressure without complete blocking -> 10 req/sec per IP prevents abuse while allowing legitimate usage |
| Example-based unit tests over property-based | HTTP handlers have complex state interactions (request context, response writers) -> property-based tests struggle with stateful I/O -> example-based tests with table-driven approach provide clear coverage of success/error paths |
| Generated datasets for E2E tests | Manual fixtures become stale as config schema evolves -> generated config data ensures edge cases are tested -> deterministic generation allows replay and debugging |
| Reuse validateConfigStructSafeRuntime | Function already exists for runtime config validation -> battle-tested in production reloads -> using it ensures API validation matches file-based validation consistency -> prevents drift between API and file reload paths |

### Rejected Alternatives

| Alternative | Why Rejected |
| --- | --- |
| Add HTTP handlers directly to main.go | main.go is already 600+ lines -> mixing Discord + HTTP concerns makes file harder to understand -> violates single responsibility principle -> harder to test HTTP handlers in isolation |
| Use gorilla/mux router | Adds external dependency -> violates project's minimal-dependency philosophy -> API only needs 3-5 endpoints with simple routing -> stdlib http.ServeMux is sufficient (Go 1.22+ supports method-based routing) |
| API key via custom header | Non-standard pattern -> less discoverable than Bearer token -> requires custom documentation instead of RFC 6750 compliance |
| Direct file write without backup | Corruption during write crashes bot -> requires manual recovery from version control -> backup provides automatic rollback path for failed updates |
| Hardcoded HTTP port | Port conflicts require code change and rebuild -> environment variable enables runtime configuration -> supports different deployment environments without recompilation |
| Create parallel ConfigManager for API | Duplicates validation and reload logic -> risk of inconsistency between managers -> violates DRY principle -> existing ConfigManager already has thread-safe patterns |
| Mock-based integration tests | Mocks must replicate complex ConfigManager behavior (file I/O, atomic swaps, debouncing) -> mocks that replicate complex behavior become tests of the mock itself -> real dependencies catch actual filesystem and concurrency bugs |

### Constraints & Assumptions

**Technical**:
- Go 1.25.5 or later required
- Existing ConfigManager with atomic.Value + RWMutex must be preserved
- Dynamic config reload system must continue working (file mtime polling every 30s)
- Discord bot functionality cannot be disrupted by HTTP API

**Organizational**:
- Project follows minimal-dependency philosophy (only discordgo currently)
- Single binary deployment (no separate config service)
- Admins have access to environment variables for Bearer token and port configuration

**Dependencies**:
- github.com/bwmarrin/discordgo (existing, must not break)
- Go stdlib: net/http, context, encoding/json, time, sync, os, io (no new external deps)

**Default conventions applied** (<default-conventions domain="testing">):
- Integration tests with real dependencies (filesystem, ConfigManager, HTTP server)
- Example-based unit tests for HTTP handlers
- Property-based tests skipped (user chose example-based)

### Known Risks

| Risk | Mitigation | Anchor |
| --- | --- | --- |
| Concurrent file write corruption | Atomic write + backup strategy prevents partial writes | main.go:420-440 (performReload shows atomic pattern) |
| Rate limiting bypass | Token bucket per IP + Bearer token auth provides two layers | N/A (new implementation) |
| Config validation inconsistency | Reuse validateConfigStructSafeRuntime for both API and file reload | main.go:180-220 (validateConfigStructSafeRuntime) |
| HTTP server startup blocks bot | Start HTTP server in separate goroutine with graceful shutdown | main.go:680-700 (goroutine pattern exists for Discord) |
| Port conflicts on deployment | API_PORT environment variable configurable by admin | N/A (new implementation) |
| Bearer token exposure in logs | Redact Authorization header in request logging middleware | N/A (new implementation) |
| Race condition during config reload | ConfigManager already uses atomic.Value + RWMutex for thread-safe access | main.go:410-430 (atomic.Value.Store pattern) |

## Invisible Knowledge

This section captures knowledge NOT deducible from reading the code alone.

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         main.go                             │
│  ┌──────────────┐         ┌──────────────┐                 │
│  │ Discord Bot  │         │ HTTP Server  │                 │
│  │   (goroutine)│         │  (goroutine) │                 │
│  └──────┬───────┘         └──────┬───────┘                 │
│         │                        │                          │
│         └────────────┬───────────┘                          │
│                      │                                      │
│              ┌───────▼────────┐                             │
│              │ ConfigManager  │                             │
│              │  (atomic.Value │                             │
│              │   + RWMutex)   │                             │
│              └───────┬────────┘                             │
│                      │                                      │
│              ┌───────▼────────┐                             │
│              │  config.json   │                             │
│              │  (file watch)  │                             │
│              └────────────────┘                             │
└─────────────────────────────────────────────────────────────┘
```

**Key insight**: The HTTP API and Discord bot are concurrent peers that both access ConfigManager. Neither owns the config - they are both clients. ConfigManager is the source of truth for thread-safe access.

### Data Flow

```
HTTP Request (PATCH /api/config)
  │
  ├─► Bearer Token Auth Middleware
  │   └─► Validate Authorization header
  │       └─► Pass if valid, return 401 if invalid
  │
  ├─► Rate Limiting Middleware
  │   └─► Check token bucket for client IP
  │       └─► Pass if under limit, return 429 if exceeded
  │
  ├─► JSON Validation Middleware
  │   └─► Decode request body
  │       └─► validateConfigStructSafeRuntime()
  │
  ├─► ConfigManager.WriteConfig()
  │   ├─► 1. Read existing config from file
  │   ├─► 2. Create backup: config.json.backup
  │   ├─► 3. Merge partial update with existing config
  │   ├─► 4. Atomic write: temp file + rename
  │   └─► 5. Trigger config reload (mtime change detected)
  │
  └─► JSON Response
      ├─► 200 OK with updated config
      └─► 400/500 on error with details
```

**Why this flow**: Each middleware layer is independently testable and can be composed. The backup-then-atomic-write pattern ensures bot never sees corrupted config.

### Why This Structure

**Separate `api/` package**:
- HTTP handlers are a distinct concern from Discord bot logic
- Enables isolated testing without importing Discord dependencies
- Allows API to be optional (compile flag or environment variable)
- Prevents main.go from growing beyond maintainability (already 600+ lines)

**ConfigManager.WriteConfig() extension**:
- Existing ConfigManager has proven thread-safe patterns (atomic.Value, RWMutex)
- Reusing validation ensures API and file reload have identical rules
- Prevents divergence where API accepts configs that file reload would reject
- Centralizes all config access patterns in one place

**Bearer token over API key**:
- Standard OAuth2 RFC 6750 pattern is more discoverable
- Ecosystem tooling (curl, Postman, proxies) recognizes this format
- Future integration with external auth systems becomes simpler
- Custom X-API-Key headers require documentation for each new consumer

### Invariants

1. **Config consistency**: All config reads (Discord bot and HTTP API) see a complete, valid config via atomic.Value. Never partial state.
2. **Validation uniformity**: API and file reload use identical validation logic (validateConfigStructSafeRuntime). No special cases.
3. **Write atomicity**: Config file is never partially written. Temp file + rename ensures all-or-nothing updates.
4. **Backup availability**: Every write creates a .backup file. Failed updates can be manually rolled back.
5. **Goroutine independence**: HTTP server and Discord bot run in separate goroutines. Neither can block the other.
6. **Mtime-based reload**: File writes trigger reload via modification time change (existing 30-second polling cycle).

### Tradeoffs

**Memory vs. simplicity**:
- Full config duplicated during atomic swap (~1KB overhead)
- Tradeoff accepted: Negligible memory cost eliminates complex partial update logic

**Port conflict flexibility vs. hardening**:
- Configurable API_PORT via environment variable
- Tradeoff accepted: Different deployment environments need runtime flexibility; container orchestration can enforce port restrictions

**Bearer token simplicity vs. OAuth2 complexity**:
- Static Bearer token (not full OAuth2 flow)
- Tradeoff accepted: Bot runs in trusted network; full OAuth2 adds token refresh, revocation, and client credential management complexity

**Rate limiting per-IP vs. per-token**:
- Token bucket per client IP
- Tradeoff accepted: Multiple admins may share same token; IP-based limiting prevents single misconfigured client from DoSing the API

## Milestones

### Milestone 1: API Package Structure and HTTP Server Setup

**Files**:
- `api/server.go`
- `api/handlers.go`
- `api/middleware.go`
- `api/response.go`

**Flags**: `conformance` (ensure patterns match existing codebase), `security` (auth middleware)

**Requirements**:
- Create HTTP server with graceful shutdown on context cancellation
- Define HTTP handler interface accepting ConfigManager dependency
- Implement Bearer token authentication middleware
- Implement rate limiting middleware (10 req/sec per IP)
- Create common response types for success/error responses

**Acceptance Criteria**:
- Server starts on configured API_PORT (default 3001)
- Server gracefully shuts down when context is cancelled
- Requests without valid Bearer token return 401 Unauthorized
- Requests exceeding rate limit return 429 Too Many Requests
- All responses use consistent JSON structure

**Tests**:
- **Test files**: `api/server_test.go`, `api/middleware_test.go`
- **Test type**: Integration (real HTTP server, real middleware)
- **Backing**: user-specified (example-based chosen in planning)
- **Scenarios**:
  - Normal: Valid Bearer token returns 200 OK
  - Edge: Missing Authorization header returns 401
  - Edge: Malformed Bearer token returns 401
  - Edge: Rate limit exhausted returns 429, recovers after 1 second
  - Error: Server shuts down gracefully with in-flight requests completing

**Code Intent**:

`api/server.go`:
- New struct `Server` with fields: `cm *ConfigManager`, `httpServer *http.Server`, `logger *log.Logger`
- New function `NewServer(cm *ConfigManager, port string, bearerToken string) *Server`
- New method `Start(ctx context.Context) error` - starts HTTP server in goroutine, waits for ctx cancellation
- New method `Stop() error` - graceful shutdown with 30-second timeout
- Uses `http.ServeMux` for routing (Go 1.22+ method-based routing)

`api/middleware.go`:
- New function `BearerAuth(token string) func(http.Handler) http.Handler`
- New function `RateLimit(requestsPerSecond int, burstSize int) func(http.Handler) http.Handler`
- New function `Logger(logger *log.Logger) func(http.Handler) http.Handler`
- BearerAuth extracts `Authorization` header, validates `Bearer <token>` format
- RateLimit uses golang.org/x/time/rate token bucket per client IP

`api/response.go`:
- New struct `ErrorResponse` with fields: `Error string`, `Details string` (optional)
- New struct `SuccessResponse` with fields: `Data interface{}`
- New function `WriteJSON(w http.ResponseWriter, status int, data interface{}) error`
- New function `WriteError(w http.ResponseWriter, status int, err string, details string) error`

`api/handlers.go`:
- Placeholder handler `HealthCheck(w http.ResponseWriter, r *http.Request)` for /health endpoint
- Handler accepts `*ConfigManager` dependency via struct field or closure

### Milestone 2: ConfigManager Write Methods

**Files**:
- `main.go` (extend ConfigManager)

**Flags**: `conformance` (match existing ConfigManager patterns), `error-handling` (file I/O failures)

**Requirements**:
- Add `WriteConfig(newConfig *Config) error` method to ConfigManager
- Add `UpdateConfig(partialConfig map[string]interface{}) error` method
- Implement backup-before-write strategy (copy to config.json.backup)
- Implement atomic write pattern (temp file + rename)
- Trigger config reload by updating file modification time

**Acceptance Criteria**:
- WriteConfig creates backup file before any modifications
- WriteConfig uses atomic temp-file-then-rename pattern
- WriteConfig returns error if validation fails (config unchanged)
- UpdateConfig merges partial changes with existing config
- Both methods trigger reload via file mtime change
- Concurrent writes are serialized (RWMutex write lock)

**Tests**:
- **Test files**: `main_test.go` (extend existing tests)
- **Test type**: Integration (real filesystem, real ConfigManager)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Valid config write creates backup and updates file
  - Normal: Partial update merges with existing config
  - Edge: Concurrent writes are serialized (no corruption)
  - Edge: Backup file exists after failed write attempt
  - Error: Invalid config returns error without modifying existing file
  - Error: Filesystem read-only returns error, original config preserved

**Code Intent**:

Extend `ConfigManager` struct in `main.go`:
- Add method `WriteConfig(newConfig *Config) error`
- Add method `UpdateConfig(partial map[string]interface{}) error`
- Add method `createBackup() error` - copies config.json to config.json.backup
- Add method `atomicWrite(data []byte) error` - writes to temp file, then os.Rename
- Both methods acquire `mu.Lock()` for write serialization
- Both methods call `validateConfigStructSafeRuntime()` before write
- `WriteConfig` calls `createBackup()` then `atomicWrite(json.Marshal(newConfig))`
- `UpdateConfig` reads current config, merges partial, calls `WriteConfig`
- After successful write, call `os.Chtimes()` to update mtime (triggers reload)

### Milestone 3: Config API Endpoints

**Files**:
- `api/handlers.go` (extend)
- `api/routes.go` (new)

**Flags**: `security` (input validation, auth), `needs-rationale` (endpoint design)

**Requirements**:
- GET /api/config - retrieve current configuration
- GET /api/config/servers - retrieve server list only
- PATCH /api/config - partial config update
- PUT /api/config - full config replacement
- POST /api/config/validate - validate config without applying

**Acceptance Criteria**:
- GET /api/config returns 200 with full config JSON
- GET /api/config/servers returns 200 with servers array only
- PATCH /api/config merges changes, returns 200 with updated config
- PUT /api/config replaces entire config, returns 200 with updated config
- POST /api/config/validate validates and returns 200 with validation result
- All endpoints require valid Bearer token
- All endpoints return 400 on malformed JSON
- All endpoints return 400 on validation failure with error details
- All endpoints return 500 on internal error

**Tests**:
- **Test files**: `api/handlers_test.go`
- **Test type**: Integration (real HTTP server, real ConfigManager, real filesystem)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: GET /api/config returns current config
  - Normal: PATCH /api/config with valid partial update merges changes
  - Normal: PUT /api/config with valid full config replaces all fields
  - Normal: POST /api/config/validate with valid config returns success
  - Edge: PATCH with empty body returns 400
  - Edge: PATCH with invalid field type returns 400 with details
  - Error: Invalid config returns 400 with validation error message
  - Error: Missing Bearer token returns 401
  - Error: Concurrent PATCH requests serialize correctly

**Code Intent**:

`api/handlers.go`:
- Add handler `GetConfig(w http.ResponseWriter, r *http.Request)` - reads from `cm.GetConfig()`, writes JSON response
- Add handler `GetServers(w http.ResponseWriter, r *http.Request)` - extracts servers array, writes JSON
- Add handler `PatchConfig(w http.ResponseWriter, r *http.Request)` - decodes body, calls `cm.UpdateConfig()`, writes result
- Add handler `PutConfig(w http.ResponseWriter, r *http.Request)` - decodes body, calls `cm.WriteConfig()`, writes result
- Add handler `ValidateConfig(w http.ResponseWriter, r *http.Request)` - decodes body, calls `validateConfigStructSafeRuntime()`, writes validation result
- All handlers use `WriteError()` for error responses
- All handlers use `WriteJSON()` for success responses

`api/routes.go`:
- Add function `RegisterRoutes(mux *http.ServeMux, s *Server, authMiddleware, rateLimitMiddleware, loggerMiddleware http.Handler)`
- Register GET /health -> HealthCheck (no auth required)
- Register GET /api/config -> GetConfig (with auth + rate limit)
- Register GET /api/config/servers -> GetServers (with auth + rate limit)
- Register PATCH /api/config -> PatchConfig (with auth + rate limit)
- Register PUT /api/config -> PutConfig (with auth + rate limit)
- Register POST /api/config/validate -> ValidateConfig (with auth + rate limit)

### Milestone 4: CORS and Security Headers

**Files**:
- `api/middleware.go` (extend)

**Flags**: `security` (CORS, headers)

**Requirements**:
- Implement CORS middleware with configurable origins
- Add security headers (X-Content-Type-Options, X-Frame-Options, etc.)
- Make CORS configurable via environment variable

**Acceptance Criteria**:
- CORS headers present on OPTIONS requests
- Preflight requests return appropriate CORS headers
- Security headers present on all responses
- CORS origins configurable via API_CORS_ORIGINS environment variable

**Tests**:
- **Test files**: `api/middleware_test.go` (extend)
- **Test type**: Integration (real HTTP server)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: OPTIONS request returns CORS headers
  - Edge: Request from disallowed origin returns 403
  - Edge: Missing Origin header is handled gracefully
  - Normal: Security headers present on response

**Code Intent**:

`api/middleware.go`:
- Add function `CORS(allowedOrigins []string) func(http.Handler) http.Handler`
- Check `Origin` header, if present validate against allowedOrigins
- Set headers: `Access-Control-Allow-Origin`, `Access-Control-Allow-Methods`, `Access-Control-Allow-Headers`
- Handle OPTIONS preflight requests
- Add function `SecurityHeaders() func(http.Handler) http.Handler`
- Set headers: `X-Content-Type-Options: nosniff`, `X-Frame-Options: DENY`, `Content-Security-Policy: default-src 'self'`

### Milestone 5: Environment Variable Configuration

**Files**:
- `main.go` (modify)

**Flags**: `conformance` (match existing DISCORD_TOKEN pattern)

**Requirements**:
- Add API_PORT environment variable (default 3001)
- Add API_BEARER_TOKEN environment variable (required if API enabled)
- Add API_CORS_ORIGINS environment variable (optional, comma-separated list)
- Add API_ENABLED environment variable (optional, default false)

**Acceptance Criteria**:
- API_PORT defaults to 3001 if not set
- API_BEARER_TOKEN is required when API_ENABLED=true
- API_CORS_ORIGINS defaults to empty (no CORS) if not set
- API_ENABLED defaults to false (API not started)
- Missing API_BEARER_TOKEN when API_ENABLED=true causes startup error

**Tests**:
- **Test files**: `main_test.go` (extend)
- **Test type**: Integration (real environment variables)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: API starts with all required env vars set
  - Edge: API_PORT uses default when not set
  - Error: Missing API_BEARER_TOKEN returns error
  - Normal: API_ENABLED=false skips HTTP server start

**Code Intent**:

`main.go`:
- Add environment variable reads in `main()` function alongside existing `DISCORD_TOKEN` and `CHANNEL_ID`
- Add `API_ENABLED` (default "false") - if false, skip HTTP server start
- Add `API_PORT` (default "3001") - HTTP server listen address
- Add `API_BEARER_TOKEN` (required if API_ENABLED=true) - Bearer token for auth
- Add `API_CORS_ORIGINS` (optional, comma-separated) - CORS allowed origins
- Add validation: if API_ENABLED=true and API_BEARER_TOKEN is empty, call `log.Fatalf`
- Pass env vars to `api.NewServer()` call
- Start HTTP server in goroutine: `go apiServer.Start(ctx)`
- Add graceful shutdown in `Bot.WaitForShutdown()`: call `apiServer.Stop()`

### Milestone 6: End-to-End Integration Tests

**Files**:
- `api/e2e_test.go`

**Flags**: None

**Requirements**:
- Create end-to-end tests covering full request flow
- Test config update flow from HTTP request to file write to ConfigManager reload
- Use generated config datasets for deterministic testing

**Acceptance Criteria**:
- Full flow test: HTTP PATCH -> file write -> backup creation -> ConfigManager reload
- Generated config data covers edge cases (empty arrays, max values, unicode)
- Tests are deterministic (same input produces same output)

**Tests**:
- **Test files**: `api/e2e_test.go`
- **Test type**: E2E (generated datasets)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Full config update flow (PATCH -> file -> reload -> Discord uses new config)
  - Edge: Generated config with 1000 servers (stress test)
  - Edge: Generated config with unicode category names and emojis
  - Error: Config file becomes corrupted during write (simulate crash)

**Code Intent**:

`api/e2e_test.go`:
- Add helper function `generateConfig(numServers int) *Config` - generates deterministic config data
- Add test `TestE2E_ConfigUpdateFlow` - starts HTTP server, sends PATCH request, verifies file written, backup exists, ConfigManager reloaded
- Add test `TestE2E_LargeConfig` - generates config with 1000 servers, verifies update succeeds
- Add test `TestE2E_UnicodeConfig` - generates config with unicode strings, verifies round-trip
- Use `t.TempDir()` for test config directory
- Use real HTTP client to make requests to test server
- Verify file contents directly with `os.ReadFile`

### Milestone 7: Documentation

**Delegated to**: @agent-technical-writer (mode: post-implementation)

**Source**: `## Invisible Knowledge` section of this plan

**Files**:
- `api/CLAUDE.md` (package index)
- `api/README.md` (architecture and usage guide)
- `README.md` (update root documentation with API section)
- `.env.example` (add new API environment variables)

**Requirements**:

Delegate to Technical Writer. For documentation format specification:

Key deliverables:
- `api/CLAUDE.md`: Tabular index of API package files
- `api/README.md`: Architecture diagrams, data flow, invariants, tradeoffs
- Root `README.md`: New section documenting REST API usage and configuration
- `.env.example`: Add API_PORT, API_BEARER_TOKEN, API_ENABLED, API_CORS_ORIGINS with comments

**Acceptance Criteria**:
- `api/CLAUDE.md` is tabular index only (no prose sections)
- `api/README.md` exists with architecture diagrams matching this plan
- Root `README.md` has API section with curl examples
- `.env.example` documents all new environment variables

**Source Material**: `## Invisible Knowledge` section of this plan

## Milestone Dependencies

```
M1 (API Package Structure)
    |
    v
M2 (ConfigManager Write Methods)
    |
    v
M3 (Config API Endpoints) <-----> M4 (CORS/Security)
    |
    v
M5 (Env Var Config)
    |
    v
M6 (E2E Tests)
    |
    v
M7 (Documentation)
```

Parallel opportunities:
- M4 can run in parallel with M3 (independent middleware)
- M5 can run in parallel with M3/M4 (env vars are independent of handler logic)
- M6 must wait for M3 and M5 (needs handlers and env var configuration)
- M7 must wait for all implementation milestones (documents final implementation)
