# Admin Frontend

Single-page web UI for AC Bot configuration management.

## Architecture Decisions

### DL-001: Embedded in Go Binary

Frontend embedded using Go `embed` package. Single binary deployment eliminates need for external web server. No separate deployment step required.

### DL-002: Vanilla JS with No Build Chain

No framework or build tooling. Zero build dependencies appropriate for small CRUD app. Trade-off: no type safety or ecosystem tooling, but gains simplicity.

### DL-003: sessionStorage for Token Storage

Bearer token stored in `sessionStorage` (cleared on tab close). Limits token exposure window compared to `localStorage` persistence. Note: both storage mechanisms are equally accessible to XSS attacks; the security benefit is reduced persistence only. Trade-off: no remember-me functionality.

### DL-004: CSRF Defense-in-Depth

CSRF middleware wired into API chain: SecurityHeaders -> CORS -> Logger -> RateLimit -> BearerAuth -> CSRF -> Handler. Frontend fetches CSRF token from `/api/csrf-token` and includes `X-CSRF-Token` header in state-changing requests.

### DL-005: /admin/ Route Separation

Admin UI served at `/admin/*`, separate from public `/health` endpoint. Clear URL structure.

## Security Design

### Token Storage

- Bearer token in `sessionStorage` (session-scoped, cleared on tab close)
- CSRF token in `sessionStorage` (fetched after login)
- Both auto-included in API requests via `api.js` wrapper

### XSS Prevention

- All user input escaped via `textContent` (never `innerHTML` for user content)
- `escapeHtml()` function sanitizes server names before rendering
- Strict CSP header (see below)

Server editor extended to edit all Server struct fields (name, port, category)
rather than just name and non-existent url field (ref: DL-001).
Category uses dropdown populated from category_order to ensure validity (ref: DL-003).
Global config fields (server_ip, update_interval, category_order, category_emojis)
added to enable full config editing via admin UI (ref: DL-002).

### CSRF Protection

- Token fetched from `/api/csrf-token` after successful login
- Included in `X-CSRF-Token` header for POST/PATCH/PUT/DELETE requests
- Login flow rolls back token on CSRF fetch failure

## CSP Override

Admin UI requires permissive CSP for inline scripts (vanilla JS has no build chain):

```
default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'
```

Applied via `withCSPForAdmin` middleware in `server.go`.

## Rate Limiting

API enforces 10 req/s with burst 20. If bulk operations trigger rate limits, increase `API_RATE_LIMIT` environment variable.

## File Load Order

```
index.html -> auth.js -> api.js -> app.js
```

`auth.js` exports `window.Auth`, `api.js` exports `window.APIClient` (depends on Auth), `app.js` uses both.

## Authentication Flow

1. User enters bearer token on login screen
2. Token format validated locally (32+ chars)
3. Token verified against `/api/config` endpoint (protected, requires valid auth)
4. CSRF token fetched from `/api/csrf-token`
5. Both tokens stored in `sessionStorage`
6. Tokens auto-included in all API requests
