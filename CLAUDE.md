# AC Discord Bot with REST API

Discord bot for monitoring Assetto Corsa racing servers with dynamic configuration reloading and optional REST API for runtime configuration management.

## Files

| File | What | When to read |
| ---- | ---- | ------------ |
| `README.md` | Complete documentation: architecture, deployment, migration guide, troubleshooting, operational procedures, REST API usage | Understanding how the bot works, deploying, debugging issues, learning config reload design |
| `main.go` | Monolithic bot implementation: types, config loading (with dynamic reload), server fetching, Discord integration, optional REST API server, update loop | Understanding architecture, modifying behavior, adding features |
| `main_test.go` | Unit tests for config validation, ConfigManager, and reload behavior | Verifying changes, adding tests, debugging reload logic |
| `config.json.example` | Template for server configuration | Setting up new deployment, understanding config schema |
| `Containerfile` | Container image definition with Go static binary | Building containers, deployment, understanding runtime |
| `PODMAN.md` | Podman-specific deployment instructions and examples | Deploying with Podman, understanding container setup |
| `go.mod` | Go module dependencies and version pinning | Updating dependencies, checking versions |
| `.gitignore` | Git ignore patterns (binaries, config files, IDE files) | Understanding what's excluded from version control |
| `.env.example` | Template for environment variables (DISCORD_TOKEN, CHANNEL_ID, API settings) | Setting up local development, configuring deployment |
| `SECURITY.md` | Security guide: incident response, credential rotation, pre-release checklist | Understanding security procedures, responding to incidents |
| `CODEBASE_ANALYSIS.md` | Detailed codebase analysis and architecture documentation | Understanding project structure, security considerations |
| `test_cleanup.sh` | Script for cleaning up test resources | Running test cleanup, managing test artifacts |
| `test_api_token_fails.sh` | Startup security integration test for API_BEARER_TOKEN validation | Verifying fail-fast token validation, testing security requirements |
| `test_cors_fails.sh` | Startup security integration test for CORS wildcard validation | Verifying CORS fail-fast behavior, testing production safety checks |

## Subdirectories

| Directory | What | When to read |
| --------- | ---- | ------------ |
| `.github/workflows/` | CI/CD pipeline for automated container builds and security scanning | Understanding release process, modifying build workflow, setting up CI |
| `api/` | HTTP API server with middleware chain, config endpoints, security layers, embedded admin frontend | Understanding API architecture, modifying endpoints, security hardening, admin UI serving |
| `api/web/admin/` | Embedded admin frontend: login/config editor SPA with vanilla JS | Understanding admin UI, modifying frontend behavior, security design |
| `pkg/` | Shared packages for internal reuse | Understanding shared components |
| `pkg/proxy/` | Reverse proxy for browser-based API access via HTTP Basic Auth | Understanding proxy architecture, modifying auth/forwarding behavior |
| `plans/` | Working planning documents for executed features | Understanding implementation history, decision rationale for past changes |

## Build

```bash
go build -o bot .
```

## Test

```bash
go test -v ./...                         # Run all tests
go test -v -run TestConfigReload         # Test config reload specifically
go test -v -run TestConfigManager        # Test ConfigManager behavior
go test -v ./api/...                     # Run API package tests
go test -v ./api/ -run TestBearerAuth    # Test authentication middleware
go test -v ./api/ -bench=. -benchmem     # Run benchmarks
```

## Development

**Environment setup:**
```bash
go mod download                           # Install dependencies

# Required for Discord bot
export DISCORD_TOKEN="your_token"
export CHANNEL_ID="your_channel_id"

# Optional: Enable REST API
export API_ENABLED="true"
export API_PORT="3001"
export API_BEARER_TOKEN="your-secure-token"
export API_CORS_ORIGINS="https://example.com"
export API_TRUSTED_PROXY_IPS=""
```

**Running locally:**
```bash
go run main.go                           # Uses ./config.json
go run main.go -c /path/to/config.json   # Uses specified config
```

**Config reload testing:**
```bash
# Terminal 1: Start bot
go run main.go

# Terminal 2: Modify config
vim config.json

# Terminal 1: Watch for "Config reloaded successfully" log
```

**Formatting:**
```bash
gofmt -l .                               # Check formatting
gofmt -w .                               # Format code
```

## REST API Development

**Architecture:**
- Hybrid application: Discord bot + optional REST API server
- API enabled via `API_ENABLED=true` environment variable
- Full documentation in `api/CLAUDE.md` and `api/README.md`

**API testing:**
```bash
# Run API-specific tests
go test -v ./api/...

# Run middleware tests (auth, rate limiting, CORS)
go test -v ./api/ -run TestBearerAuth
go test -v ./api/ -run TestRateLimit

# Run E2E tests
go test -v ./api/ -run TestE2E

# Run benchmarks
go test -v ./api/ -bench=. -benchmem
```

**API package structure:**
- `api/server.go` - HTTP server lifecycle, graceful shutdown
- `api/handlers.go` - Config endpoint handlers (GET, PATCH, PUT, validate)
- `api/middleware.go` - Auth (Bearer token), rate limiting, CORS, security headers
- `api/routes.go` - Route registration
- `api/response.go` - Common response types

**Security features:**
- Constant-time Bearer token comparison (timing attack prevention)
- IP spoofing protection for rate limiting
- Configurable trusted proxy IPs
- Incremental rate limiter cleanup
- CORS and security headers

See `api/CLAUDE.md` for complete API package documentation.
