# Web Backend Layer for Bearer Token Protection

## Overview

Implement a session-based authentication layer that proxies API requests, eliminating Bearer token exposure in browser JavaScript. The frontend authenticates once via Bearer token, receives an HTTP-only session cookie, and all subsequent API requests use the session cookie instead of the Bearer token. The backend validates sessions and proxies requests to the existing API, adding the Bearer token server-side.

**Key decision**: Single binary deployment with file-based session storage and 4-hour session timeout, balancing security with operational simplicity for single-instance deployments.

## Planning Context

### Decision Log

| Decision | Reasoning Chain |
|----------|-----------------|
| Approach 1: Session Proxy over full backend service | User requirements: file-based sessions + single binary -> Minimal approach fits constraints -> Full backend would add unnecessary complexity for current scale |
| File-based session storage | User explicitly chose file-based -> No external dependencies (Redis) -> Suitable for single-instance deployments -> Simpler operational model |
| 4-hour session timeout | User chose 1-8 hours -> 4 hours balances security and UX -> Long enough for admin work sessions -> Short enough to limit exposure window |
| Single binary deployment | User chose single binary -> Matches existing deployment model (Containerfile) -> No orchestration changes needed -> Simpler operations |
| HTTP-only SameSite=Strict cookies | Prevents XSS access to tokens -> SameSite=Strict prevents CSRF -> HTTP-only blocks JavaScript access -> Defense in depth for session security |
| Session middleware pattern | Standard Go web security practice -> Clean separation of auth/proxy logic -> Easy to test in isolation -> Reusable for future features |
| gob encoding for sessions | Fast binary serialization -> Built into Go standard library -> No external dependencies -> Sufficient for simple session data |
| Session rotation on auth refresh | New token invalidates old session -> Prevents session fixation -> Forces re-auth on privilege changes -> Standard security practice |
| Graceful degradation on backend failure | Frontend shows clear error -> No silent failures -> Admin can retry authentication -> Better than opaque failures |
| Proxy path prefix `/proxy` | Clear separation from bot API -> Easy to add security rules -> No conflict with existing routes -> Future API versioning path |
| 5-minute session cleanup interval | User confirmed 5 minutes -> Balances stale session removal with CPU usage -> 1 min too frequent (excessive I/O), 15 min allows stale accumulation -> Suitable for admin tool traffic patterns |
| 10-second proxy upstream timeout | User confirmed 10 seconds -> Fast error reporting for unresponsive bot API -> 30s too long (poor UX on hangs), 60s unacceptable for interactive UI -> Accepts that complex config operations may timeout |
| 16-byte session ID length (128 bits) | 10^18 collision resistance for billion-session namespace -> Sufficient for single-instance deployment -> Base64 encoding keeps cookies small (<32 chars) -> Shorter lengths (8 bytes) increase collision risk, longer (32 bytes) waste space |

### Rejected Alternatives

| Alternative | Why Rejected |
|-------------|--------------|
| Full backend service with Redis | User chose file-based storage + single binary -> Adds operational complexity -> External dependency not needed for current scale |
| Separate container proxy | User chose single binary deployment -> Requires orchestration changes -> More complex deployment without clear benefit for single-instance |
| JWT-based stateless sessions | File storage is simpler for single instance -> JWT adds crypto complexity -> No benefit without distributed systems |
| Reverse proxy (nginx/caddy) | Not written in Go -> Doesn't match project language -> Adds external service -> User wanted Go solution |
| Client-side Bearer token with CSP | Token still exposed to XSS -> CSP is defense-in-depth not primary control -> Session cookies are stronger guarantee |

### Constraints & Assumptions

**Technical constraints:**
- Go 1.25.5 (existing project version)
- Existing Alpine.js frontend must work with minimal changes
- Bot API continues to accept Bearer tokens (backward compatibility)
- File-based session storage (user-specified)
- Session timeout: 4 hours (user-specified range: 1-8 hours)
- Single binary deployment (user-specified)
- Session cleanup interval: 5 minutes (user-specified)
- Proxy upstream timeout: 10 seconds (user-specified)

**Organizational constraints:**
- Minimal operational complexity preferred
- Existing deployment uses Podman with single container
- No external dependencies (Redis, databases)

**Dependencies:**
- Go standard library (net/http, sync/atomic, encoding/gob)
- Existing Alpine.js frontend
- Existing bot API with Bearer token authentication

**Default conventions applied:**
- `<default-conventions domain="testing">`: Integration tests with real dependencies, example-based unit tests, generated E2E datasets

### Known Risks

| Risk | Mitigation | Anchor |
|------|------------|--------|
| File-based session storage lost on crash | Accepted: Single-instance deployment -> Sessions require re-auth (acceptable for admin tool) -> No data loss (sessions only, no persistent data) | N/A (design tradeoff) |
| Concurrent session file access | sync.RWMutex protects session map -> Atomic file operations -> Test with concurrent requests | New code: pkg/proxy/session.go (to be implemented) |
| Session fixation attacks | Rotate session on privilege changes -> Regenerate on token refresh -> Expire old sessions | New code: pkg/proxy/auth.go (to be implemented) |
| XSS attacks reading session cookies | HTTP-only flag blocks JavaScript access -> Defense-in-depth with CSP -> Cookies not accessible via document.cookie | New code: pkg/proxy/auth.go SetCookie headers |
| CSRF attacks | SameSite=Strict cookie attribute -> CSRF token validation on state changes -> Existing CSRF protection in main.go:api handler | static/js/app.js:196-211 (existing CSRF retry logic) |
| Proxy backend unavailable | Frontend shows clear error message -> 503 Service Unavailable response -> Admin can retry authentication | New code: pkg/proxy/proxy.go error handling |

## Invisible Knowledge

### Architecture

```
Browser                          Go Backend (Single Binary)                     Bot API
  |                                   |                                         |
  | 1. POST /proxy/login (Bearer)     |                                         |
  |---------------------------------->| 2. Validate Bearer token               |
  |                                   |--------------------------------------->|
  |                                   |<---------------------------------------|
  | 3. Set-Cookie: session_id        | 4. Create session (file + in-memory)   |
  |<----------------------------------|                                         |
  |                                   |                                         |
  | 4. GET /proxy/api/config (Cookie) |                                         |
  |---------------------------------->| 5. Validate session                    |
  |                                   | 6. Add Bearer token server-side        |
  |                                   |--------------------------------------->|
  |                                   |<---------------------------------------|
  | 7. Response data                  |                                         |
  |<----------------------------------|                                         |
```

**Component relationships:**
- Frontend authenticates ONCE with Bearer token, receives session cookie
- Session cookie is HTTP-only (inaccessible to JavaScript XSS)
- Backend validates session on each request, adds Bearer token server-side
- Bot API sees Bearer token (unchanged), frontend never sees it after auth

### Data Flow

```
Authentication Flow:
User enters Bearer token
  -> POST /proxy/login {token: "..."}
  -> Backend validates token against bot API
  -> Backend creates session: {id, token, expires, created}
  -> Backend writes session to file: sessions/{id}.gob
  -> Backend returns Set-Cookie: session_id={id}; HttpOnly; SameSite=Strict; Path=/proxy

Request Flow (authenticated):
User action (e.g., save config)
  -> GET/POST /proxy/api/config (Cookie: session_id={id})
  -> Backend validates session from file map
  -> Backend extracts Bearer token from session data
  -> Backend proxies request to bot API with Authorization: Bearer {token}
  -> Bot API returns response
  -> Backend returns response to frontend

Session Cleanup:
Background goroutine every 5 minutes
  -> Scan session files for expired entries
  -> Delete expired files
  -> Remove from in-memory map
```

### Why This Structure

**Separation of proxy and bot concerns:**
- `pkg/proxy/` package isolates session/proxy logic from bot code
- Bot API remains unchanged (backward compatibility)
- Can be extracted to separate binary later if needed
- Clean testing boundary (proxy tests don't require Discord bot)

**File-based sessions with in-memory cache:**
- Files provide persistence across restarts
- In-memory map provides fast lookup (hot path)
- RWMutex allows concurrent reads (server polling)
- Write lock only on session creation/deletion (rare)

**Middleware pattern:**
- auth middleware wraps proxy handler
- Easy to add logging, metrics, rate limiting later
- Testable in isolation (mock request/response)
- Reusable for any future protected endpoints

### Invariants

1. **Session consistency**: Session file always exists before in-memory map entry
   - Write file first, then add to map
   - Delete from map first, then delete file
   - Prevents serving non-existent sessions

2. **Bearer token isolation**: Frontend never receives Bearer token after authentication
   - Token only stored server-side in session file
   - HTTP-only cookie prevents JavaScript access
   - Proxied requests add token server-side

3. **Session expiration**: Expired sessions never served
   - Background cleanup removes expired files
   - Validation checks expiration on every request
   - 4-hour timeout enforced consistently

4. **Concurrent session access**: Multiple goroutines can read sessions safely
   - sync.RWMutex on session map
   - Atomic read operations during request validation
   - Write operations serialized (session create/delete)

### Tradeoffs

**Security vs. simplicity:**
- Chose HTTP-only cookies over encrypted local storage -> More secure against XSS -> Requires backend implementation (accepted cost)
- Chose SameSite=Strict over lax CSRF policies -> Breaks some cross-origin flows (not needed for this app) -> Stronger CSRF protection

**Performance vs. persistence:**
- Chose file + in-memory hybrid over pure file -> Fast reads (memory) -> Survives restarts (files) -> Minimal complexity
- Tradeoff: Slight delay on startup to load sessions (acceptable for admin tool traffic patterns)

**Session timeout duration:**
- Chose 4 hours over shorter (15min) or longer (24h) -> Balances security and UX -> Acceptable exposure window for admin tool -> Frequent enough re-auth for security
- Tradeoff: Longer sessions increase exposure, shorter sessions inconvenience users

## Milestones

### Milestone 1: Session Storage Foundation

**Files**:
- `pkg/proxy/session.go`
- `pkg/proxy/session_test.go`

**Flags**: `conformance`, `security`

**Requirements**:
- Create `SessionStore` struct with file-based persistence and in-memory cache
- Implement session CRUD operations (Create, Read, Delete, List, Cleanup)
- Use gob encoding for session file format
- Thread-safe with sync.RWMutex for concurrent access

**Acceptance Criteria**:
- Session file created in `sessions/` directory with `.gob` extension
- Session stored in-memory map after file write success
- Read returns session data from in-memory map (no file read on hot path)
- Delete removes from map first, then deletes file
- Cleanup removes expired session files and map entries
- Concurrent reads (100 goroutines) complete without deadlock
- File permissions are 0600 (owner read/write only)

**Tests**:
- **Test files**: `pkg/proxy/session_test.go`
- **Test type**: integration (real file operations)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Create session, read session, delete session
  - Edge: Concurrent reads/writes, expired session cleanup
  - Error: File permissions denied, corrupt session file

**Code Intent**:
- New file `pkg/proxy/session.go`:
  - `Session` struct: ID, Token, Expires, Created, LastAccessed
  - `SessionStore` struct: sessions map (sync.RWMutex), sessionsDir string
  - `NewSessionStore(dir string) (*SessionStore, error)`: Create directory if needed, load existing sessions
  - `Create(token string, timeout time.Duration) (*Session, error)`: Generate ID, write file, add to map
  - `Get(id string) (*Session, bool)`: Read from map, check expiration
  - `Delete(id string) error`: Remove from map, delete file
  - `Cleanup() error`: Scan files, delete expired, update map
  - `stopBackgroundCleanup()`: Stop cleanup goroutine (called on shutdown)
- Session ID format: 16-byte random (crypto/rand) base64-encoded
- Expiration: 4 hours from creation (Decision: "4-hour session timeout")
- File format: gob-encoded Session struct
- Background cleanup: every 5 minutes (Decision: "5-minute session cleanup interval")

**Code Changes**:
(Filled by Developer agent after plan approval)

---

### Milestone 2: Authentication Middleware

**Files**:
- `pkg/proxy/auth.go`
- `pkg/proxy/auth_test.go`

**Flags**: `security`, `needs-rationale`

**Requirements**:
- Implement session-based authentication middleware
- Validate session cookie on each request
- Set HTTP-only, SameSite=Strict cookies on authentication
- Return 401 Unauthorized for invalid/missing sessions
- Handle CSRF token forwarding for state-changing requests

**Acceptance Criteria**:
- Valid session cookie allows request to proceed
- Invalid/expired session returns 401 with clear error message
- Login endpoint creates session and sets Set-Cookie header
- Logout endpoint deletes session and clears cookie
- Cookie attributes: HttpOnly=true, SameSite=Strict, Path=/proxy, Secure=true (if HTTPS)
- CSRF token extracted from cookie and added to request context for downstream handlers
- Session rotation on re-auth (old session invalidated)

**Tests**:
- **Test files**: `pkg/proxy/auth_test.go`
- **Test type**: integration (real http.Request/ResponseRecorder)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Valid session proceeds, login creates session, logout clears session
  - Edge: Missing cookie, malformed session ID, expired session
  - Error: Invalid Bearer token during login, concurrent login attempts

**Code Intent**:
- New file `pkg/proxy/auth.go`:
  - `AuthMiddleware(next http.Handler, store *SessionStore) http.Handler`: Validate session cookie, set context
  - `LoginHandler(store *SessionStore, botAPIURL string) http.HandlerFunc`: Validate Bearer token against bot API, create session, set cookie
  - `LogoutHandler(store *SessionStore) http.HandlerFunc`: Delete session, clear cookie
  - `GetSession(r *http.Request) (*Session, bool)`: Extract session from request context
  - `SetSessionCookie(w http.ResponseWriter, sessionID string)`: Set cookie with HttpOnly, SameSite=Strict
  - `ClearSessionCookie(w http.ResponseWriter)`: Clear cookie by setting MaxAge=-1
- Session cookie name: "proxy_session"
- Bot API validation: GET /api/config with Bearer token (Decision: "reuse existing health check")
- Context key for session: "session" (type: contextKey)
- CSRF token: Extract from cookie, pass in header to bot API (existing behavior)
- Session rotation: Delete old session before creating new one in LoginHandler

**Code Changes**:
(Filled by Developer agent after plan approval)

---

### Milestone 3: Proxy Handler

**Files**:
- `pkg/proxy/proxy.go`
- `pkg/proxy/proxy_test.go`

**Flags**: `conformance`, `error-handling`

**Requirements**:
- Implement reverse proxy handler that forwards requests to bot API
- Add Bearer token from session to upstream Authorization header
- Forward all request headers, body, and query parameters
- Return upstream response to client with status code and body
- Handle upstream errors with clear messages

**Acceptance Criteria**:
- GET/POST/PATCH requests forwarded to bot API with Bearer token
- Response status and body returned to client unmodified
- Upstream connection errors return 503 with error message
- Upstream 401/403 errors returned to client (invalid session)
- Request URL path preserved (e.g., /proxy/api/config -> http://localhost:3001/api/config)
- Query parameters forwarded correctly
- Request/response body buffered and forwarded

**Tests**:
- **Test files**: `pkg/proxy/proxy_test.go`
- **Test type**: integration (real HTTP server)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: GET request returns data, POST request creates data
  - Edge: Large request body, special characters in query params
  - Error: Upstream unreachable, upstream returns error status

**Code Intent**:
- New file `pkg/proxy/proxy.go`:
  - `ProxyHandler(botAPIURL string, store *SessionStore) http.Handler`: Extract session from context, add Bearer token, forward request
  - `forwardRequest(req *http.Request, botAPIURL string) (*http.Response, error)`: Create upstream request, copy headers/body, execute
  - `copyHeaders(dst, src http.Header)`: Copy all headers except Hop-by-hop (Connection, Keep-Alive, etc.)
- Bot API URL: http://localhost:3001 (default API_PORT in main.go)
- Bearer token extraction: session.Token from context
- Upstream timeout: 10 seconds (Decision: "10-second proxy upstream timeout")
- Error responses: JSON format {error: string} matching existing API responses
- Hop-by-hop headers excluded: Connection, Keep-Alive, Proxy-Authenticate, Proxy-Authorization, TE, Trailers, Transfer-Encoding, Upgrade

**Code Changes**:
(Filled by Developer agent after plan approval)

---

### Milestone 4: Frontend Integration

**Files**:
- `static/js/app.js`
- `static/test/app.test.js`

**Flags**: `conformance`

**Requirements**:
- Remove Bearer token storage from frontend
- Replace Bearer auth with session cookie authentication
- Update login flow to POST to /proxy/login
- Update API requests to use /proxy prefix
- Remove CSRF token handling (proxy handles it server-side)

**Acceptance Criteria**:
- Bearer token not stored in sessionStorage or localStorage
- Login sends POST /proxy/login {token: "..."}, receives session cookie
- API requests use /proxy prefix (e.g., /proxy/api/config)
- Authorization header not sent with requests (proxy adds it)
- CSRF token fetching removed from init flow
- Polling and error handling unchanged
- Existing tests pass with new endpoints

**Tests**:
- **Test files**: `static/test/app.test.js`
- **Test type**: integration (generated datasets)
- **Backing**: default-derived (per <default-conventions domain="testing">: E2E generated datasets for frontend integration tests)
- **Scenarios**:
  - Normal: Login succeeds, API requests work, logout clears cookie
  - Edge: Session expired during polling, backend unavailable
  - Error: Invalid Bearer token, network errors

**Code Intent**:
- Modify `static/js/app.js`:
  - Remove `token` and `inputToken` properties
  - Remove `sessionStorage.getItem('apiToken')` and `setItem` calls
  - Remove `fetchCSRFToken()` method and call
  - Modify `login()` to POST /proxy/login with {token: input}
  - Modify `apiRequest()` to use /proxy prefix and remove Authorization header
  - Remove X-CSRF-Token header logic (proxy handles it)
  - Remove CSRF token retry logic (lines 196-211 in existing code)
  - Keep existing error handling, polling, dirty flag logic
- Login request: POST /proxy/login, body JSON {token: "..."}
- API request prefix: /proxy/api/config, /proxy/api/config/servers, etc.
- Session cookie: Set by backend, HttpOnly (invisible to JavaScript)

**Code Changes**:
(Filled by Developer agent after plan approval)

---

### Milestone 5: Main Binary Integration

**Files**:
- `main.go`
- `Containerfile`

**Flags**: `conformance`

**Requirements**:
- Add proxy server startup to main.go
- Add command-line flag for proxy enable/disable
- Add proxy port configuration
- Share API Bearer token environment variable
- Start proxy server in separate goroutine
- Update Containerfile to expose proxy port

**Acceptance Criteria**:
- Proxy server starts when PROXY_ENABLED=true
- Proxy listens on PROXY_PORT (default 8080)
- Proxy uses same API_BEARER_TOKEN as bot API validation
- Graceful shutdown stops proxy server
- Containerfile exposes port 8080
- Bot API and proxy run concurrently in same binary
- Existing bot functionality unchanged

**Tests**:
- **Test files**: `main_test.go` (add tests)
- **Test type**: integration (real HTTP servers)
- **Backing**: default-derived (per <default-conventions domain="testing">: Integration tests with real HTTP servers for binary integration verification)
- **Scenarios**:
  - Normal: Proxy starts, handles requests, bot API works
  - Edge: Proxy disabled flag, proxy port in use
  - Error: Session directory permissions, proxy startup failure

**Code Intent**:
- Modify `main.go`:
  - Add `proxyEnabled bool` flag and `proxyPort string` flag (read from env vars)
  - Add `startProxyServer()` function: Create session store, setup routes, start HTTP server
  - Add proxy routes: POST /proxy/login, POST /proxy/logout, GET/POST/PATCH /proxy/api/*
  - Import `github.com/bombom/absa-ac/proxy` package
  - Call `startProxyServer()` in main() if proxyEnabled
  - Add proxy server shutdown to `WaitForShutdown()` or cleanup handler
- Environment variables: PROXY_ENABLED (bool, default false), PROXY_PORT (string, default "8080")
- Session directory: "./sessions" (relative to working directory)
- Graceful shutdown: Shutdown context cancels proxy server context
- Error handling: Log proxy startup errors, continue bot operation if proxy fails

- Modify `Containerfile`:
  - Add `EXPOSE 8080` for proxy port
  - No other changes (binary contains both bot and proxy)

**Code Changes**:
(Filled by Developer agent after plan approval)

---

### Milestone 6: Documentation

**Delegated to**: @agent-technical-writer (mode: post-implementation)

**Source**: `## Invisible Knowledge` section of this plan

**Files**:
- `pkg/proxy/CLAUDE.md`
- `pkg/proxy/README.md`
- `static/README.md` (update for proxy integration)
- `README.md` (update main README with proxy section)

**Requirements**:
- CLAUDE.md files follow tabular index format (WHAT/WHEN columns)
- README.md files capture invisible knowledge from this plan
- Architecture diagrams in README.md match plan
- No prose sections in CLAUDE.md (navigation index only)
- Self-contained documentation (no external references)

**Acceptance Criteria**:
- `pkg/proxy/CLAUDE.md` exists with tabular format
- `pkg/proxy/README.md` exists with architecture, data flow, invariants, tradeoffs
- `static/README.md` updated with proxy integration details
- Main `README.md` updated with proxy deployment instructions
- All documentation is self-contained (no external wiki links)

**Source Material**: `## Invisible Knowledge` section of this plan

---

### Cross-Milestone Integration Tests

Integration tests spanning multiple milestones:

**File**: `pkg/proxy/integration_test.go` (created in Milestone 5)

**Test coverage**:
- Full auth flow: Login -> Session created -> API request -> Logout -> Session deleted
- Session expiration: Login -> Wait 4 hours -> API request returns 401
- Concurrent requests: 10 goroutines making API requests simultaneously
- Backend failure: Bot API down -> Proxy returns 503 with clear error
- Proxy startup: Session directory created -> Existing sessions loaded -> Proxy listens

**Integration tests in Milestone 5** verify the complete flow that end users would exercise, using real HTTP servers and file operations. This creates fast feedback as soon as all components exist.

## Milestone Dependencies

```
M1 (Session Storage)
  |
  v
M2 (Auth Middleware) -----> M3 (Proxy Handler)
  |                              |
  v                              v
M4 (Frontend) <------------------+
  |
  v
M5 (Main Integration) ----> M6 (Documentation)
```

Parallel opportunities:
- M2 and M3 can be developed in parallel after M1 (both depend on SessionStore)
- M4 can be developed in parallel with M2/M3 (frontend changes are independent)
- M5 waits for M1-M4 completion (integration point)
