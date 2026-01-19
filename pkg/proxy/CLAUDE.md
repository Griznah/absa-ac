# Proxy Package

Session-based authentication layer for secure API access.

## Files

| File | What | When to read |
| ---- | ---- | ------------ |
| `README.md` | Architecture, data flow, invariants, tradeoffs for session proxy | Understanding security model, session management, proxy design decisions |
| `session.go` | Session storage with file-based persistence and in-memory cache | Understanding session lifecycle, file operations, concurrent access patterns |
| `auth.go` | Authentication middleware, login/logout handlers, cookie management | Understanding auth flow, session validation, cookie security settings |
| `proxy.go` | Reverse proxy handler that adds Bearer token server-side | Understanding request forwarding, header handling, error responses |

## Subdirectories

None.
