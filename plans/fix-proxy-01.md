# Fix Critical Proxy Security Issues + JSON Migration

## Overview

Fixes 5 CRITICAL security vulnerabilities in the session-based proxy layer and migrates session persistence from unstable gob format to JSON. The proxy currently exposes bearer tokens in plaintext, uses hardcoded `isHTTPS()=false` (cookies never marked Secure), has no login rate limiting (DoS vulnerability), lacks CSRF protection, and is vulnerable to path traversal attacks in session file loading. Session persistence uses gob encoding which is unstable across Go versions and not human-readable for debugging.

## Planning Context

### Decision Log

| Decision | Reasoning Chain |
| --- | --- |
| JSON over gob for session persistence | gob is unstable across Go versions -> not human-readable for debugging at 3 AM -> JSON is stable, readable, and has stdlib support -> faster migration without backward compatibility needed |
| Env var for HTTPS detection | Auto-detect from request can be fooled by reverse proxies -> always Secure breaks HTTP dev setups -> PROXY_HTTPS=true env var provides explicit operator control -> Secure flag only set when operator confirms HTTPS termination |
| AES-256-GCM for token encryption | Tokens must not be stored in plaintext (issue #3) -> symmetric encryption avoids per-token key management overhead -> GCM provides authenticated encryption (prevents tampering) -> 256-bit key is industry standard for sensitive data |
| Token-encrypted-at-rest pattern | Encrypting entire session JSON would require decrypting for every read (hot path) -> only Bearer token is sensitive; other fields (ID, timestamps) are not -> encrypt token separately -> store encrypted token alongside plaintext metadata -> faster Get() operations |
| Fixed-window rate limiting | Token bucket requires stateful counter per IP (complexity) | fixed-window with in-memory map is simpler -> DoS protection goal is basic throttling, not perfect rate shaping -> acceptable tradeoff for single-server deployment |
| Double-submit cookie CSRF | Original CSRF token system removed in frontend -> session cookie is HttpOnly (JavaScript cannot read it) -> double-submit pattern uses same cookie value as header -> no server-side state needed -> works with proxy architecture |
| Migrate without backward compatibility | User explicitly stated no gob support needed -> proxy unreleased so no existing sessions to migrate -> simply delete any .gob files on startup -> simpler code, no migration tool needed |
| Filename sanitization for path traversal | os.ReadDir + filepath.Join trusts directory contents -> attacker who can create files in sessions_dir could escape -> validate session IDs match base64 charset before file operations -> reject malformed filenames early |

### Rejected Alternatives

| Alternative | Why Rejected |
| --- | --- |
| Keep gob encoding | Unstable across Go versions - session files break after Go upgrades; not human-readable for debugging production issues |
| Auto-detect HTTPS from request | Reverse proxies terminate SSL and forward via HTTP - request.Scheme always "http" -> cookies incorrectly marked insecure; env var is explicit and operator-controlled |
| Encrypt entire session JSON | Every Get() would require decryption (hot path) -> performance degradation; only Bearer token is sensitive data |
| Token bucket rate limiting | Requires per-IP state and cleanup goroutines -> complexity disproportionate to single-server threat model; fixed-window provides adequate basic throttling |
| Server-side CSRF tokens | Requires server-side session state -> conflicts with stateless session design; double-submit cookie uses existing session cookie |
| Dual format migration | User explicitly declined backward compatibility; adds branches and validation logic; one-shot migration is simpler and faster |
| bcrypt for token hashing | One-way hash prevents token retrieval -> proxy needs to forward Bearer token to upstream API -> cannot use hashing; must use reversible encryption |

### Constraints & Assumptions

- **Technical**: Go 1.25.5+, single-server deployment (no distributed session store), existing .env configuration pattern
- **Organizational**: ~5 week timeline acceptable, user explicitly declined backward compatibility
- **Dependencies**: crypto/aes for encryption, encoding/json for serialization, existing rate-limiting patterns not present in codebase
- **Default conventions applied**: <default-conventions domain="testing"> (integration tests with real deps, property-based for complex logic)

### Known Risks

| Risk | Mitigation | Anchor |
| --- | --- | --- |
| Encryption key exposed in environment | Use file permissions (0600) on session directory | `session.go:68` creates dir with 0700 |
| Rate limiting evicts legitimate users | Window expires after 60s -> temporary inconvenience only | Design in Milestone 4 |
| CSRF double-submit cookie fails with subdomain paths | Session cookie Path is "/proxy" -> SameSite Strict prevents subdomain attacks | `auth.go:198` sets Path and SameSite |

## Invisible Knowledge

### Architecture

```
HTTP Request --> Proxy (/proxy/api/*)
                   |
                   v
           AuthMiddleware
                   |
                   v
        Validate Session Cookie
                   |
                   v
        Decrypt Token (AES-256-GCM)
                   |
                   v
        ForwardRequest (add Bearer header)
                   |
                   v
           Upstream Bot API
```

**Session lifecycle**:
1. Login: User POSTs Bearer token -> validate against bot API -> encrypt token -> create session JSON -> set HttpOnly cookie
2. Proxy: Request arrives with cookie -> load session JSON -> decrypt token -> add Authorization header -> forward upstream
3. Logout: Delete session file -> clear cookie

### Data Flow

```
Login Flow:
Client (Bearer token) --> POST /proxy/login
                         |
                         v
                    validateBearerTokenWithBotAPI()
                         |
                         v
                    encryptToken()
                         |
                         v
                    sessionStore.Create()
                    (writes JSON with encrypted token)
                         |
                         v
                    SetSessionCookie (HttpOnly, Secure if PROXY_HTTPS=true)

Proxy Flow:
Client (session cookie) --> GET /proxy/api/config
                           |
                           v
                    AuthMiddleware extracts session ID
                           |
                           v
                    sessionStore.Get() loads JSON
                           |
                           v
                    decryptToken() -> Bearer token
                           |
                           v
                    forwardRequest() adds Authorization header
                           |
                           v
                    Bot API responds
```

### Why This Structure

- **Separation of concerns**: `session.go` handles persistence, `auth.go` handles HTTP auth flow, `proxy.go` handles request forwarding - each file has single responsibility
- **Encryption at session layer**: Token encryption happens in `session.go` before storage, not in `auth.go` - ensures all session operations use encrypted tokens consistently
- **Migration is one-shot**: No dual format support - simpler code, faster migration, explicit user approval

### Invariants

- Session IDs are always base64-encoded 16-byte values (128 bits entropy)
- Encrypted tokens in JSON are base64-encoded ciphertext
- Session files are JSON (not gob) after migration
- Session cookie Path is always "/proxy" (prevents cookie leakage to other paths)
- Session timeout is 4 hours from creation (not rolling expiry)

### Tradeoffs

- **Performance vs security**: Token encryption on every Create() adds ~1ms overhead -> acceptable for login flow (not hot path); Get() decrypts on every request (hot path) -> AES-GCM is fast (~500ns per op)
- **Simplicity vs perfect rate limiting**: Fixed-window rate limiting allows bursts at window boundary -> simpler implementation than token bucket; acceptable for basic DoS protection
- **Debuggability vs disk space**: JSON is larger than gob (~30% larger) -> human-readable for production debugging; disk space is cheap

## Milestones

### Milestone 1: HTTPS Detection Configuration

**Files**:
- `pkg/proxy/auth.go`
- `main.go`

**Flags**: `security`

**Requirements**:
- Replace `isHTTPS()` hardcoded `false` with environment variable check
- Add `PROXY_HTTPS` environment variable reading in main.go
- Pass HTTPS flag to auth handlers
- Update `SetSessionCookie()` and `ClearSessionCookie()` to use flag
- Add `PROXY_UPSTREAM_TIMEOUT` environment variable for configurable upstream timeout
- Pass upstream timeout to proxy handlers

**Acceptance Criteria**:
- When `PROXY_HTTPS=true`, session cookies set `Secure=true`
- When `PROXY_HTTPS` unset or `false`, `Secure=false`
- Environment variable is read once at startup, not per-request
- When `PROXY_UPSTREAM_TIMEOUT` set, upstream API requests use configured timeout
- When `PROXY_UPSTREAM_TIMEOUT` unset, defaults to 10 seconds

**Tests**:
- **Test files**: `pkg/proxy/auth_test.go`
- **Test type**: integration (real http.Cookie creation)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: `PROXY_HTTPS=true` results in Secure cookie
  - Normal: `PROXY_HTTPS=false` results in non-Secure cookie
  - Edge: Unset env var defaults to false
  - Normal: `PROXY_UPSTREAM_TIMEOUT=30s` results in 30 second timeout
  - Normal: `PROXY_UPSTREAM_TIMEOUT` unset defaults to 10 seconds

**Code Intent**:
- Modify `auth.go`: Change `isHTTPS()` from hardcoded `false` to accept `bool` parameter
- Modify `auth.go`: Add `proxyUpstreamTimeout` parameter to `validateBearerTokenWithBotAPI()`
- Modify `main.go`: Read `PROXY_HTTPS` env var during startup, pass to auth handlers
- Modify `main.go`: Read `PROXY_UPSTREAM_TIMEOUT` env var during startup, parse duration, pass to auth and proxy handlers
- Modify `auth.go`: Update `SetSessionCookie()` and `ClearSessionCookie()` to accept `bool` secure flag
- No behavior changes to cookie Path, MaxAge, HttpOnly, or SameSite attributes

**Dependencies**: None

---

### Milestone 2: Path Traversal Fix in Session Loading

**Files**:
- `pkg/proxy/session.go`

**Flags**: `security`

**Requirements**:
- Validate session ID format before file operations
- Reject filenames that don't match base64 charset (URL-safe alphabet)
- Early validation in `Get()`, `Delete()`, and `loadExistingSessions()`

**Acceptance Criteria**:
- Filenames containing path separators (`/`, `\`) are rejected
- Filenames with characters outside base64url charset are rejected
- Invalid session IDs return error without filesystem access

**Tests**:
- **Test files**: `pkg/proxy/session_test.go`
- **Test type**: property-based (quickcheck style for session ID validation)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Valid base64 session IDs accepted
  - Edge: Empty string rejected
  - Edge: Single character rejected (below min length)
  - Security: `../../../etc/passwd` rejected
  - Security: `../malicious.gob` rejected
  - Security: Absolute paths rejected

**Code Intent**:
- Add `isValidSessionID(id string) bool` function in `session.go`
- Validate: length >= 1, only base64url chars (A-Za-z0-9_-), no path separators
- Call `isValidSessionID()` at start of `Get()`, `Delete()`, `loadExistingSessions()`
- Return `ErrInvalidSessionID` (new error) for invalid IDs
- Update `loadExistingSessions()` to log warnings when skipping invalid filenames
- Modify `Get()` to return `(*Session, error)` instead of `(*Session, bool)` for consistent error propagation

**Dependencies**: None

---

### Milestone 3: Token Encryption + JSON Persistence

**Files**:
- `pkg/proxy/session.go`
- `pkg/proxy/session_test.go`

**Flags**: `security`, `needs-rationale`, `complex-algorithm`

**Requirements**:
- Replace gob encoding with JSON format for session persistence
- Encrypt Bearer tokens before storing in JSON
- Use AES-256-GCM for authenticated encryption

**Acceptance Criteria**:
- New sessions are stored as JSON with structure: `{id, encrypted_token, expires, created, last_accessed}`
- Token encryption key sourced from `SESSION_ENCRYPTION_KEY` env var (base64-encoded 32 bytes)
- `Get()` decrypts token before returning Session struct
- Existing .gob files in sessions directory are deleted on startup (no migration needed)

**Tests**:
- **Test files**: `pkg/proxy/session_test.go`
- **Test type**: integration (real filesystem, real encryption)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Create session -> JSON file created with encrypted token
  - Normal: Get session -> token decrypted correctly
  - Edge: Invalid encryption key -> error on Create
  - Edge: Tampered JSON ciphertext -> authentication failure on Get
  - Security: Plaintext token never appears in JSON file
  - Startup: Existing .gob files deleted with warning logged

**Code Intent**:

**session.go changes**:
- Remove `encoding/gob` import, add `encoding/json`, `crypto/aes`, `crypto/cipher`, `crypto/rand`, `encoding/base64`
- Define constants: `ENCRYPTION_KEY_ENV = "SESSION_ENCRYPTION_KEY"`, `keySize = 32`
- Add `encryptToken(plaintext, key []byte) (string, error)` function using AES-256-GCM
- Add `decryptToken(ciphertext string, key []byte) (string, error)` function
- Add `loadEncryptionKey() ([]byte, error)` function reading env var, base64-decoding
- Modify `SessionStore` struct: add `encryptionKey []byte` field
- Modify `NewSessionStore()`: read encryption key, store in struct, error if missing
- Modify `NewSessionStore()`: delete any existing .gob files in sessions directory with warning log (no migration needed)
- Modify `Session` struct for JSON serialization: add `EncryptedToken string` json tag, remove `Token` from JSON (keep in memory)
- Modify `Create()`: encrypt token before writing JSON
- Modify `loadSessionFromFile()`: decrypt token after loading JSON
- Replace gob encoding/decoding with json.Marshal/Unmarshal
- Update `writeSessionToFile()`: use JSON instead of gob
- Update file extension logic: `.json` instead of `.gob`

**Dependencies**: None

---

### Milestone 4: Rate Limiting on Login

**Files**:
- `pkg/proxy/auth.go`
- `pkg/proxy/auth_test.go`

**Flags**: `security`, `performance`

**Requirements**:
- Add fixed-window rate limiting to `/proxy/login` endpoint
- Track failed login attempts per IP address
- Return 429 Too Many Requests after threshold

**Acceptance Criteria**:
- More than 5 failed login attempts per IP per 60 seconds returns 429
- Successful login resets counter for that IP
- Counter expires after 60 seconds of inactivity
- Rate limiting state is in-memory (lost on restart - acceptable)

**Tests**:
- **Test files**: `pkg/proxy/auth_test.go`
- **Test type**: integration (real HTTP requests, in-memory rate limiter)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: 5 failed attempts allowed, 6th returns 429
  - Normal: Successful login resets counter
  - Edge: Counter expires after 60 seconds
  - Edge: Different IPs tracked independently
  - Security: Rate limit bypass attempts (X-Forwarded-For spoofing) not mitigated (documented limitation)

**Code Intent**:
- Add `rateLimiter` struct in `auth.go`: `map[string]*attemptEntry`, `sync.Mutex`, `windowDuration=60s`, `maxAttempts=5`
- Add `attemptEntry` struct: `count int`, `windowStart time.Time`
- Add `checkRateLimit(ip string) bool` function: returns true if allowed
- Add `recordFailedAttempt(ip string)` function: increments counter
- Add `resetRateLimit(ip string)` function: clears counter on success
- Modify `LoginHandler()`: extract IP from `r.RemoteAddr`, check rate limit, record failures
- Add background cleanup goroutine in `rateLimiter` to expire old entries (or lazy cleanup on check)
- Return 429 with `Retry-After` header when rate limited

**Dependencies**: Milestone 3 (JSON sessions required before adding auth enhancements)

---

### Milestone 5: CSRF Protection

**Files**:
- `pkg/proxy/auth.go`
- `pkg/proxy/proxy.go`
- `static/js/app.js`
- `pkg/proxy/auth_test.go`

**Flags**: `security`

**Requirements**:
- Add double-submit cookie CSRF protection
- Frontend sends session cookie value in X-CSRF-Token header
- Backend validates header matches cookie on state-changing requests

**Acceptance Criteria**:
- POST/PATCH/PUT/DELETE requests to `/proxy/api/*` require X-CSRF-Token header
- Header value must match session cookie value
- Mismatched header returns 403 Forbidden
- GET requests do not require CSRF token

**Tests**:
- **Test files**: `pkg/proxy/auth_test.go`, `static/test/app.test.js`
- **Test type**: integration (real HTTP with CSRF middleware)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Valid CSRF token allows POST
  - Security: Missing CSRF token returns 403
  - Security: Mismatched CSRF token returns 403
  - Normal: GET requests work without CSRF token
  - Edge: Empty session cookie returns 401 (before CSRF check)

**Code Intent**:

**auth.go changes**:
- Add `CSRFMiddleware(next http.Handler, store *SessionStore) http.Handler` function
- Extract session cookie from request
- Extract X-CSRF-Token header from request
- Compare values, return 403 if mismatch
- Allow GET requests to bypass CSRF check
- Apply CSRF middleware to proxy handler in middleware chain

**proxy.go changes**:
- Add `validateSessionForProxy(r *http.Request, store *SessionStore) (*Session, error)` helper function
- Extract session validation from `ProxyHandler()` into separate function
- Session validation: get session from context, check existence, check expiration
- Modify `ProxyHandler()` to call `validateSessionForProxy()` for session extraction
- Modify `ProxyHandler()` to focus on request forwarding and response copying
- This separation reduces `ProxyHandler()` complexity by isolating session logic

**main.go changes**:
- Wrap proxy handler with CSRF middleware when setting up routes

**static/js/app.js changes**:
- Add `getCSRFToken()` function: reads session cookie value
- Modify `apiRequest()`: add X-CSRF-Token header for state-changing methods (POST, PATCH, PUT, DELETE)
- Handle 403 responses with CSRF-specific error message

**Dependencies**: Milestone 4 (auth layer required before CSRF)

---

### Milestone 6: Documentation

**Delegated to**: @agent-technical-writer (mode: post-implementation)

**Source**: `## Invisible Knowledge` section of this plan

**Files**:
- `pkg/proxy/README.md` (NEW - architecture, data flow, security model)
- `pkg/proxy/CLAUDE.md` (UPDATE - add new files, update descriptions)

**Requirements**:

Delegate to Technical Writer. For documentation format specification:

<file working-dir=".claude" uri="conventions/documentation.md" />

Key deliverables:
- CLAUDE.md: Pure navigation index (tabular format)
- README.md: Invisible knowledge (security model, migration guide, data flow)

**Acceptance Criteria**:
- CLAUDE.md is tabular index only (no prose sections)
- README.md exists with architecture diagrams matching this plan
- README.md includes migration instructions (gob→JSON)
- README.md documents security model (HTTPS detection, token encryption, CSRF)
- README.md is self-contained (no external references)

**Source Material**: `## Invisible Knowledge` section of this plan

---

## Milestone Dependencies

```
M1 (HTTPS) ──────┐
                 │
M2 (Path) ───────┤
                 ├──> M4 (Rate Limit) ──> M5 (CSRF) ──> M6 (Docs)
M3 (Crypto) ─────┘
```

**Parallelization**:
- Wave 1: M1, M2, M3 can execute in parallel (no file overlap)
- Wave 2: M4 after M3 (auth layer depends on session format)
- Wave 3: M5 after M4 (CSRF middleware extends auth layer)
- Wave 4: M6 after M5 (documentation captures final implementation)

**Deployment note**: No migration needed - proxy is unreleased. M3 deletes any .gob files on startup.
