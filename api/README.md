# API Package

REST API for dynamic configuration management of the AC Discord Bot. Provides HTTP endpoints for reading, updating, and validating bot configuration at runtime without restarting the service.

## Architecture

### Component Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         Bot Struct                              │
│  ┌─────────────┐  ┌──────────────┐  ┌─────────────────────────┐│
│  │ DiscordGo  │  │ConfigManager │  │   API Server (optional)  ││
│  │   Session   │  │              │  │  ┌────────┐  ┌────────┐ ││
│  └─────────────┘  └──────────────┘  │  │Middleware│  │Handlers│ ││
│         │                 │          │  └────────┘  └────────┘ ││
│         └─────────────────┼──────────┴─────────────────────────┘│
│                           ▼                                       │
│                    Shared State (RWMutex)                        │
│                   - Current Config                               │
│                   - Server IPs                                    │
└─────────────────────────────────────────────────────────────────┘
```

**Component relationships:**
- Bot owns all dependencies (Discord session, ConfigManager, optional API server)
- ConfigManager owns config state with RWMutex for concurrent access
- API server shares ConfigManager reference with Discord bot
- No global state - all dependencies injected via constructors

**Data flow:**
1. Bot.Start() launches Discord bot and optional API server concurrently
2. API handlers call ConfigManager methods (GetConfig, UpdateConfig, WriteConfig)
3. ConfigManager serializes writes with RWMutex, updates in-memory config
4. Discord bot reads config via ConfigManager.GetConfig() (RLock)
5. File watcher (ConfigManager) detects mtime changes, reloads with debounce

### API Server Architecture

```
HTTP Request
    │
    ▼
┌──────────────────────────────────────────────────────────┐
│ Middleware Chain (order matters)                         │
│  1. SecurityHeaders    - outermost (applies to all)     │
│  2. CORS              - cross-origin checks              │
│  3. Logger            - request logging                  │
│  4. RateLimit         - throttling before expensive auth │
│  5. BearerAuth        - innermost (token validation)     │
└──────────────────────────────────────────────────────────┘
    │
    ▼
┌──────────────────────────────────────────────────────────┐
│ Route Handler (handlers.go)                              │
│  - Context cancellation check                            │
│  - Request size limit (1MB)                              │
│  - JSON decode                                           │
│  - ConfigManager method call                             │
└──────────────────────────────────────────────────────────┘
    │
    ▼
┌──────────────────────────────────────────────────────────┐
│ ConfigManager (main.go)                                  │
│  - RWMutex for concurrent access                         │
│  - Deep merge for partial updates                        │
│  - Atomic file write with backup rotation                │
└──────────────────────────────────────────────────────────┘
```

## Middleware Layers

### Security Headers (Outermost)
Applied to all responses including errors. Prevents XSS, clickjacking, MIME sniffing.

**Headers:**
- `X-Content-Type-Options: nosniff` - Prevents MIME type sniffing
- `X-Frame-Options: DENY` - Prevents clickjacking
- `X-XSS-Protection: 1; mode=block` - XSS protection for legacy browsers
- `Content-Security-Policy: default-src 'self'` - Restricts content sources
- `Referrer-Policy: strict-origin-when-cross-origin` - Controls referrer information

### CORS (Second Layer)
Cross-Origin Resource Sharing checks before authentication.

**Behavior:**
- Empty origin list = no CORS headers (same-origin only)
- `"*"` = allow all origins (development only)
- Specific origins = strict allowlist validation
- Rejects `"*"` combined with specific origins (ambiguous security policy)

**Preflight requests:** Returns 204 No Content with allowed methods and headers.

### Logger (Third Layer)
Logs all requests with method, path, status code, and duration.

**Redaction:** Authorization header replaced with `Bearer <redacted>` before logging.

### Rate Limiting (Fourth Layer)
Token bucket rate limiting per client IP before expensive authentication.

**Algorithm:**
- 10 requests/second default (configurable via `API_RATE_LIMIT` env var)
- Burst of 20 default (configurable via `API_RATE_BURST` env var)
- Per-IP limiters with 5-minute expiration
- Health check `/health` bypasses rate limiting

**IP extraction:**
- Uses `RemoteAddr` by default
- Extracts rightmost IP from `X-Forwarded-For` header (trusts last proxy, not client)
- Prevents IP spoofing bypass attempts

**Memory management:** Inline cleanup on each request removes limiters inactive for >5 minutes.

### Bearer Authentication (Innermost)
Validates OAuth2 Bearer tokens per RFC 6750.

**Timing-safe comparison:** Uses `crypto/subtle.ConstantTimeCompare` to prevent timing attack vectors where attacker measures response time to guess token byte-by-byte.

**Health check bypass:** `/health` endpoint requires no authentication.

## Configuration Endpoints

### GET /health
Public health check endpoint. Returns 200 OK if API server is running.

**Response:**
```json
{
  "status": "ok",
  "service": "ac-bot-api"
}
```

### GET /api/config
Returns current bot configuration.

**Authentication:** Required
**Response:** Full config object

### GET /api/config/servers
Returns only the servers list from current configuration.

**Authentication:** Required
**Response:** Servers array

### PATCH /api/config
Applies partial configuration update (deep merge).

**Authentication:** Required
**Request body:** JSON with partial config fields
**Response:** Updated full config

**Merge behavior:**
- Top-level fields: merged recursively
- `servers` array: merge by server name (preserves existing servers)
- New servers appended to array
- Existing servers updated by matching name

### PUT /api/config
Replaces entire configuration.

**Authentication:** Required
**Request body:** JSON with complete config
**Response:** Updated full config

### POST /api/config/validate
Validates configuration without applying it.

**Authentication:** Required
**Request body:** JSON config to validate
**Response:**
```json
{
  "valid": true,
  "message": "Config JSON is valid (full validation requires ConfigManager type)"
}
```

## Invariants

### Rate Limiter Cleanup
Limiters expire after 5 minutes of inactivity. Inline cleanup on each request scans and removes stale entries. Prevents unbounded memory growth from IP spoofing attacks.

### Atomic Write Guarantees
Config writes use temp file + atomic rename. Either entire config written or nothing. Prevents partial writes on crash/power loss.

### Backup Rotation
Always maintains 3 backup versions:
- `.backup` = most recent
- `.backup.1` = second most recent
- `.backup.2` = third most recent
- `.backup.3` = oldest (deleted on rotation)

Rotate before writing new backup (3→4 delete, 2→3, 1→2, current→1).

### Request Size Limits
All request bodies limited to 1MB maximum. Checked via `Content-Length` header before JSON decode. Returns 413 Payload Too Large if exceeded. Prevents memory exhaustion DoS.

### Context Cancellation
All handlers check `r.Context().Err()` before processing. Return 503 Service Unavailable if context cancelled (client disconnect or server shutdown). Respects graceful shutdown.

### Config Consistency
All servers in config must have IP field set to `config.ServerIP`. Enforced by `initializeServerIPs()` called after every config load. Required by HTTP query logic in Discord bot.

## Design Decisions

### Why Bot Struct with Dependency Injection
Package-level globals (`apiServer`, `discordToken`) make testing impossible. Cannot inject mocks or test doubles. Lifecycle management is implicit. Bot struct encapsulates all dependencies, constructor injection enables tests with proper lifecycle control.

### Why Shared ConfigManager
Both Discord bot and API need access to current config. Duplicate copies would diverge on updates. RWMutex allows concurrent reads (Discord bot polls config) while serializing writes (API updates).

### Why Debounce on File Reload
Editors write config in multiple bursts (vim creates .swp, writes, deletes). Without debounce, each write triggers reload. 100ms debounce coalesces rapid writes into single reload.

### Why touchConfigFile After Atomic Write
File watcher uses mtime to detect changes. `atomicWrite()` uses temp file + rename. Some filesystems preserve mtime across rename. Explicit `Chtimes()` ensures reload triggers.

### Why Timing-Safe Token Comparison
String comparison short-circuits on first mismatch. Attacker measures response time to guess token byte-by-byte. `crypto/subtle.ConstantTimeCompare` eliminates timing side channel.

### Why Trust Rightmost IP in X-Forwarded-For
Leftmost IP is client (can be spoofed). Rightmost IP is last proxy (trusted). Extract rightmost IP for rate limiting. Prevents IP spoofing bypass where attacker rotates leftmost IPs.

### Why 1MB Request Size Limit
Unbounded payloads cause memory exhaustion. 1MB sufficient for config.json (typical <10KB). `Content-Length` check before allocation prevents DoS via huge payloads.

### Why CORS Strict Allowlist
Wildcard `"*"` allows any origin. Attacker can craft malicious pages. Strict allowlist with `"*"` special case for local dev. Prevents cross-origin attacks. Rejects `"*"` combined with specific origins (ambiguous security policy).

### Why Middleware Chain Order
Security headers outermost (applies to all responses even on error). CORS second (cross-origin checks before auth). Logger third (logs all requests). Rate limit fourth (throttling before expensive auth). Auth innermost (validates token only after other checks pass). Ensures efficient resource use and consistent security headers.

### Why Context Cancellation in Handlers
Respects graceful shutdown. Client disconnect should not continue processing. Server shutdown should not accept new work. Early return prevents wasted resources.

## Tradeoffs

### Memory vs Correctness (Rate Limiter Cleanup)
**Cost:** Inline cleanup scan on each request adds O(n) overhead
**Benefit:** Prevents OOM from unbounded limiter map without background goroutine
**Alternative:** Fixed-size LRU cache would reject legitimate clients
**Decision:** Inline per-request cleanup balances simplicity and memory safety

### Performance vs Simplicity (Deep Merge)
**Cost:** Multiple JSON marshal/unmarshal cycles
**Benefit:** Works for arbitrary config structures without reflection
**Alternative:** Type-safe merge for known fields would be faster but brittle
**Decision:** Keep JSON-based merge but add special handling for server arrays

### Testability vs Ceremony (DI Constructor)
**Cost:** More boilerplate (Bot struct, constructors, injection)
**Benefit:** Tests can inject mocks, control lifecycle
**Alternative:** Globals with test reset functions
**Decision:** DI is idiomatic Go, worth the ceremony for testability

### Security vs Usability (CORS Strict Mode)
**Cost:** Require explicit origin allowlist
**Benefit:** Prevents cross-origin attacks
**Alternative:** Wildcard `"*"` with additional checks
**Decision:** Security is default, opt-in for specific origins

## Error Handling

### Starting the API Server

The API server is controlled by environment variables:

```bash
# .env file
API_ENABLED=true
API_PORT=3001
API_BEARER_TOKEN=your-secure-token-here
API_CORS_ORIGINS=https://example.com,https://app.com
```

### Status Codes
- `400 Bad Request` - Invalid JSON, missing fields, validation failure
- `401 Unauthorized` - Missing or invalid Bearer token
- `403 Forbidden` - Origin not in CORS allowlist
- `413 Payload Too Large` - Request body exceeds 1MB limit
- `429 Too Many Requests` - Rate limit exceeded
- `500 Internal Server Error` - Server-side errors (CORS misconfiguration)
- `503 Service Unavailable` - Request cancelled (context done)

## Security Considerations

### Token Storage
Bearer token read from `API_BEARER_TOKEN` environment variable. Never logged (redacted in middleware). Should be stored securely (e.g., Kubernetes secrets, Vault).

### CORS Configuration
Development: `"*"` wildcard for local testing
Production: Explicit origin allowlist (e.g., `https://admin.example.com`)
Never combine `"*"` with specific origins (ambiguous security policy)

# Get current config
curl -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:3001/api/config

# Get servers only
curl -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:3001/api/config/servers

# Partial update (PATCH)
curl -X PATCH \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"update_interval": 120}' \
  http://localhost:3001/api/config

# Full replacement (PUT)
curl -X PUT \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d @config.json \
  http://localhost:3001/api/config

# Validate without applying
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d @config.json \
  http://localhost:3001/api/config/validate

# Health check (no auth required)
curl http://localhost:3001/health
```

1. Context cancellation signal received
2. HTTP server calls `Shutdown()` with 30-second timeout
3. In-flight requests allowed up to 30 seconds to complete
4. No new requests accepted
5. Handler goroutines complete
6. Server exits cleanly

Handlers respect context cancellation by checking `r.Context().Err()` and returning early if cancelled.
