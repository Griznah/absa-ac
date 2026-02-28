# Proxy Package

Reverse proxy that translates HTTP Basic Auth to Bearer token authentication for browser-based API access.

## Architecture

```
Browser --[Basic Auth]--> Proxy (8080) --[Bearer Token]--> API (3001) --> ConfigManager
```

## Why This Exists

The admin UI at `/admin/` requires Bearer token authentication, which browsers cannot natively handle. Users had to manually enter tokens into the UI. This proxy enables browser-native HTTP Basic Auth dialogs while preserving the API's Bearer token requirement unchanged.

## Invariants

- API always requires Bearer token (proxy injects it, never modifies API auth)
- Basic Auth credentials sent with every request (use HTTPS in production)
- Proxy is optional - can run independently or disabled entirely
- Health endpoint (`/health`) bypasses authentication

## Tradeoffs

| Decision | Benefit | Cost |
| -------- | ------- | ---- |
| Basic Auth vs Bearer | Browser-native login dialog | Credentials sent with every request |
| Single credential pair | Simple configuration | No per-user audit trail |
| Separate port (8080) | Clean separation from API | Additional port management |

## Security

- Constant-time password comparison (prevents timing attacks)
- Fail-fast validation: missing/invalid credentials cause startup failure
- Password minimum: 8 characters (OWASP minimum)
- Auth failures logged with source IP

## Middleware Chain

Request flow (outside-in):

```
AccessLog -> BasicAuth -> ProxyHandler -> mux
```

All requests logged. Non-health requests require valid Basic Auth. Authenticated requests forwarded with Bearer token injection.
