# Reverse Proxy (Basic Auth → Bearer Token) — Decision Record

Admin UI required manual Bearer token entry, not browser-native. Built standalone reverse proxy package (`pkg/proxy`) that accepts HTTP Basic Auth, validates credentials, and forwards requests to the API with Bearer token injection. Runs on separate port (default 8080), optional via `PROXY_ENABLED`.

```
Browser --[Basic Auth]--> Proxy Server --[Bearer Token]--> API Server --> ConfigManager
```

Status: implemented. Code and git history are the implementation record; this file preserves design rationale.

## Decisions

| ID | Decision | Reasoning Chain |
|---|---|---|
| DL-001 | Create standalone proxy package (pkg/proxy) separate from api package | Proxy is optional/separate from main bot -> independent package allows separate binary deployment -> no modification to existing API auth (out of scope constraint) -> cleaner separation of concerns |
| DL-002 | Use HTTP Basic Auth (RFC 7617) for browser-native authentication | Browser handles Basic Auth dialog natively -> no custom login form needed -> credentials in Authorization header per RFC -> simpler UX for web access |
| DL-003 | Proxy injects Bearer token when forwarding to API | Existing API requires Bearer token (invariant) -> proxy validates Basic Auth credentials -> replaces Authorization header with Bearer token -> API unchanged |
| DL-004 | Proxy runs on configurable separate port (default 8080) | API already on port 3001 -> separate port allows independent operation -> configurable via PROXY_PORT env var |
| DL-005 | Single credential pair via environment variables (PROXY_USER, PROXY_PASSWORD) | Single user or shared credentials acceptable (M priority assumption) -> environment variables match existing API_BEARER_TOKEN pattern -> simple configuration |
| DL-006 | Proxy forwards to configurable API URL (default localhost:API_PORT) | Allows proxy to run on different host from API -> configurable via PROXY_API_URL env var -> supports containerized deployment |
| DL-007 | Constant-time password comparison using crypto/subtle.ConstantTimeCompare | Prevents timing attacks on password -> matches existing BearerAuth pattern in api/middleware.go -> security consistency |
| DL-008 | Proxy includes health endpoint at /health (no auth required) | Matches existing API health endpoint pattern -> allows load balancer health checks -> consistent observability |
| DL-009 | Proxy supports optional startup via PROXY_ENABLED environment variable | Optional component matches API_ENABLED pattern -> can run independently or as part of main binary -> flexible deployment |
| DL-010 | TLS termination handled by reverse proxy/load balancer, not proxy itself | Proxy is internal component -> TLS termination at edge (ingress/load balancer) -> follows standard deployment patterns -> simpler proxy implementation -> consistent with existing API deployment |
| DL-011 | HTTP client with 30s timeout, connection pooling (MaxIdleConns=10, IdleConnTimeout=90s), reuse across requests | Default Go http.Client has no timeout -> risk of hanging requests -> 30s reasonable for internal API calls -> connection pooling reduces latency -> reuse client for efficiency |
| DL-012 | Proxy does NOT implement rate limiting - relies on API rate limiting | API already has rate limiting middleware -> duplicate rate limiting adds complexity -> proxy is passthrough for auth translation -> consistent with out-of-scope for API modification |
| DL-013 | Graceful degradation: return 502 Bad Gateway on upstream failure, 504 Gateway Timeout on timeout, include error message in response body | Standard HTTP proxy error codes -> clients understand gateway errors -> error message aids debugging -> no retry logic (keep proxy simple) |
| DL-014 | Credential rotation is manual: update env vars and restart proxy | Single credential pair (assumption) -> rotation is rare operational task -> no runtime rotation needed -> restart is acceptable -> documented in operational procedures |
| DL-015 | Fail-fast: PROXY_ENABLED=true with missing/invalid credentials causes fatal error at startup | Security-sensitive component -> silent fallback could expose API -> explicit failure alerts operator -> no partial operation -> consistent with API_BEARER_TOKEN validation pattern |
| DL-016 | 8+ character minimum for password matches OWASP minimum and provides reasonable security margin | OWASP recommends minimum 8 characters -> shorter passwords vulnerable to brute force -> consistent with industry standards -> not excessive for operational use |

## Known Risks (accepted)

- Basic Auth sends credentials with every request: documented tradeoff, HTTPS recommended in production
- Single credential pair means no per-user audit trail: acceptable for single admin/small team; proxy logs source IP
- Proxy adds hop latency: proxy runs on same host as API by default, minimal impact
