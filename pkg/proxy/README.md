# Proxy Package

Session-based authentication layer that proxies API requests with AES-256-GCM token encryption, CSRF protection, and JSON persistence.

## Overview

The proxy package provides a secure authentication layer for the bot API. Frontend authenticates once via Bearer token, receives an HTTP-only session cookie, and all subsequent API requests use the session cookie instead of the Bearer token. The backend validates sessions, decrypts tokens server-side, and proxies requests to the existing API.

**Security model**: Tokens are encrypted at rest using AES-256-GCM, stored as JSON files, and never exposed to browser JavaScript. Login endpoint is rate-limited to prevent DoS attacks. State-changing requests require double-submit cookie CSRF protection.

## Architecture

```
HTTP Request --> Proxy (/proxy/api/*)
                   |
                   v
           AuthMiddleware (validate session cookie)
                   |
                   v
        CSRFMiddleware (validate X-CSRF-Token header)
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
1. **Login**: User POSTs Bearer token -> validate against bot API -> encrypt token -> create session JSON -> set HttpOnly cookie
2. **Proxy**: Request arrives with cookie -> load session JSON -> decrypt token -> add Authorization header -> forward upstream
3. **Logout**: Delete session file -> clear cookie

## Components

### SessionStore (`session.go`)

File-based session storage with in-memory cache and AES-256-GCM token encryption.

**Responsibilities**:
- Create session: Generate 16-byte session ID, encrypt token, write JSON file, add to in-memory map
- Read session: Retrieve from in-memory map, validate expiration, decrypt token
- Delete session: Remove from map, delete JSON file
- Cleanup expired sessions: Scan JSON files, delete expired, update map
- Background cleanup: Goroutine runs every 5 minutes

**Thread safety**: `sync.RWMutex` protects in-memory map. Concurrent reads allowed during request validation. Write lock only on session creation/deletion.

**Encryption**: Tokens encrypted with AES-256-GCM before storage. Key sourced from `SESSION_ENCRYPTION_KEY` environment variable (base64-encoded 32 bytes). Each encryption uses unique nonce (prepended to ciphertext). Ciphertext stored as base64 in JSON.

**Migration**: On startup, any existing `.gob` files are deleted with warning logged (no migration - proxy was unreleased).

### AuthMiddleware & CSRFMiddleware (`auth.go`)

Session validation and CSRF protection middleware.

**AuthMiddleware**:
- Extract session cookie from request
- Validate session exists and not expired
- Set session in request context for downstream handlers
- Return 401 Unauthorized for invalid/missing sessions

**CSRFMiddleware**:
- Extract session cookie and X-CSRF-Token header
- Validate header matches cookie value (double-submit pattern)
- Allow GET requests to bypass CSRF check
- Return 403 Forbidden for mismatched tokens

**LoginHandler**: Validates Bearer token against bot API, encrypts token, creates session, sets HTTP-only cookie. Rate-limited to 5 attempts per IP per 60 seconds. Successful login resets counter.

**LogoutHandler**: Deletes session, clears cookie.

**Rate limiting**: Fixed-window algorithm with lazy cleanup. WARNING: Unbounded memory growth if many IPs make one-time attempts (documented limitation - acceptable for single-server deployment).

### ProxyHandler (`proxy.go`)

Reverse proxy that adds Bearer token server-side.

**Responsibilities**:
- Extract session from context (set by middleware)
- Retrieve Bearer token from session (already decrypted)
- Forward request to bot API with Authorization header
- Copy response headers (excluding hop-by-hop headers)
- Return upstream response to client

**Upstream timeout**: Configurable via `PROXY_UPSTREAM_TIMEOUT` environment variable (defaults to 10 seconds).

## Data Flow

### Login Flow

```
Client (Bearer token) --> POST /proxy/login
                         |
                         v
                    validateBearerTokenWithBotAPI()
                         |
                         v
                    encryptToken() (AES-256-GCM)
                         |
                         v
                    sessionStore.Create()
                    (writes JSON with encrypted token)
                         |
                         v
                    SetSessionCookie
                    (HttpOnly, Secure if PROXY_HTTPS=true,
                     SameSite=Strict, Path=/proxy)
```

### Proxy Flow

```
Client (session cookie) --> GET /proxy/api/config
                           |
                           v
                    AuthMiddleware extracts session ID
                           |
                           v
                    CSRFMiddleware validates X-CSRF-Token
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

### Session File Format

**JSON structure** (`sessions/{id}.json`):
```json
{
  "id": "base64url-encoded-session-id",
  "encrypted_token": "base64url-encoded-aes-gcm-ciphertext",
  "expires": "2026-01-19T12:00:00Z",
  "created": "2026-01-19T08:00:00Z",
  "last_accessed": "2026-01-19T11:30:00Z"
}
```

**Migration from gob**: JSON replaced unstable gob encoding. No backward compatibility - `.gob` files deleted on startup. JSON is human-readable for debugging and stable across Go versions.

## Security Model

### Token Encryption

**Algorithm**: AES-256-GCM (authenticated encryption)

**Key management**:
- Key sourced from `SESSION_ENCRYPTION_KEY` environment variable
- Must be base64-encoded 32-byte value
- Stored in memory only (never written to disk)
- Missing or invalid key prevents session store creation

**Encryption flow**:
1. Generate unique 12-byte nonce for each encryption
2. Encrypt token with AES-256-GCM (nonce + ciphertext + auth tag)
3. Base64-encode for JSON serialization
4. Store in `encrypted_token` field

**Decryption flow**:
1. Base64-decode ciphertext
2. Split nonce (first 12 bytes) and encrypted data
3. Decrypt and authenticate with AES-256-GCM
4. Return plaintext token (or error if tampered)

**Performance**: ~500ns per decrypt operation (hot path). ~1ms per encrypt (login flow - not hot path).

### HTTPS Detection

**Environment variable**: `PROXY_HTTPS=true` enables Secure cookie flag.

**Rationale**: Auto-detect from request can be fooled by reverse proxies (request.Scheme always "http"). Environment variable provides explicit operator control.

**Cookie attributes**:
- `Secure=true` when `PROXY_HTTPS=true`
- `Secure=false` when unset or `false` (allows HTTP dev setups)
- `HttpOnly=true` (blocks JavaScript access)
- `SameSite=Strict` (prevents CSRF)
- `Path=/proxy` (scopes cookie to proxy routes)
- `MaxAge=14400` (4 hours)

### CSRF Protection

**Pattern**: Double-submit cookie

**Implementation**:
- Frontend sends session cookie value in `X-CSRF-Token` header
- Backend validates header matches cookie
- GET requests bypass CSRF check (read-only)
- POST/PATCH/PUT/DELETE require matching token

**Rationale**: Session cookie is HttpOnly (JavaScript cannot read). Double-submit uses same value for cookie and header. No server-side state needed.

### Path Traversal Protection

**Validation**: Session IDs validated before file operations.

**Rules**:
- Length >= 1 character
- Only base64url characters (A-Za-z0-9_-)
- No path separators (/ or \)
- Rejected early with `ErrInvalidSessionID`

**Locations**: `Get()`, `Delete()`, `loadExistingSessions()` all validate before filesystem access.

### Rate Limiting

**Algorithm**: Fixed-window (60 seconds)

**Threshold**: 5 failed login attempts per IP per window

**Behavior**:
- Failed attempt increments counter
- Successful login resets counter
- Returns 429 Too Many Requests when exceeded
- `Retry-After: 60` header set

**Known limitation**: Lazy cleanup only (expired entries removed on next check). Unbounded memory growth if many IPs make one-time attempts. Acceptable for single-server deployment.

### Session Security

**Session ID entropy**: 16 bytes (128 bits) -> 10^18 collision resistance

**File permissions**: 0600 (owner read/write only) on session directory and files

**Timeout**: 4 hours from creation (not rolling expiry)

**Cleanup**: Background goroutine every 5 minutes removes expired files

## Invariants

1. **Session consistency**: Session JSON file always exists before in-memory map entry
   - Write file first, then add to map
   - Delete from map first, then delete file
   - Prevents serving non-existent sessions

2. **Bearer token isolation**: Frontend never receives Bearer token after authentication
   - Token encrypted before storage (never plaintext in JSON)
   - HTTP-only cookie prevents JavaScript access
   - Proxied requests add token server-side (client never sees it)

3. **Token encryption at rest**: Encrypted token in JSON, plaintext token in memory only
   - `EncryptedToken` field stored in JSON (base64 ciphertext)
   - `Token` field in memory only (tagged `json:"-"`)
   - Decryption happens during `Get()` for every request

4. **Session ID format**: All session IDs are base64url-encoded 16-byte values
   - 128 bits entropy from crypto/rand
   - URL-safe charset (no + or / characters)
   - Validated before filesystem operations

5. **Cookie scope**: Session cookie always scoped to `/proxy` path
   - Prevents cookie leakage to other application paths
   - SameSite=Strict prevents cross-origin attacks
   - Secure flag set only when operator confirms HTTPS

6. **Session timeout**: 4 hours from creation (fixed, not rolling)
   - `Expires` field set at creation time
   - Not updated on activity
   - Consistent with security model for admin tool

## Design Decisions

### JSON over gob for Session Persistence

**Rationale**:
- gob is unstable across Go versions (session files break after upgrades)
- gob is not human-readable for debugging production issues at 3 AM
- JSON is stable, readable, and has stdlib support
- Faster migration without backward compatibility needed (proxy unreleased)

**Tradeoff**: JSON is ~30% larger than gob. Disk space is cheap; debuggability is critical.

### Environment Variable for HTTPS Detection

**Rationale**:
- Auto-detect from request can be fooled by reverse proxies
- `request.Scheme` always "http" when SSL terminates at reverse proxy
- `Secure=true` breaks HTTP dev setups
- `PROXY_HTTPS=true` provides explicit operator control

**Tradeoff**: Manual configuration required (but prevents security misconfig).

### AES-256-GCM for Token Encryption

**Rationale**:
- Tokens must not be stored in plaintext
- Symmetric encryption avoids per-token key management overhead
- GCM provides authenticated encryption (prevents tampering)
- 256-bit key is industry standard for sensitive data
- Fast operation (~500ns per decrypt)

**Tradeoff**: Encryption on every Create() adds ~1ms overhead. Acceptable for login flow (not hot path).

### Token-Encrypted-at-Rest Pattern

**Rationale**:
- Encrypting entire session JSON would require decrypting for every read (hot path)
- Only Bearer token is sensitive; other fields (ID, timestamps) are not
- Encrypt token separately, store alongside plaintext metadata
- Faster Get() operations (decrypt token only, not entire session)

**Tradeoff**: More complex Session struct (two token fields). Worth it for performance.

### Fixed-Window Rate Limiting

**Rationale**:
- Token bucket requires stateful counter per IP (complexity)
- Fixed-window with in-memory map is simpler
- DoS protection goal is basic throttling, not perfect rate shaping
- Acceptable tradeoff for single-server deployment

**Tradeoff**: Allows bursts at window boundary. Acceptable for basic DoS protection.

### Double-Submit Cookie CSRF

**Rationale**:
- Original CSRF token system removed in frontend
- Session cookie is HttpOnly (JavaScript cannot read it)
- Double-submit pattern uses same cookie value as header
- No server-side state needed
- Works with proxy architecture

**Tradeoff**: Less flexible than server-side tokens. Simpler and sufficient.

### Migrate Without Backward Compatibility

**Rationale**:
- User explicitly stated no gob support needed
- Proxy unreleased so no existing sessions to migrate
- Simply delete any .gob files on startup
- Simpler code, no migration tool needed

**Tradeoff**: Breaking change (no users affected).

### Filename Sanitization for Path Traversal

**Rationale**:
- `os.ReadDir` + `filepath.Join` trusts directory contents
- Attacker who can create files in `sessions_dir` could escape
- Validate session IDs match base64 charset before file operations
- Reject malformed filenames early

**Tradeoff**: Extra validation step. Prevents critical security vulnerability.

## Migration Guide

### From gob to JSON (Version Upgrade)

**No action required**. Migration is automatic:

1. Set `SESSION_ENCRYPTION_KEY` environment variable (base64-encoded 32 bytes)
2. Start proxy
3. Any existing `.gob` files are deleted with warning logged
4. New sessions created as JSON with encrypted tokens

**Generating encryption key**:
```bash
# Generate 32 random bytes and base64-encode
openssl rand -base64 32
```

**Set environment variable**:
```bash
export SESSION_ENCRYPTION_KEY="<base64-encoded-key>"
```

### From Plaintext to Encrypted Tokens

**No action required**. All tokens encrypted automatically when sessions are created. Existing sessions invalidated on next login (token re-encrypted).

## Environment Variables

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `SESSION_ENCRYPTION_KEY` | Yes | - | Base64-encoded 32-byte AES-256 key for token encryption |
| `PROXY_HTTPS` | No | `false` | Set `true` to enable Secure cookie flag (HTTPS deployments) |
| `PROXY_UPSTREAM_TIMEOUT` | No | `10s` | Timeout for upstream bot API requests (duration string) |

## Constants

| Constant | Value | Rationale |
|----------|-------|-----------|
| `defaultSessionTimeout` | 4 hours | Balances security and UX for admin tool |
| `cleanupInterval` | 5 minutes | Balances stale session removal with CPU usage |
| `sessionIDLength` | 16 bytes (128 bits) | Sufficient collision resistance for single-instance |
| `defaultUpstreamTimeout` | 10 seconds | Fast error reporting for unresponsive bot API |
| `keySize` | 32 bytes | AES-256 key size requirement |
| `ENCRYPTION_KEY_ENV` | `"SESSION_ENCRYPTION_KEY"` | Environment variable name for encryption key |
| `windowDuration` | 60 seconds | Rate limiting window for login attempts |
| `maxAttempts` | 5 | Failed login attempts before rate limit |

## Tradeoffs

### Performance vs Security

- **Token encryption**: ~1ms overhead on Create() (login flow - acceptable). ~500ns per Get() (hot path - negligible).
- **CSRF validation**: Additional header check on every request (minimal overhead).
- **Session file loading**: One file read per request from cold cache (acceptable for admin tool traffic).

### Simplicity vs Perfect Rate Limiting

- **Fixed-window allows bursts at window boundary**: Simpler implementation than token bucket. Acceptable for basic DoS protection in single-server deployment.
- **Lazy cleanup causes unbounded memory growth**: Documented limitation. Mitigation: add background cleanup goroutine if needed for high-traffic deployments.

### Debuggability vs Disk Space

- **JSON is ~30% larger than gob**: Human-readable for production debugging. Disk space is cheap; operational simplicity is critical.

### Migration Complexity vs Backward Compatibility

- **No migration tool**: One-shot deletion of .gob files. Simpler code, faster implementation. No users affected (proxy unreleased).
