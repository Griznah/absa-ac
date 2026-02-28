# pkg/proxy/

Reverse proxy server for browser-based API access via HTTP Basic Auth.

## Index

| File | Contents | Read When |
| ---- | -------- | --------- |
| `config.go` | Config struct, environment loading, validation | Understanding proxy configuration, adding new env vars |
| `server.go` | HTTP server lifecycle, graceful shutdown, health endpoint | Modifying server behavior, debugging startup/shutdown |
| `auth.go` | BasicAuth middleware, constant-time comparison, client IP extraction | Debugging auth failures, modifying authentication logic |
| `handler.go` | ProxyHandler, Bearer token injection, hop-by-hop header filtering, upstream error handling | Modifying request forwarding, debugging upstream issues |
| `logging.go` | AccessLog middleware, response status capture | Adding request logging, debugging request flow |
| `config_test.go` | Config validation tests | Verifying config changes, adding new validation tests |
