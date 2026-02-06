# Plan: Remove Proxy and Static File Serving Functionality

**Created:** 2026-02-05
**Status:** Draft (pending quality review)

## Overview

### Problem
Remove all proxy and static file serving functionality from the codebase. The proxy package provides session-based authentication with AES-256-GCM token encryption and CSRF protection. The static directory contains an Alpine.js web UI that connects to the proxy for secure session-based access. Both are unreleased features with no backward compatibility concerns.

### Approach
Delete entire `pkg/proxy/` and `static/` directories, remove proxy-related code from `main.go` and `api/routes.go`, and update documentation. The removal is straightforward because no other code depends on these packages.

## Decision Log

| ID | Decision | Reasoning |
|----|----------|-----------|
| DL-001 | Remove all proxy and static file serving functionality | Proxy is unreleased feature with no backward compatibility concerns → Direct removal is safe → Static files depend on proxy for session-based auth → No remaining use case for static serving after proxy removal |
| DL-002 | Delete pkg/proxy/ directory entirely | Proxy package contains only session-based auth layer → No other code depends on it → Complete deletion simpler than partial removal |
| DL-003 | Delete static/ directory entirely | Static frontend requires proxy for session auth → Alpine.js web UI becomes unusable without proxy → No equivalent Bearer-token-authenticated frontend exists |
| DL-004 | Remove proxy-related environment variables from main.go | PROXY_ENABLED, PROXY_PORT, PROXY_HTTPS, PROXY_UPSTREAM_TIMEOUT no longer serve purpose → API_BEARER_TOKEN remains for REST API → Cleaner configuration reduces confusion |
| DL-005 | Remove static file serving from api/routes.go | /static/ route serves same files as proxy root path → Proxy removal eliminates session-based auth for static files → API only needs Bearer auth endpoints → Removing unused routes simplifies API surface |
| DL-006 | Remove api/static_test.go entirely | Tests verify static file serving behavior → Static serving being removed → Tests would all fail → No alternative test scenarios without proxy |

## Risks and Mitigations

| ID | Risk | Mitigation | Anchor |
|----|------|------------|--------|
| R-001 | Removing proxy functionality may break existing deployments that have PROXY_ENABLED=true | Proxy was explicitly stated as unreleased feature. Document removal in release notes and migration guide. | main.go:L1572-L1608 |
| R-002 | Deleting pkg/proxy/ may leave orphaned import statement | Verify build succeeds after removal with 'go build ./...' | main.go:L23 |
| R-003 | Tests may reference proxy functionality in main_test.go | Remove or update proxy-related tests (TestProxyServer_*) | main_test.go:L2287-L2706 |

## Invariants

- Discord bot must continue to function without any changes
- REST API must continue to work with Bearer token authentication
- All API endpoints (/health, /api/config, /api/config/servers, PATCH /api/config, PUT /api/config, POST /api/config/validate) must remain functional
- go build must succeed after all removals
- All tests must pass after proxy-related tests are removed

## Tradeoffs

- Chose complete deletion over deprecation period → Proxy unreleased so no users affected → Simpler codebase
- Chose removing static files entirely → Static UI depends on proxy auth → No Bearer-authenticated replacement exists
- Chose removing proxy tests from main_test.go → Tests verify removed functionality → Keeping would cause build failures

## Implementation Waves

### Wave 1: Code cleanup in main.go

**Milestones:** M-001, M-002, M-003, M-004, M-005, M-012

#### M-001: Remove proxy import and variables from main.go
- Remove `"github.com/bombom/absa-ac/pkg/proxy"` import (line 23)
- Remove `proxyEnabled` and `proxyPort` global variables (lines 171-173)
- Remove `apiTrustedProxyList` local variable from proxy config section (lines 1506-1562)

**Acceptance Criteria:**
- go build succeeds without proxy package import
- No references to proxyEnabled or proxyPort in main.go

#### M-002: Remove proxy server initialization and startup
- Remove `PROXY_ENABLED` environment variable read (line 1573)
- Remove `PROXY_PORT` environment variable read (lines 1574-1577)
- Remove `PROXY_HTTPS` environment variable read (line 1579)
- Remove `PROXY_UPSTREAM_TIMEOUT` parsing (lines 1582-1593)
- Remove proxy configuration validation block (lines 1595-1608)
- Remove `startProxyServer()` call (lines 1638-1644)

**Acceptance Criteria:**
- No proxy environment variables read in main()
- No startProxyServer() call in main()
- Binary starts without proxy configuration

#### M-003: Remove proxy server shutdown logic
- Remove proxy server shutdown code from `WaitForShutdown` method (lines 1330-1343)

**Acceptance Criteria:**
- WaitForShutdown() contains no proxy-related shutdown code
- Graceful shutdown works for Discord bot and API server

#### M-004: Remove Bot struct proxy fields
- Remove `proxyServer *http.Server` field (lines 765-768)
- Remove `proxyCancel context.CancelFunc` field (lines 765-768)
- Remove `proxyStore *proxy.SessionStore` field (lines 765-768)

**Acceptance Criteria:**
- Bot struct contains no proxy-related fields
- Bot struct compiles successfully

#### M-005: Delete startProxyServer function
- Delete entire `startProxyServer` function definition (lines 1374-1434)

**Acceptance Criteria:**
- startProxyServer function no longer exists in main.go
- No references to startProxyServer remain

#### M-012: Remove proxy tests from main_test.go
- Remove `TestProxyServer_Startup`, `TestProxyServer_DisabledFlag`, `TestProxyServer_PortInUse`, `TestProxyServer_SessionDirPermissions`, `TestProxyServer_GracefulShutdown` test functions
- Remove `testSerialMutex` and `globalStateMutex` if no longer needed

**Acceptance Criteria:**
- go test -run TestProxyServer returns no tests
- go test ./... succeeds
- No data races related to proxy state

### Wave 2: Directory deletions and API cleanup

**Milestones:** M-006, M-007, M-008, M-009

#### M-006: Delete pkg/proxy/ directory
- Delete entire `pkg/proxy/` directory including:
  - session.go, auth.go, proxy.go
  - All test files (session_test.go, auth_test.go, proxy_test.go)
  - README.md, CLAUDE.md

**Acceptance Criteria:**
- pkg/proxy/ directory no longer exists
- go build ./... succeeds
- go test ./... succeeds (after removing proxy tests from main_test.go)

#### M-007: Delete static/ directory
- Delete entire `static/` directory including:
  - index.html
  - css/ directory
  - js/ directory (including alpine.min.js)
  - test/ directory
  - README.md, CLAUDE.md

**Acceptance Criteria:**
- static/ directory no longer exists
- No references to static files remain in codebase

#### M-008: Remove static file serving from api/routes.go
- Remove static file server registration block (lines 24-36)
  - staticDir variable
  - os.Stat check
  - fs handler
  - Handle calls for /static/

**Acceptance Criteria:**
- /static/ route no longer registered
- api/routes.go only contains API endpoint registrations

#### M-009: Delete api/static_test.go
- Delete entire `api/static_test.go` file

**Acceptance Criteria:**
- api/static_test.go no longer exists
- go test ./api/ succeeds

### Wave 3: Documentation updates

**Milestones:** M-010, M-011

#### M-010: Update CLAUDE.md to remove proxy references
- Remove all references to:
  - proxy package
  - proxy server
  - static files
  - PROXY_ENABLED, PROXY_PORT, PROXY_HTTPS, PROXY_UPSTREAM_TIMEOUT
  - /static/ route

**Acceptance Criteria:**
- CLAUDE.md contains no references to proxy or static functionality
- Documentation accurately reflects current functionality

#### M-011: Update README.md to remove proxy documentation
- Remove PROXY_ENABLED, PROXY_PORT environment variable documentation
- Remove proxy server deployment examples and configuration
- Remove web UI static file serving documentation

**Acceptance Criteria:**
- README.md contains no proxy or static file documentation
- README.md accurately describes only Discord bot and REST API functionality
- Environment variables section lists only: DISCORD_TOKEN, CHANNEL_ID, API_ENABLED, API_PORT, API_BEARER_TOKEN, API_CORS_ORIGINS, API_TRUSTED_PROXY_IPS

## Files Modified

| File | Changes |
|------|---------|
| `main.go` | Remove proxy import, vars, server init, shutdown, Bot struct fields, startProxyServer() |
| `main_test.go` | Remove TestProxyServer_* test functions and related mutexes |
| `api/routes.go` | Remove static file server registration |
| `api/static_test.go` | Delete entire file |
| `pkg/proxy/` | Delete entire directory |
| `static/` | Delete entire directory |
| `CLAUDE.md` | Remove proxy/static references |
| `README.md` | Remove proxy environment var docs and deployment examples |

## Testing Strategy

### Build Verification
```bash
go build ./...          # Should succeed after all removals
```

### Test Verification
```bash
go test ./...           # Should succeed after proxy tests removed
go test ./api/...       # Should succeed without static_test.go
```

### Specific Test Exclusions
```bash
go test -run TestProxyServer  # Should return "no tests to run"
```

## Notes

- All proxy-related environment variables are optional (default behavior when not set)
- No backward compatibility concerns (features were never released)
- Discord bot and REST API functionality must remain intact
- Session directory (`./sessions`) created by proxy will be unused after removal
