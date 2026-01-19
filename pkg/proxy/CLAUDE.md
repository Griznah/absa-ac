# pkg/proxy/

Session-based authentication layer with encrypted token storage and CSRF protection.

## Files

| File | What | When to read |
| ---- | ---- | ------------ |
| `README.md` | Architecture, security model, data flow, invariants, tradeoffs | Understanding security design, session encryption, CSRF protection, migration guide |
| `session.go` | Session storage with JSON persistence and AES-256-GCM token encryption | Understanding session lifecycle, encryption/decryption, file operations, concurrent access |
| `auth.go` | Authentication handlers, rate limiting, CSRF middleware, cookie management | Understanding login/logout flow, rate limiting, CSRF validation, cookie security |
| `proxy.go` | Reverse proxy handler that adds Bearer token server-side | Understanding request forwarding, header handling, upstream timeout configuration |
| `session_test.go` | Integration tests for session storage and encryption | Verifying session operations, encryption behavior, path traversal protection |
| `auth_test.go` | Integration tests for authentication flow | Verifying login/logout, rate limiting, CSRF protection |
| `proxy_test.go` | Integration tests for request forwarding | Verifying proxy behavior, header copying, error handling |

## Subdirectories

None.
