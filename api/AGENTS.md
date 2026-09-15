# api/

REST API server: middleware chain, auth, rate limiting, config endpoints, embedded admin UI.

| File | What |
| ---- | ---- |
| `README.md` | Architecture, middleware layers, design decisions, security |
| `server.go` | HTTP server lifecycle, graceful shutdown, CORS/security wiring, admin UI serving |
| `handlers.go` | Config endpoints: GET, PATCH, PUT, validate, download, upload |
| `middleware.go` | Bearer auth (constant-time), rate limiting, CORS, security headers, request logging, trusted proxy, IP extraction |
| `response.go` | ErrorResponse/SuccessResponse + JSON helpers |
| `routes.go` | Route registration |
| `csrf.go` | CSRF utilities, token generation |
| `csrf_middleware.go` | CSRF middleware |
| `*_test.go` | Unit, integration, E2E, security, benchmark tests per component (`-run TestBearerAuth`, `TestRateLimit`, `TestE2E`) |

## Subdirectories

| Directory | What |
| --------- | ---- |
| `web/` | Frontend assets |
| `web/admin/` | Embedded admin SPA, vanilla JS — see `web/admin/AGENTS.md` |
