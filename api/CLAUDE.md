# API Package

| File | What | When to read |
| ---- | ---- | ------------ |
| `server.go` | HTTP server with graceful shutdown, context management, CORS/security middleware integration | Understanding API lifecycle, startup/shutdown flow, server configuration |
| `handlers.go` | HTTP request handlers for config endpoints (GET, PATCH, PUT, validate) | Implementing new endpoints, modifying request/response handling |
| `middleware.go` | Authentication (Bearer token), rate limiting, CORS, security headers, request logging | Adding middleware, modifying auth/security behavior |
| `response.go` | Common response types (ErrorResponse, SuccessResponse) and JSON helpers | Understanding response format, adding new response types |
| `routes.go` | Route registration for all API endpoints | Adding new routes, modifying endpoint paths |
| `server_test.go` | Integration tests for HTTP server lifecycle and graceful shutdown | Verifying server behavior, testing shutdown scenarios |
| `middleware_test.go` | Tests for auth, rate limiting, CORS, security headers middleware | Validating middleware behavior, edge cases |
| `handlers_test.go` | Unit tests for config endpoint handlers (GET, PATCH, PUT, validate) | Testing handler logic, error cases |
| `e2e_test.go` | End-to-end integration tests with real HTTP client and server | Validating full request flows, large configs, unicode |
