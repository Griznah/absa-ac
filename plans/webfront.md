# Embedded Admin Frontend — Decision Record

Built web frontend for the REST API: authenticated users view and manage bot configuration at `/admin/`. Vanilla JS SPA embedded in the Go binary.

Status: implemented. Code and git history are the implementation record; this file preserves design rationale.

## Decisions

| ID | Decision | Reasoning Chain |
|---|---|---|
| DL-001 | Embed frontend in Go server using http.FileServer | User selected embedded serving -> minimal deployment complexity (single binary) -> no separate web server needed -> uses Go embed package |
| DL-002 | Single-page app with vanilla JS (no framework) | Constraints require minimal dependencies -> vanilla JS has zero build chain -> appropriate for CRUD operations on small config -> no complex state management needed |
| DL-003 | Store bearer token in sessionStorage after login | Single-user/shared token scenario -> sessionStorage cleared on tab close -> more secure than localStorage (XSS protection) -> token included in all API requests via Authorization header |
| DL-004 | Wire CSRF middleware into API server, frontend fetches token from /api/csrf-token and includes X-CSRF-Token header | CSRF middleware exists (csrf_middleware.go) + route registered (GET /api/csrf-token) -> middleware NOT in chain (server.go:74-88) -> wire it in for defense-in-depth -> frontend must fetch token on login and include in state-changing requests |
| DL-005 | Frontend route: /admin/ (redirect / to /admin/ for authenticated users) | Separation of public health endpoint from admin UI -> clear URL structure -> /health remains public -> /admin/* requires authentication |

## Known Risks (mitigations shipped)

- XSS would expose bearer token in sessionStorage: strict CSP (`default-src 'self'`, unsafe-inline for the no-build-chain SPA), textContent not innerHTML for user input
- Token lost on tab close: intentional (session-based auth), clear re-login UX
- Rate limiting during bulk edits: 10 req/s burst 20 suffices; raise `API_RATE_LIMIT` if needed

## Tradeoffs

- Vanilla JS with no build chain trades type safety/ecosystem tooling for simplicity and zero build dependencies (appropriate for small CRUD app)
- sessionStorage for token trades remember-me functionality for better XSS protection (session-based auth appropriate for admin tool)

## Middleware Chain

SecurityHeaders -> CORS -> Logger -> RateLimit -> BearerAuth -> CSRF -> Handler (CSRF wired in per DL-004, after auth, before handler)
