# AC Bot REST API

The `api` package provides a REST API for dynamic configuration management of the AC Discord Bot. It runs as a concurrent HTTP server alongside the Discord bot, sharing access to the same `ConfigManager`.

## Architecture

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

## Data Flow

```
HTTP Request (PATCH /api/config)
  │
  ├─► SecurityHeaders middleware
  │   └─► Set X-Content-Type-Options, X-Frame-Options, CSP headers
  │
  ├─► CORS middleware
  │   └─► Validate Origin header (if present)
  │       └─► Pass if allowed, return 403 if disallowed
  │
  ├─► Logger middleware
  │   └─► Redact Authorization header, log request with duration
  │
  ├─► RateLimit middleware
  │   └─► Check token bucket for client IP
  │       └─► Pass if under limit, return 429 if exceeded
  │
  ├─► BearerAuth middleware
  │   └─► Validate Authorization header
  │       └─► Pass if valid, return 401 if invalid
  │
  ├─► Handler (PatchConfig)
  │   ├─► Decode request body
  │   ├─► Call ConfigManager.UpdateConfig()
  │   │   ├─► 1. Read existing config from file
  │   │   ├─► 2. Create backup: config.json.backup
  │   │   ├─► 3. Merge partial update with existing config
  │   │   ├─► 4. Validate merged config
  │   │   ├─► 5. Atomic write: temp file + rename
  │   │   └─► 6. Trigger config reload (mtime change)
  │   └─► Write JSON response
  │
  └─► JSON Response
      ├─► 200 OK with updated config
      └─► 400/500 on error with details
```

**Why this flow**: Each middleware layer is independently testable and can be composed. The backup-then-atomic-write pattern ensures bot never sees corrupted config.

## Invariants

1. **Config consistency**: All config reads (Discord bot and HTTP API) see a complete, valid config via atomic.Value. Never partial state.
2. **Validation uniformity**: API and file reload use identical validation logic (`validateConfigStructSafeRuntime`). No special cases.
3. **Write atomicity**: Config file is never partially written. Temp file + rename ensures all-or-nothing updates.
4. **Backup availability**: Every write creates a `.backup` file. Failed updates can be manually rolled back.
5. **Goroutine independence**: HTTP server and Discord bot run in separate goroutines. Neither can block the other.
6. **Mtime-based reload**: File writes trigger reload via modification time change (existing 30-second polling cycle).

## Tradeoffs

**Memory vs. simplicity**:
- Full config duplicated during atomic swap (~1KB overhead)
- Tradeoff accepted: Negligible memory cost eliminates complex partial update logic

**Port conflict flexibility vs. hardening**:
- Configurable `API_PORT` via environment variable
- Tradeoff accepted: Different deployment environments need runtime flexibility; container orchestration can enforce port restrictions

**Bearer token simplicity vs. OAuth2 complexity**:
- Static Bearer token (not full OAuth2 flow)
- Tradeoff accepted: Bot runs in trusted network; full OAuth2 adds token refresh, revocation, and client credential management complexity

**Rate limiting per-IP vs. per-token**:
- Token bucket per client IP
- Tradeoff accepted: Multiple admins may share same token; IP-based limiting prevents single misconfigured client from DoSing the API

## Usage

### Starting the API Server

The API server is controlled by environment variables:

```bash
# .env file
API_ENABLED=true
API_PORT=3001
API_BEARER_TOKEN=your-secure-token-here
API_CORS_ORIGINS=https://example.com,https://app.com
```

When `API_ENABLED=true`, the main bot automatically starts the HTTP server in a background goroutine.

### API Endpoints

All endpoints (except `/health`) require Bearer token authentication:

```bash
# Set your token
export API_TOKEN="your-secure-token-here"

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

## Testing

The package includes comprehensive tests:

- **Unit tests**: Middleware, handlers, response types
- **Integration tests**: HTTP server lifecycle, graceful shutdown
- **E2E tests**: Full request flows with real HTTP client

```bash
# Run all API tests
go test -v ./api/...

# Run only E2E tests
go test -v ./api/... -run TestE2E
```
