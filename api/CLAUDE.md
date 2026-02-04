# API Package

| File | What | When to read |
| ---- | ---- | ------------ |
| `README.md` | Security architecture: timing attack prevention, IP spoofing protection, rate limiter cleanup, trusted proxy configuration | Understanding security features, deploying behind proxies, configuring trusted proxies, troubleshooting security issues |
| `server.go` | HTTP server with graceful shutdown, context management, CORS/security middleware integration | Understanding API lifecycle, startup/shutdown flow, server configuration |
| `handlers.go` | HTTP request handlers for config endpoints (GET, PATCH, PUT, validate) | Implementing new endpoints, modifying request/response handling |
| `middleware.go` | Authentication (Bearer token, constant-time compare), rate limiting (IP validation, incremental cleanup), CORS, security headers, request logging, trusted proxy validation | Adding middleware, modifying auth/security behavior, understanding IP extraction logic |
| `response.go` | Common response types (ErrorResponse, SuccessResponse) and JSON helpers | Understanding response format, adding new response types |
| `routes.go` | Route registration for all API endpoints | Adding new routes, modifying endpoint paths |
| `server_test.go` | Integration tests for HTTP server lifecycle and graceful shutdown | Verifying server behavior, testing shutdown scenarios |
| `middleware_test.go` | Tests for auth, rate limiting, CORS, security headers middleware, IP spoofing protection, cleanup lifecycle | Validating middleware behavior, edge cases, security scenarios |
| `middleware_benchmark_test.go` | Benchmarks for BearerAuth performance (valid vs invalid tokens) | Measuring authentication overhead, verifying constant-time comparison |
| `handlers_test.go` | Unit tests for config endpoint handlers (GET, PATCH, PUT, validate) | Testing handler logic, error cases |
| `e2e_test.go` | End-to-end integration tests with real HTTP client and server | Validating full request flows, large configs, unicode |
| `middleware_security_test.go` | Security-focused tests: timing attacks, IP spoofing, memory exhaustion, CORS bypass | Verifying security fixes, testing attack vectors |
| `integration_security_test.go` | Integration tests for middleware chain order, full request flow through all layers | Validating complete request processing, security layering |
| `benchmarks_test.go` | Performance benchmarks for config validation, deep merge, Bearer auth comparison | Measuring performance impact, optimizing operations |
| `static_test.go` | Tests for static file serving with path traversal protection | Validating static file security, path sanitization |
| `README.md` | Complete architecture documentation: component relationships, middleware layers, design decisions, tradeoffs, security considerations | Understanding API architecture, security design, why decisions were made |
