# API Package

| File | What | When to read |
| ---- | ---- | ------------ |
| `server.go` | HTTP server with graceful shutdown, context management, middleware chain integration | Understanding API lifecycle, startup/shutdown flow, server configuration |
| `handlers.go` | HTTP request handlers for config endpoints (GET, PATCH, PUT, validate) with 1MB size limits and context cancellation | Implementing new endpoints, modifying request/response handling, understanding rate limiting |
| `middleware.go` | Authentication (timing-safe Bearer token), rate limiting (token bucket), CORS (strict allowlist), security headers, request logging | Adding middleware, modifying auth/security behavior, understanding middleware order |
| `csrf.go` | CSRF state storage, token generation, double-submit cookie pattern validation | Understanding CSRF protection, token lifecycle, state validation |
| `csrf_middleware.go` | CSRF middleware integration with request context, stateful validation | Adding CSRF to endpoints, debugging CSRF validation |
| `response.go` | Common response types (ErrorResponse, SuccessResponse) and JSON helpers | Understanding response format, adding new response types |
| `routes.go` | Route registration for all API endpoints | Adding new routes, modifying endpoint paths |
| `server_test.go` | Integration tests for HTTP server lifecycle, graceful shutdown, context cancellation | Verifying server behavior, testing shutdown scenarios |
| `middleware_test.go` | Tests for auth, rate limiting, CORS, security headers middleware | Validating middleware behavior, edge cases, security properties |
| `handlers_test.go` | Unit tests for config endpoint handlers (GET, PATCH, PUT, validate) | Testing handler logic, error cases, request size limits |
| `csrf_test.go` | Unit tests for CSRF token generation, validation, double-submit pattern | Verifying CSRF protection, edge cases, token validation |
| `e2e_test.go` | End-to-end integration tests with real HTTP client and server | Validating full request flows, large configs, unicode |
| `middleware_security_test.go` | Security-focused tests: timing attacks, IP spoofing, memory exhaustion, CORS bypass | Verifying security fixes, testing attack vectors |
| `integration_security_test.go` | Integration tests for middleware chain order, full request flow through all layers | Validating complete request processing, security layering |
| `benchmarks_test.go` | Performance benchmarks for config validation, deep merge, Bearer auth comparison | Measuring performance impact, optimizing operations |
| `static_test.go` | Tests for static file serving with path traversal protection | Validating static file security, path sanitization |
| `README.md` | Complete architecture documentation: component relationships, middleware layers, design decisions, tradeoffs, security considerations | Understanding API architecture, security design, why decisions were made |
