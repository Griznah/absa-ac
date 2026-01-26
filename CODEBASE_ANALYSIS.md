# Codebase Understanding Summary

## Structure

### Monolithic Architecture with Clear Separation
The bot implements a **single-file monolithic architecture** in `main.go` (1,227 lines) with well-defined sections:

```
main.go sections:
├── Environment Loading (.env support)
├── Configuration Management (ConfigManager, validation)
├── Type Definitions (ServerInfo, Bot, Config structs)
├── HTTP Client (concurrent server fetching)
├── Discord Integration (embed building, message lifecycle)
├── Event Handlers (bot startup, shutdown)
├── Update Loop (periodic status checks)
└── Main Entry Point (initialization)
```

### Package Organization
- **Main Package**: Core bot logic in `main.go`
- **API Package** (`github.com/bombom/absa-ac/api`): Optional REST API for dynamic configuration management
- **Test Files**: `main_test.go` and `api/` tests

### Key Components
- **ConfigManager** (lines 108-249): Thread-safe configuration management with atomic operations
- **Bot** (lines 251-344): Discord session management and update coordination
- **Server Fetching** (lines 346-445): Concurrent HTTP requests to Assetto Corsa servers
- **Embed Building** (lines 447-519): Discord message formatting and categorization
- **Update Loop** (lines 984-1092): 30-second polling cycle with config reload detection

## Patterns

### Architectural Patterns

**Reader-Writer Lock Pattern**
- `sync.RWMutex` protects config reload operations
- `atomic.Value` enables lock-free concurrent reads
- Zero-copy access during server polling (critical for performance)

**Worker Pool Pattern**
- Concurrent server fetching with `sync.WaitGroup`
- 2-second timeout per server request
- Graceful degradation for failed servers

**Middleware Chain Pattern**
- Layered API middleware: security headers → CORS → logger → rate limit → auth
- Reverse order application (innermost first)
- Clean separation of concerns

**Debouncing Pattern**
- 100ms debounce timer prevents reload storms during file saves
- Timer reset on each write event
- Background execution doesn't block main loop

### Concurrency Patterns

**Atomic Operations**
- `atomic.Value.Store()` for lock-free config swaps
- Single-instruction pointer replacement
- Memory consistency guarantees via Go's atomic package

**Graceful Shutdown**
- Context-based cancellation propagation
- Wait group tracking for goroutines
- 30-second timeout for API server shutdown

**Error Containment**
- Server failures return "Offline" status instead of crashing
- Config validation failures preserve old config
- No cascading failures from component errors

## Flows

### Primary Update Loop (30-second intervals)
```
Every 30 seconds:
  1. Check config file mtime → trigger reload if changed
  2. Fetch all server info concurrently (goroutines)
  3. Build Discord embed with grouped servers by category
  4. Update status message (edit existing or recreate if deleted)
  5. Log update completion
```

**Code references**: main.go:984-1092

### Configuration Reload Flow
```
Config file modified
  → checkAndReloadIfNeeded() detects mtime change (line 161)
  → scheduleReload() starts 100ms debounce timer (line 177)
  → performReload() loads and validates new config (line 194)
  → atomic.Value.Store() atomically swaps config pointer (line 229)
  → Next update cycle uses new config seamlessly
```

**Key behaviors**:
- Double-check pattern prevents race conditions (line 206)
- Runtime validation rejects invalid configs but keeps old one
- Atomic swap ensures no partial reads during concurrent access

### Server Monitoring Flow
```
Assetto Corsa Server HTTP /info endpoint
  → JSON response: {clients, maxclients, track}
  → ServerInfo struct with offline fallback (line 52)
  → Grouped by category for embed fields (line 468)
  → Discord message with acstuff.club join links (line 503)
```

**Concurrency**: All servers queried in parallel (line 1022)

### API Request Flow (when enabled)
```
Client Request
  → Security headers middleware
  → CORS validation
  → Request logging
  → Rate limiting (10 req/sec, burst 20)
  → Bearer token authentication
  → Handler processes request
  → Atomic file write + backup creation
  → Touch file to trigger bot reload
  → Return response
```

**Code references**: api/server.go:66-71 (middleware chain)

## Decisions

### Technology Choices

**Go Language**
- Rationale: Excellent concurrency support (goroutines, channels)
- Standard library suffices (net/http, encoding/json, sync/atomic)
- Single static binary deployment

**discordgo Library**
- Mature Discord API bindings for Go
- Session management and embed support
- Message lifecycle operations

**Polling vs Event-Driven Config Reload**
- **Chosen**: File modification time polling (30s intervals)
- **Rationale**: Leverages existing update loop, no fsnotify dependency
- **Trade-off**: 100ms maximum reload latency vs reduced complexity

**Atomic Full Config Replacement**
- **Chosen**: Complete config swap via atomic.Value
- **Rationale**: Ensures consistency, eliminates merge complexity
- **Trade-off**: Cannot preserve partial edits during validation failures

### Architecture Decisions

**Monolithic Structure**
- Single 1,227-line main.go file
- **Rationale**: Simple deployment, easy navigation, clear dependencies
- **Trade-off**: Cannot split into separate packages without circular imports

**Separate Validation Modes**
- Startup validation: Fatal on errors (strict)
- Runtime validation: Log errors and continue (forgiving)
- **Rationale**: Prevent bad deployments but avoid bot crashes from typos

**Read-Only Config Access**
- Bot detects changes but never writes config
- **Rationale**: Simpler implementation, no file corruption risk
- **Trade-off**: Requires external tool (API or manual edit) for updates

**Concurrent Server Fetching**
- Parallel HTTP requests with 2-second timeouts
- **Rationale**: Responsive even if some servers are slow/down
- **Trade-off**: More complex error handling than sequential requests

### Security Decisions

**Bearer Token Authentication**
- RFC 6750 compliant for API endpoints
- **Rationale**: Industry standard, simple implementation
- **Trade-off**: Less sophisticated than OAuth2

**Rate Limiting**
- Token bucket: 10 req/sec, burst 20 per IP
- **Rationale**: Prevents DoS attacks without complex infrastructure
- **Trade-off**: Memory overhead for per-IP tracking

**Non-Root Container User**
- Runs as UID 1001 in Alpine image
- **Rationale**: Principle of least privilege
- **Trade-off**: Requires careful file permission management

## Context

### Purpose
Discord bot for monitoring Assetto Corsa racing servers with dynamic configuration reloading. Provides real-time server status updates in Discord channels with categorized server groups, player counts, track information, and direct join links.

### Constraints

**Deployment Environment**
- Container-based deployment (Podman/Docker)
- Read-only config file mounted as volume
- Environment variables for secrets (DISCORD_TOKEN, CHANNEL_ID)
- No persistent database (stateless operation)

**Operational Requirements**
- Zero-downtime config reloads
- Graceful degradation when servers are offline
- Automatic message recovery if deleted
- Clean shutdown with in-flight request completion

**Filesystem Considerations**
- Config file may be edited externally (vim, nano, etc.)
- Editor save patterns create multiple rapid writes
- Atomic writes required to prevent corruption

### Trade-offs

**Simplicity vs Extensibility**
- Chose simplicity: Single file, minimal dependencies
- Trade-off: Harder to extend with new features (e.g., database persistence)

**Performance vs Safety**
- Chose performance: Lock-free reads via atomic.Value
- Trade-off: More complex concurrency model than simple mutex

**Reliability vs Complexity**
- Chose reliability: Debouncing, atomic swaps, validation
- Trade-off: More code than naive file polling implementation

### Evolution

**Recent Changes** (from git history):
- REST API added for dynamic config management (commit d09d49b)
- Containerfile corrections for volume declarations (commit 24cf77a)
- Server IP initialization during runtime reload (commit 8e34119)

**Testing Strategy**:
- Comprehensive unit tests in main_test.go (568-2439 lines)
- Covers reload logic, concurrency, debouncing, integration scenarios
- Integration tests verify bot behavior during config changes

**Operational Maturity**:
- Production-ready with comprehensive error handling
- Container-optimized with multi-stage builds
- CI/CD pipeline via GitHub Actions
- Detailed documentation in README.md and PODMAN.md

---

## Detailed Analysis: Configuration Reload Mechanism

### Core Architecture
The bot implements a sophisticated thread-safe configuration reload system using a hybrid approach combining **atomic operations**, **read-write locks**, and **debounced file polling**.

**ConfigManager Structure** (main.go:108-118):
```go
type ConfigManager struct {
    config        atomic.Value     // stores *Config
    configPath    string
    lastModTime   time.Time
    mu            sync.RWMutex     // protects reload operations
    debounceTimer *time.Timer     // 100ms debounce timer
}
```

### Lock-Free Reading Pattern
**GetConfig()** (main.go:139-144):
- Uses `atomic.Value.Load()` for zero-copy access
- No mutex overhead during server polling
- Critical for performance as polling happens frequently

### Reload Sequence
1. **File Modification Detection** (main.go:161-192)
   - Checks modification time every update cycle
   - Schedules debounced reload if changed

2. **Debounce Implementation** (main.go:177-189)
   - 100ms delay allows file writes to settle
   - Timer reset on each new write
   - Background execution doesn't block main loop

3. **Actual Reload Process** (main.go:194-235)
   - Double-checks modification time (anti-race condition)
   - Loads and validates new config
   - Atomic swap via `atomic.Value.Store()`
   - Updates modification time on success

### Concurrency Safety
- **Readers** (server polling): Lock-free via `atomic.Value`
- **Writers** (reload operations): Serialized via `sync.RWMutex`
- **Atomic swap**: Single instruction pointer replacement
- **No torn reads**: Guaranteed by Go's atomic operations

### Error Handling
- **Malformed config**: Keep old config, log error, retry on next check
- **Rapid writes**: Debounce prevents reload storms
- **Validation failures**: Preserve old config, automatic recovery when fixed

## Detailed Analysis: REST API Implementation

### API Architecture
Located in `api/` directory as separate package with clean interface design:

**Endpoints**:
```
GET  /health                 # Health check (no auth)
GET  /api/config            # Get full configuration
GET  /api/config/servers    # Get servers list only
PATCH /api/config           # Partial config update
PUT  /api/config            # Replace entire config
POST /api/config/validate   # Validate without applying
```

### Middleware Chain (api/server.go:66-71)
Applied in reverse order (innermost first):
1. **Security Headers**: X-Content-Type-Options, X-Frame-Options, CSP
2. **CORS**: Configurable origins, preflight handling
3. **Logger**: Request/response logging with token redaction
4. **Rate Limiting**: 10 req/sec, burst 20 per IP
5. **Authentication**: Bearer token (RFC 6750)

### Config Operations

**GET /api/config**:
- Lock-free read via `atomic.Value`
- Returns entire config as JSON

**PUT /api/config**:
- Validates request body
- Atomic write pattern (temp file → rename)
- Creates backup before modification
- Touches file to trigger bot reload

**PATCH /api/config**:
- Merges partial config with existing
- Validates merged result
- Atomic write with backup

### Security Model

**Authentication**:
- Bearer token required for all endpoints except /health
- Token validation via strict prefix matching
- Detailed error responses for debugging

**Rate Limiting**:
- Token bucket algorithm using `golang.org/x/time/rate`
- Per-IP tracking prevents DoS attacks
- Health check bypasses rate limiting

**Security Headers**:
- X-Content-Type-Options: nosniff
- X-Frame-Options: DENY
- X-XSS-Protection: 1; mode=block
- Content-Security-Policy: default-src 'self'
- Referrer-Policy: strict-origin-when-cross-origin

### Integration with Bot

**Shared State**:
- Both API and bot use same ConfigManager instance
- Bot polls config file for changes
- API writes trigger file modification time updates
- Bot reloads config automatically

**Conflict Prevention**:
- ConfigManager serializes write operations via RWMutex
- Atomic.Value enables lock-free concurrent reads
- Debounce timer prevents reload storms

**Lifecycle**:
- API server runs in separate goroutine
- Bot manages graceful shutdown of API server
- 30-second timeout for in-flight requests

---

**Analysis Summary**: This codebase demonstrates excellent balance between simplicity and robustness. The monolithic architecture is well-organized with clear separation of concerns. The dynamic configuration reload mechanism is particularly well-designed, using atomic operations and debouncing to achieve zero-downtime updates without complex dependencies. The optional REST API provides modern configuration management while maintaining clean separation from the core bot functionality.
