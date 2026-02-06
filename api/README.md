# API Package

REST API for dynamic configuration management of the AC Discord Bot. Provides HTTP endpoints for reading, updating, and validating bot configuration at runtime without restarting the service.

## Security Architecture

The API implements defense-in-depth security through multiple middleware layers:

```
HTTP Request → Security Headers → CORS → Logger → Rate Limit → Bearer Auth → Handler
                                                                    ↑
                                                            Trusted Proxy Check
                                                                    ↑
                                                        Structured Logging (security events)
```

**Middleware order rationale**: Rate limiting happens BEFORE authentication to prevent DoS on auth validation. IP spoofing protection ensures rate limiting cannot be bypassed through X-Forwarded-For header manipulation.

## Architecture

### Component Overview

```
┌─────────────────────────────────────────────────────────────────┐
│                         Bot Struct                              │
│  ┌─────────────┐  ┌──────────────┐  ┌─────────────────────────┐│
│  │ DiscordGo  │  │ConfigManager │  │   API Server (optional)  ││
│  │   Session   │  │              │  │  ┌────────┐  ┌────────┐ ││
│  └─────────────┘  └──────────────┘  │  │Middleware│  │Handlers│ ││
│         │                 │          │  └────────┘  └────────┘ ││
│         └─────────────────┼──────────┴─────────────────────────┘│
│                           ▼                                       │
│                    Shared State (RWMutex)                        │
│                   - Current Config                               │
│                   - Server IPs                                    │
└─────────────────────────────────────────────────────────────────┘
```

**Component relationships:**
- Bot owns all dependencies (Discord session, ConfigManager, optional API server)
- ConfigManager owns config state with RWMutex for concurrent access
- API server shares ConfigManager reference with Discord bot
- No global state - all dependencies injected via constructors

**Data flow:**
1. Bot.Start() launches Discord bot and optional API server concurrently
2. API handlers call ConfigManager methods (GetConfig, UpdateConfig, WriteConfig)
3. ConfigManager serializes writes with RWMutex, updates in-memory config
4. Discord bot reads config via ConfigManager.GetConfig() (RLock)
5. File watcher (ConfigManager) detects mtime changes, reloads with debounce

### API Server Architecture

```
HTTP Request
    │
    ▼
┌──────────────────────────────────────────────────────────┐
│ Middleware Chain (order matters)                         │
│  1. SecurityHeaders    - outermost (applies to all)     │
│  2. CORS              - cross-origin checks              │
│  3. Logger            - request logging                  │
│  4. RateLimit         - throttling before expensive auth │
│  5. BearerAuth        - innermost (token validation)     │
└──────────────────────────────────────────────────────────┘
    │
    ▼
┌──────────────────────────────────────────────────────────┐
│ Route Handler (handlers.go)                              │
│  - Context cancellation check                            │
│  - Request size limit (1MB)                              │
│  - JSON decode                                           │
│  - ConfigManager method call                             │
└──────────────────────────────────────────────────────────┘
    │
    ▼
┌──────────────────────────────────────────────────────────┐
│ ConfigManager (main.go)                                  │
│  - RWMutex for concurrent access                         │
│  - Deep merge for partial updates                        │
│  - Atomic file write with backup rotation                │
└──────────────────────────────────────────────────────────┘
```

## Middleware Layers

### Security Headers (Outermost)
Applied to all responses including errors. Prevents XSS, clickjacking, MIME sniffing.

**Headers:**
- `X-Content-Type-Options: nosniff` - Prevents MIME type sniffing
- `X-Frame-Options: DENY` - Prevents clickjacking
- `X-XSS-Protection: 1; mode=block` - XSS protection for legacy browsers
- `Content-Security-Policy: default-src 'self'` - Restricts content sources
- `Referrer-Policy: strict-origin-when-cross-origin` - Controls referrer information

### CORS (Second Layer)
Cross-Origin Resource Sharing checks before authentication.

**Behavior:**
- Empty origin list = no CORS headers (same-origin only)
- `"*"` = allow all origins (development only)
- Specific origins = strict allowlist validation
- Rejects `"*"` combined with specific origins (ambiguous security policy)

**Preflight requests:** Returns 204 No Content with allowed methods and headers.

### Logger (Third Layer)
Logs all requests with method, path, status code, and duration.

**Redaction:** Authorization header replaced with `Bearer <redacted>` before logging.

### Rate Limiting (Fourth Layer)
Token bucket rate limiting per client IP before expensive authentication.

**Algorithm:**
- 10 requests/second default (configurable via `API_RATE_LIMIT` env var)
- Burst of 20 default (configurable via `API_RATE_BURST` env var)
- Per-IP limiters with 5-minute expiration
- Health check `/health` bypasses rate limiting

**IP extraction:**
- Uses `RemoteAddr` by default
- Extracts rightmost IP from `X-Forwarded-For` header (trusts last proxy, not client)
- Prevents IP spoofing bypass attempts

**Memory management:** Inline cleanup on each request removes limiters inactive for >5 minutes.

### Bearer Authentication (Innermost)
Validates OAuth2 Bearer tokens per RFC 6750.

**Timing-safe comparison:** Uses `crypto/subtle.ConstantTimeCompare` to prevent timing attack vectors where attacker measures response time to guess token byte-by-byte.

**Health check bypass:** `/health` endpoint requires no authentication.

## Configuration Endpoints

### GET /health
Public health check endpoint. Returns 200 OK if API server is running.

**Response:**
```json
{
  "status": "ok",
  "service": "ac-bot-api"
}
```

### GET /api/config
Returns current bot configuration.

**Authentication:** Required
**Response:** Full config object

### GET /api/config/servers
Returns only the servers list from current configuration.

**Authentication:** Required
**Response:** Servers array

### PATCH /api/config
Applies partial configuration update (deep merge).

**Authentication:** Required
**Request body:** JSON with partial config fields
**Response:** Updated full config

**Merge behavior:**
- Top-level fields: merged recursively
- `servers` array: merge by server name (preserves existing servers)
- New servers appended to array
- Existing servers updated by matching name

### PUT /api/config
Replaces entire configuration.

**Authentication:** Required
**Request body:** JSON with complete config
**Response:** Updated full config

### POST /api/config/validate
Validates configuration without applying it.

**Authentication:** Required
**Request body:** JSON config to validate
**Response:**
```json
{
  "valid": true,
  "message": "Config JSON is valid (full validation requires ConfigManager type)"
}
```

## Security Features

### Timing Attack Prevention

Bearer token authentication uses `crypto/subtle.ConstantTimeCompare` instead of direct string comparison. This prevents timing side-channel attacks where an attacker measures response times to brute-force tokens character-by-character.

**How it works**: Unlike `!=` which fails fast on the first mismatch, `ConstantTimeCompare` always processes the entire string in constant time regardless of where the mismatch occurs.

**Performance impact**: Benchmarks show ~1,932 ns/op for valid tokens and ~3,063 ns/op for invalid tokens (including structured logging overhead). Authentication is not on the hot path (executes once per request), so this impact is negligible.

### IP Spoofing Prevention

The API validates `X-Forwarded-For` headers only when requests come from trusted proxies. This prevents rate limit bypass through header spoofing.

**IP extraction algorithm**:
1. Check if `X-Forwarded-For` header exists and trusted proxies are configured
2. If no header or no trusted proxies, use `RemoteAddr` (secure default)
3. If header exists, verify request comes from trusted proxy IP
4. Parse comma-separated IP list (max 10 IPs to prevent CPU DoS)
5. Validate each IP is routable (reject loopback, link-local, multicast)
6. Extract rightmost non-trusted IP as client IP
7. Fallback to `RemoteAddr` if any validation fails

**Rejected IP ranges**: `isRoutableIP()` rejects 0.0.0.0, ::1 (loopback), 169.254.0.0/16, fe80::/10 (link-local), ff00::/8 (multicast). These ranges shouldn't appear in legitimate client requests and are commonly used in spoofing attempts.

**IPv4-mapped IPv6 normalization**: Addresses like `::ffff:192.168.1.1` are normalized to `192.168.1.1` to prevent bypass via different representations of the same IP.

### Rate Limiter Memory Leak Protection

Rate limiters are cleaned up incrementally to prevent unbounded memory growth during DoS attacks or extended operation.

**Cleanup strategy**: Every 5 minutes, a background goroutine processes 1,000 entries from the rate limiter map. Entries not accessed within 5 minutes are deleted. The cursor tracks progress across cleanup cycles.

**Why incremental**: Full O(n) cleanup blocks all rate limit checks during iteration. With 100k entries, this causes 100ms+ blocking every 5 minutes. Incremental processing spreads work across multiple ticks, preventing request blocking.

**Lifecycle coordination**: Cleanup goroutine starts with the server and stops gracefully via `context.Context` cancellation. No goroutine leaks on shutdown. Panic recovery ensures transient bugs don't crash cleanup permanently.

### Fail-Fast Configuration Validation

Malformed IP addresses in `API_TRUSTED_PROXY_IPS` cause immediate exit at startup. This prevents silent misconfiguration where admin thinks proxy is trusted but isn't.

**Example**:
```bash
# Typo causes immediate failure
export API_TRUSTED_PROXY_IPS="192.168.1.1,invalid-ip"
# Server exits: "failed to parse trusted proxy IP 'invalid-ip': invalid IP"
```

## Middleware Details

### BearerAuth

**Purpose**: Validates OAuth2 Bearer tokens using constant-time comparison.

**Flow**:
1. Bypass auth for `/health` endpoint
2. Extract `Authorization` header
3. Validate "Bearer " prefix
4. Compare token value with `ConstantTimeCompare`
5. Log authentication attempt (token redacted)
6. Return 401 if mismatch, pass to next handler if match

**Security**: Always executes full comparison regardless of mismatch position. Response time is independent of token length or match position.

### RateLimit

**Purpose**: Token bucket rate limiting per client IP with memory leak protection.

**IP extraction**: Calls `extractClientIP()` with trusted proxy validation (see IP spoofing prevention).

**Rate limiter lifecycle**:
1. Look up limiter for client IP (with mutex protection)
2. Create new limiter if not exists (double-checked locking)
3. Update `lastAccess` timestamp
4. Check if request allowed within rate limit
5. Return 429 if exceeded, pass to next handler if allowed

**Background cleanup**: Every 5 minutes, process 1,000 entries and delete those with `lastAccess` older than 5 minutes.

### Trusted Proxy Configuration

**Environment variable**: `API_TRUSTED_PROXY_IPS` (comma-separated list, empty default)

**Behavior**:
- Empty (default): Ignores `X-Forwarded-For` entirely, uses `RemoteAddr`
- Configured: Validates `X-Forwarded-For` only from trusted proxy IPs
- Malformed: Exits immediately at startup (fail-fast)

**Deployment patterns**:
- No proxy: Leave empty (secure default)
- Single proxy: Set to proxy IP (e.g., `10.0.0.1`)
- CDN + proxy: Set to both IPs (e.g., `10.0.0.1,10.0.0.2`)

## Observability

### Structured Logging

Security events are logged with `log/slog` for forensic analysis:

| Event | Fields | Severity |
| ----- | ------ | -------- |
| Authentication failure | success, reason, ip, token (redacted) | INFO |
| Authentication success | success, ip | INFO |
| IP spoofing detected | reason, xff_header, remote_addr, trusted_proxies | WARN |
| Rate limit exceeded | ip, path | INFO (logged by rate limiter) |
| Cleanup event | entries_processed, entries_deleted, total_entries | INFO |
| Cleanup panic | panic, stack | ERROR |

**Why structured logging**: No existing observability infrastructure. Prometheus adds dependency and operational complexity. Structured logging provides forensic data with stdlib only. Logs can be shipped to external aggregators if needed.

### Interpreting Security Logs

**IP spoofing alerts**: Look for `ip_spoof_detected` with `reason` field:
- `xff_from_untrusted_source`: Client sent X-Forwarded-For but isn't trusted proxy
- `too_many_ips_in_xff`: Header contains more than 10 IPs (CPU DoS attempt)
- `invalid_or_non_routable_ip`: Header contains loopback/link-local/multicast IP
- `all_ips_are_trusted_proxies`: All IPs in chain are trusted proxies (fallback to RemoteAddr)

**Cleanup monitoring**: Track `rate_limit_cleanup` logs to verify memory management:
- `entries_processed`: Should be ~1,000 per tick
- `entries_deleted`: High counts indicate traffic spikes or churn
- `total_entries`: Should stabilize, not grow indefinitely

## Performance

### Benchmarks

Measured on Intel Xeon E3-1230 v5 @ 3.40GHz:

| Benchmark | ns/op | B/op | allocs/op |
| --------- | ----- | ---- | --------- |
| ValidToken | 2,190 | 224 | 5 |
| InvalidToken | 3,702 | 1,073 | 11 |

**Overhead breakdown**: Invalid tokens are slower due to structured logging (token redaction, IP extraction, log write). The constant-time comparison itself adds minimal overhead vs direct string comparison.

### Memory Usage

**Rate limiter memory**: Approximately 1KB per unique IP (rate.Limiter struct + metadata). With 5-minute expiry, memory stabilizes under normal traffic.

**Cleanup overhead**: 1,000 entries processed per tick = ~1ms cleanup time. Spread across 5 minutes = negligible impact on request handling.

## Deployment

### Environment Variables

```bash
# Enable API server
API_ENABLED=true

# Server configuration
API_PORT=3001
API_BEARER_TOKEN=your-secure-random-token-here

# CORS (optional)
API_CORS_ORIGINS=https://example.com,https://app.com

# Trusted proxy IPs (comma-separated, empty default)
API_TRUSTED_PROXY_IPS=10.0.0.1,10.0.0.2
```

### Configuring Trusted Proxies

**When to configure**: Deploy behind reverse proxy (nginx, AWS ALB, Cloudflare).

**When to leave empty**: Direct internet exposure, no proxy.

**Finding proxy IPs**:
```bash
# AWS ALB: Get network load balancer IPs
aws elbv2 describe-load-balancers --names my-alb --query 'LoadBalancers[0].AvailabilityZones[*].LoadBalancerAddresses[*].IpAddress'

# Nginx proxy: Check upstream configuration
curl http://localhost/api/config  # Check RemoteAddr in logs
```

### Migration Guide

**Existing deployments (no proxy)**: No changes needed. Empty `API_TRUSTED_PROXY_IPS` is secure default.

**Adding proxy to existing deployment**:
1. Add proxy IP to `API_TRUSTED_PROXY_IPS`
2. Restart server
3. Monitor logs for `ip_spoof_detected` warnings
4. Verify rate limiting works with forwarded IPs

**Removing proxy**:
1. Remove or set `API_TRUSTED_PROXY_IPS` to empty
2. Restart server
3. Rate limiting falls back to `RemoteAddr`

**Common deployment patterns**:

| Pattern | `API_TRUSTED_PROXY_IPS` | Notes |
| ------- | ---------------------- | ----- |
| Direct internet exposure | (empty) | Secure default |
| Single nginx proxy | `10.0.0.1` | Check nginx upstream IP |
| Multiple proxies | `10.0.0.1,10.0.0.2` | List all proxy IPs |
| Cloudflare | (empty) + Cloudflare auth | Don't trust CF IPs directly |
| CDN + nginx | `10.0.0.1,10.0.0.2` | List all proxy IPs |

## Troubleshooting

### Requests Being Rate-Limited Unexpectedly

**Symptom**: Legitimate requests return 429 Too Many Requests.

**Diagnosis**:
1. Check logs for `ip_spoof_detected` warnings
2. Verify `API_TRUSTED_PROXY_IPS` matches proxy IP
3. Check if requests coming through proxy or direct to server
4. Verify `X-Forwarded-For` header format

**Solution**: If using proxy, ensure `API_TRUSTED_PROXY_IPS` is set correctly. If not, rate limiting is per-client-IP, which is expected behavior.

### Proxy Configuration Issues

**Symptom**: Server exits at startup with "failed to parse trusted proxy IP".

**Diagnosis**: Malformed IP in `API_TRUSTED_PROXY_IPS` (e.g., typo, invalid format).

**Solution**: Fix IP address and restart. Server validates IPs at startup to prevent silent misconfiguration.

**Symptom**: All requests show same IP in logs.

**Diagnosis**: Trusted proxy not configured, so `RemoteAddr` is proxy IP not client IP.

**Solution**: Add proxy IP to `API_TRUSTED_PROXY_IPS` and restart.

### Memory Leak Detection

**Symptom**: Memory grows indefinitely over time.

**Diagnosis**: Check logs for `rate_limit_cleanup` events. If absent, cleanup goroutine crashed.

**Solution**: Look for `rate_limit_cleanup_panic` in ERROR logs. Cleanup auto-restarts after 1 minute. If crashes persist, file bug with stack trace.

**Monitoring**: Add alert if `total_entries` in cleanup logs exceeds threshold (e.g., 100k entries indicates unusual traffic pattern or attack).

### Debugging Timing Issues

**Symptom**: Authentication times vary significantly.

**Diagnosis**: Direct string comparison (not constant-time) allows timing leakage.

**Solution**: Verify code uses `crypto/subtle.ConstantTimeCompare`. Run benchmarks to confirm consistent timing: `go test -bench=BenchmarkBearerAuth ./api/`.

## Invariants

### Security Invariants

1. **Constant-time comparison always executes fully**: `ConstantTimeCompare` processes entire string regardless of mismatch position. Prevents timing leakage.
2. **Trusted proxy list is immutable after startup**: Environment variables read once during initialization. Changing requires restart. Prevents race conditions during reconfiguration.
3. **X-Forwarded-For chain length is limited**: Headers with more than 10 IPs rejected. Bounds parsing cost, prevents CPU DoS.
4. **Rate limiter map is protected by mutex**: All accesses (read and write) acquire mutex. Cleanup goroutine and request handlers coordinate via lock. Incremental cleanup minimizes hold time.
5. **Client IP extraction is deterministic**: Same request headers and configuration always return same result. No random or stateful behavior.
6. **Cleanup goroutine stops on server shutdown**: Ticker stopped via `ctx.Done()`. No goroutine leaks on shutdown. Panic recovery ensures transient bugs don't crash cleanup permanently.
7. **IP addresses are normalized before comparison**: IPv4-mapped IPv6 normalized to IPv4 form. Prevents bypass via different representations.
8. **Non-routable IPs are rejected**: Loopback, link-local, multicast IPs rejected. Prevents spoofing via reserved ranges.

### Configuration Invariants

1. **Config consistency**: All config reads (Discord bot and HTTP API) see a complete, valid config via atomic.Value. Never partial state.
2. **Validation uniformity**: API and file reload use identical validation logic (`validateConfigStructSafeRuntime`). No special cases.
3. **Write atomicity**: Config file is never partially written. Temp file + rename ensures all-or-nothing updates.
4. **Backup availability**: Every write creates a `.backup` file. Failed updates can be manually rolled back.
5. **Goroutine independence**: HTTP server and Discord bot run in separate goroutines. Neither can block the other.
6. **Mtime-based reload**: File writes trigger reload via modification time change (existing 30-second polling cycle).

## Tradeoffs

**Memory vs. complexity (rate limiter expiry)**: Time-based expiry keeps stale limiters for up to 5 minutes, using more memory than immediate eviction. Avoids tracking last-access time on every request (atomic operations hurt performance). Accepted tradeoff: 5-minute window balances memory usage with realistic traffic patterns.

**Security vs. usability (trusted proxy configuration)**: Explicit trusted proxy configuration is more secure but adds deployment complexity. Users behind proxies must configure environment variables. Secure-by-default approach favors security over convenience.

**Performance vs. correctness (constant-time comparison)**: `ConstantTimeCompare` is slower than direct string comparison but prevents timing attacks. Authentication executes once per request, so performance impact is negligible (~1,932 ns/op for valid tokens).

**Configuration simplicity vs. flexibility**: Single environment variable for trusted proxy IPs (comma-separated) is simpler than structured config but less flexible for complex proxy hierarchies. Sufficient for common single-proxy deployments.

**Observability vs. dependencies**: Structured logging uses stdlib only (`log/slog`). Prometheus metrics would add external dependency, operational complexity (scraping, retention), and deployment overhead. Logs provide sufficient forensic data for security events.

**Cleanup latency vs. request blocking (incremental cleanup)**: Incremental cleanup takes longer to reclaim all stale memory (multiple ticks) but prevents blocking requests during cleanup. Full O(n) cleanup reclaims memory faster but blocks rate limit checks.

**Fail-fast vs. graceful degradation (config validation)**: Fail-fast on malformed config exits immediately at startup. More lenient approach would log warnings and continue. Fail-fast prevents silent security misconfigurations but requires exact configuration.

## Error Handling

### Starting the API Server

The API server is controlled by environment variables:

```bash
# .env file
API_ENABLED=true
API_PORT=3001
API_BEARER_TOKEN=your-secure-token-here
API_CORS_ORIGINS=https://example.com,https://app.com
```

### Status Codes
- `400 Bad Request` - Invalid JSON, missing fields, validation failure
- `401 Unauthorized` - Missing or invalid Bearer token
- `403 Forbidden` - Origin not in CORS allowlist
- `413 Payload Too Large` - Request body exceeds 1MB limit
- `429 Too Many Requests` - Rate limit exceeded
- `500 Internal Server Error` - Server-side errors (CORS misconfiguration)
- `503 Service Unavailable` - Request cancelled (context done)

## Security Considerations

### Token Storage
Bearer token read from `API_BEARER_TOKEN` environment variable. Never logged (redacted in middleware). Should be stored securely (e.g., Kubernetes secrets, Vault).

### CORS Configuration
Development: `"*"` wildcard for local testing
Production: Explicit origin allowlist (e.g., `https://admin.example.com`)
Never combine `"*"` with specific origins (ambiguous security policy)

# Get current config
curl -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:3001/api/config

# Get servers only
curl -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:3001/api/config/servers

# Partial update (PATCH)
curl -X PATCH \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"update_interval": 120}' \
  http://localhost:3001/api/config

# Full replacement (PUT)
curl -X PUT \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d @config.json \
  http://localhost:3001/api/config

# Validate without applying
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d @config.json \
  http://localhost:3001/api/config/validate

# Health check (no auth required)
curl http://localhost:3001/health
```

1. Context cancellation signal received
2. HTTP server calls `Shutdown()` with 30-second timeout
3. In-flight requests allowed up to 30 seconds to complete
4. No new requests accepted
5. Handler goroutines complete
6. Server exits cleanly

Handlers respect context cancellation by checking `r.Context().Err()` and returning early if cancelled.
