# Proxy Package

Session-based authentication layer that proxies API requests, eliminating Bearer token exposure in browser JavaScript.

## Overview

The proxy package provides a session-based authentication layer for the bot API. Frontend authenticates once via Bearer token, receives an HTTP-only session cookie, and all subsequent API requests use the session cookie instead of the Bearer token. The backend validates sessions and proxies requests to the existing API, adding the Bearer token server-side.

**Single binary deployment**: File-based session storage with 4-hour session timeout, balancing security with operational simplicity for single-instance deployments.

## Architecture

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

**Component relationships**:
- Frontend authenticates ONCE with Bearer token, receives session cookie
- Session cookie is HTTP-only (inaccessible to JavaScript XSS)
- Backend validates session on each request, adds Bearer token server-side
- Bot API sees Bearer token (unchanged), frontend never sees it after auth

## Data Flow

### Authentication Flow

```
User enters Bearer token
  -> POST /proxy/login {token: "..."}
  -> Backend validates token against bot API
  -> Backend creates session: {id, token, expires, created}
  -> Backend writes session to file: sessions/{id}.gob
  -> Backend returns Set-Cookie: session_id={id}; HttpOnly; SameSite=Strict; Path=/proxy
```

### Request Flow (authenticated)

```
User action (e.g., save config)
  -> GET/POST /proxy/api/config (Cookie: session_id={id})
  -> Backend validates session from file map
  -> Backend extracts Bearer token from session data
  -> Backend proxies request to bot API with Authorization: Bearer {token}
  -> Bot API returns response
  -> Backend returns response to frontend
```

### Session Cleanup

```
Background goroutine every 5 minutes
  -> Scan session files for expired entries
  -> Delete expired files
  -> Remove from in-memory map
```

## Components

### SessionStore (`session.go`)

File-based session storage with in-memory cache for fast lookups.

**Responsibilities**:
- Create session: Generate 16-byte session ID (128 bits), write file, add to map
- Read session: Retrieve from in-memory map (hot path), check expiration
- Delete session: Remove from map, delete file
- Cleanup expired sessions: Scan files, delete expired, update map
- Background cleanup: Goroutine runs every 5 minutes

**Thread safety**: `sync.RWMutex` protects in-memory map. Concurrent reads allowed during request validation. Write lock only on session creation/deletion.

### AuthMiddleware (`auth.go`)

Session validation middleware for protected routes.

**Responsibilities**:
- Extract session cookie from request
- Validate session exists and not expired
- Set session in request context for downstream handlers
- Return 401 Unauthorized for invalid/missing sessions

**LoginHandler**: Validates Bearer token against bot API, creates session, sets HTTP-only cookie.

**LogoutHandler**: Deletes session, clears cookie.

### ProxyHandler (`proxy.go`)

Reverse proxy that adds Bearer token server-side.

**Responsibilities**:
- Extract session from context (set by AuthMiddleware)
- Retrieve Bearer token from session data
- Forward request to bot API with Authorization header
- Copy response headers (excluding hop-by-hop headers)
- Return upstream response to client

**Upstream timeout**: 10 seconds for all proxy requests.

## Invariants

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

## Tradeoffs

### Security vs. Simplicity

- **HTTP-only cookies** over encrypted local storage
  - Benefit: More secure against XSS
  - Cost: Requires backend implementation (accepted)

- **SameSite=Strict** over lax CSRF policies
  - Benefit: Stronger CSRF protection
  - Cost: Breaks some cross-origin flows (not needed for this app)

### Performance vs. Persistence

- **File + in-memory hybrid** over pure file
  - Benefit: Fast reads (memory), survives restarts (files)
  - Cost: Slight delay on startup to load sessions (acceptable for admin tool)

### Session Timeout Duration

- **4 hours** over shorter (15min) or longer (24h)
  - Benefit: Balances security and UX for admin tool
  - Cost: Longer sessions increase exposure, shorter sessions inconvenience users

## Security Properties

### XSS Protection

- HTTP-only session cookie blocks JavaScript access
- Token stored server-side only
- Frontend never sees Bearer token after authentication

### CSRF Protection

- SameSite=Strict cookie attribute prevents cross-origin requests
- Session cookie scoped to `/proxy` path
- Existing CSRF token validation in bot API layer

### Session Security

- 16-byte session ID (128 bits) provides 10^18 collision resistance
- File permissions: 0600 (owner read/write only)
- Session rotation on new login (old session invalidated)
- Background cleanup removes expired sessions

## Design Decisions

### Separation of Proxy and Bot Concerns

`pkg/proxy/` package isolates session/proxy logic from bot code.

**Rationale**:
- Bot API remains unchanged (backward compatibility)
- Can be extracted to separate binary later if needed
- Clean testing boundary (proxy tests don't require Discord bot)

### File-Based Sessions with In-Memory Cache

**Rationale**:
- Files provide persistence across restarts
- In-memory map provides fast lookup (hot path)
- RWMutex allows concurrent reads (server polling)
- Write lock only on session creation/deletion (rare)

**Tradeoff**: Slight delay on startup to load sessions (acceptable for admin tool traffic patterns).

### Middleware Pattern

**Rationale**:
- Clean separation of auth/proxy logic
- Easy to add logging, metrics, rate limiting later
- Testable in isolation (mock request/response)
- Reusable for future protected endpoints

## Constants

| Constant | Value | Rationale |
|----------|-------|-----------|
| `defaultSessionTimeout` | 4 hours | Balances security and UX for admin tool |
| `cleanupInterval` | 5 minutes | Balances stale session removal with CPU usage |
| `sessionIDLength` | 16 bytes (128 bits) | Sufficient collision resistance for single-instance |
| `upstreamTimeout` | 10 seconds | Fast error reporting for unresponsive bot API |
