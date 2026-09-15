# pkg/proxy/

Reverse proxy for browser API access via HTTP Basic Auth.

| File | What |
| ---- | ---- |
| `README.md` | Architecture, invariants, tradeoffs |
| `config.go` | Config struct, env loading, validation |
| `server.go` | Server lifecycle, graceful shutdown, health endpoint |
| `auth.go` | BasicAuth middleware, constant-time compare, client IP extraction |
| `handler.go` | ProxyHandler: Bearer injection, hop-by-hop header filtering, upstream errors |
| `logging.go` | AccessLog middleware, response status capture |
| `config_test.go` | Config validation tests |
