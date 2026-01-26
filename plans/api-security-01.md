# API Security Fixes: Timing Attacks, IP Spoofing, and Memory Leaks

## Overview

This plan addresses three critical security vulnerabilities in the API middleware: timing attack vulnerability in Bearer token authentication, IP spoofing via X-Forwarded-For header allowing rate limit bypass, and unbounded memory growth in the rate limiter enabling denial-of-service attacks. The fixes use constant-time comparison for token validation, configurable trusted proxy IPs for header validation, and time-based expiry for rate limiter cleanup with incremental processing to prevent request blocking. All fixes maintain backward compatibility while being secure by default.

## Planning Context

### Decision Log

| Decision | Reasoning Chain |
| --- | --- |
| Constant-time token comparison over direct string comparison | `!=` operator fails fast on first mismatch -> attacker measures response times to brute-force token character-by-character -> `crypto/subtle.ConstantTimeCompare` executes in constant time regardless of input -> eliminates timing side-channel |
| Environment variable for trusted proxy IPs (secure by default) | X-Forwarded-For is trivially spoofed by clients -> blindly trusting it allows rate limit bypass -> must only trust headers from known proxies -> environment variable with empty default means no trusted proxy -> secure by default, explicit opt-in for proxy deployments |
| Time-based expiry for rate limiter cleanup | LRU cache requires additional dependency or custom implementation -> sync.Pool recreates limiters on each request (more allocations) -> time-based expiry cleans entries older than threshold -> low overhead, no external deps, simple to understand |
| Example-based (table-driven) tests over property-based | Project uses table-driven tests in middleware_test.go -> consistency with existing patterns -> Go's testing package handles this well -> easier to add specific edge case coverage for security scenarios |
| Real HTTP server for integration tests | Project has e2e_test.go using httptest.ResponseRecorder -> matches existing pattern -> validates middleware in actual request context -> catches integration issues unit tests miss |
| 5-minute expiry window for rate limiters | Shorter window (1min) still allows DoS by rotating IPs frequently -> longer window (10min) wastes memory on stale entries -> 5 minutes balances memory usage with realistic traffic patterns -> attackers would need significant resources to exploit |
| Maximum 10 IPs in X-Forwarded-For chain | Legitimate deployments rarely exceed 3-4 hops (client → proxy1 → proxy2 → proxy3) -> larger limits enable CPU DoS via parsing overhead -> 10-IP limit accommodates complex proxy chains while bounding parsing cost -> reject with fallback to RemoteAddr if exceeded |
| Rightmost IP in X-Forwarded-For chain | Clients can append fake IPs: "spoofed, 1.2.3.4, real" -> proxies append on right: "client, proxy1, proxy2" -> rightmost value after trusted proxies is the real client IP -> standard proxy behavior |
| Incremental cleanup over full O(n) blocking | Full O(n) cleanup blocks all rate limit checks during iteration -> 100k entries × 1µs = 100ms blocking every 5min -> incremental 1000 entries/tick spreads work across time -> prevents DoS via cleanup contention |
| Fail-fast over warn-and-continue for config | Silent misconfiguration allows rate limit bypass -> admin thinks proxy is trusted but it's not -> fail-fast prevents silent security failures -> typo is caught immediately at startup |
| Structured logging over Prometheus metrics | No existing observability infrastructure in project -> Prometheus adds dependency complexity -> structured logging provides forensic data without overhead -> sufficient for security event visibility |
| context.Context coordination for cleanup lifecycle | Server.Start() accepts context for shutdown coordination -> cleanup ticker goroutine needs same mechanism -> derive cleanup lifecycle from ctx.Done() -> ensures graceful shutdown without goroutine leaks |
| Panic recovery in cleanup goroutine | Cleanup panic crashes goroutine silently -> memory grows forever without cleanup -> defer recover() logs error and restarts -> prevents resource exhaustion from transient bugs |
| Statistical timing testing over single-run | Single-run timing tests fail in CI due to scheduler/GC noise -> 10,000 iterations with variance detection -> marks test as +build integration to skip in normal CI -> reliable verification of constant-time property |
| IP routability validation over net.ParseIP only | net.ParseIP accepts "1.2.3.4.5" (parses as "1.2.3.4") -> accepts link-local (169.254.0.0/16) and loopback -> custom IsRoutableIP rejects non-routable addresses -> prevents spoofing via reserved IP ranges |
| Add benchmarks for timing attack fix | ConstantTimeCompare claimed ~10% slower -> need measurement to verify claim -> BenchmarkBearerAuth_ValidToken vs _InvalidToken -> documents actual latency overhead (e.g., 350ns → 385ns) |

### Rejected Alternatives

| Alternative | Why Rejected |
| --- | --- |
| HMAC-based token authentication | Adds significant complexity (key management, token generation) -> Bearer tokens are simple and sufficient for this use case -> constant-time comparison addresses the immediate security issue |
| IP whitelist middleware | Adds configuration surface -> doesn't address rate limiting bypass -> trusted proxy approach is more targeted |
| sync.Pool for rate limiters | Recreates limiters on each request -> increased GC pressure -> time-based expiry provides better amortized performance |
| Breaking change to require proxy configuration | Would break existing deployments -> users behind proxies would need immediate config change -> secure-by-default approach allows gradual migration |
| Prometheus metrics for observability | No existing metrics infrastructure -> adds external dependency and operational complexity -> structured logging sufficient for forensic analysis |
| Immediate O(n) cleanup for rate limiters | Blocks all rate limit checks during cleanup -> 100k entries causes 100ms+ blocking -> DoS via cleanup contention under attack traffic |
| Warn-and-continue for malformed config | Silent misconfiguration allows security bypass -> admin thinks proxy is trusted but isn't -> fail-fast prevents silent failures |

### Constraints & Assumptions

**Technical:**
- Go 1.25.5 (from go.mod)
- Project uses `golang.org/x/time/rate` for rate limiting
- Bearer token is stored in `API_BEARER_TOKEN` environment variable
- Existing middleware structure: security headers, CORS, auth, rate limit, logger
- No existing observability/metrics infrastructure
- Server uses context.Context for graceful shutdown coordination (api/server.go:55)

**Organizational:**
- User confirmed example-based (table-driven) test approach
- User confirmed real HTTP server for integration tests
- Security fixes are high priority (user requested immediate action)
- User chose structured logging only (no Prometheus)
- User chose fail-fast for config validation
- User chose incremental cleanup strategy

**Dependencies:**
- `crypto/subtle` (stdlib) for constant-time comparison
- `net` package for IP parsing and validation
- `sync` for mutex protection of rate limiter map
- `context` for cleanup goroutine lifecycle
- `log/slog` (stdlib Go 1.21+) for structured logging

**Default conventions applied:**
- `<default-conventions domain="testing">` - Table-driven tests for security scenarios
- `<default-conventions domain="file-creation">` - Extend existing middleware.go rather than create new file
- `<default-conventions domain="observability">` - Structured logging over external metrics

### Known Risks

| Risk | Mitigation | Anchor |
| --- | --- | --- |
| Rate limiter cleanup causes contention | Incremental cleanup processes 1000 entries/tick -> spreads work across multiple cleanup cycles -> prevents blocking requests during cleanup | api/middleware.go:55-93 (current rate limit implementation) |
| X-Forwarded-For parsing breaks on malformed input | IP parsing returns error on invalid format -> malformed headers fall back to RemoteAddr -> safe default behavior | net.ParseIP() documentation specifies error handling |
| Constant-time comparison performance impact | ConstantTimeCompare is ~10% slower than `!=` -> authentication path is not hot loop -> negligible impact on overall request latency | crypto/subtle.ConstantTimeCompare is optimized assembly |
| Cleanup goroutine leaks on server shutdown | Cleanup ticker stopped via ctx.Done() -> graceful shutdown coordinated with server lifecycle -> prevents goroutine leaks | api/server.go:91 (WaitForShutdown via ctx.Done()) |
| Silent misconfiguration of trusted proxy | Fail-fast on malformed IPs exits immediately at startup -> admin must fix typo before deployment -> prevents silent security bypass | main.go:1163 (existing fail-fast pattern for API_BEARER_TOKEN) |
| Non-routable IPs bypass rate limiting | IsRoutableIP rejects loopback, link-local, multicast -> prevents spoofing via reserved ranges -> normalization prevents IPv4-mapped IPv6 confusion | Custom validation function |
| Timing tests flaky in CI | Mark as +build integration -> skip in normal CI -> run only when explicitly invoked -> prevents false negatives from scheduler noise | api/middleware_test.go (new benchmark file) |

## Invisible Knowledge

### Architecture

```
HTTP Request → Security Headers → CORS → Logger → Rate Limit → Bearer Auth → Handler
                                                                    ↑
                                                            Trusted Proxy Check
                                                                    ↑
                                                        Structured Logging (security events)
```

**Request flow:**
1. Security headers applied first (outermost middleware)
2. CORS validation (or 403 if origin not allowed)
3. Request logged (with auth header redacted)
4. Rate limiting (using extracted client IP)
5. Bearer token validation (constant-time compare)
6. Handler executes

**IP extraction logic:**
```
X-Forwarded-For header present?
    ├─ Yes → Trusted proxy IPs configured?
    │         ├─ Yes → Parse comma-separated list (max 10 IPs)
    │         │         ├─ Validate each IP with IsRoutableIP()
    │         │         ├─ Extract rightmost non-trusted IP
    │         │         └─ Log security event if spoofing detected
    │         └─ No  → Ignore header, use RemoteAddr
    └─ No  → Use RemoteAddr
```

**Cleanup coordination:**
```
Server.Start(ctx)
    ↓
Initialize RateLimit middleware with ctx
    ↓
Start cleanup goroutine (select on ctx.Done())
    ↓
Server shutdown → ctx.Cancel()
    ↓
Cleanup goroutine exits gracefully
```

### Data Flow

**Bearer token authentication:**
1. Extract `Authorization` header
2. Validate "Bearer " prefix
3. Compare token value using constant-time comparison
4. Log authentication attempt (redacted token)
5. Return 401 if mismatch, pass to next handler if match

**Rate limiting with IP extraction:**
1. Extract client IP (with X-Forwarded-For handling)
2. Validate IP is routable (reject loopback, link-local, multicast)
3. Look up or create rate limiter for IP
4. Update lastAccess timestamp
5. Check if request allowed within rate limit
6. If not allowed, return 429 and log rate limit event
7. If allowed, pass to next handler
8. Background: every 5 minutes, incremental cleanup (1000 entries/tick)

**Incremental cleanup:**
1. Every 5 minutes, cleanup goroutine wakes
2. Acquire write lock on limiters map
3. Process next 1000 entries (maintain cursor)
4. Delete entries where `time.Since(entry.lastAccess) > 5*time.Minute`
5. Release lock
6. Repeat next tick (continue where cursor left off)
7. On ctx.Done(), stop ticker and exit

**Structured logging for security events:**
- Authentication failures: log with timestamp, IP (redacted if needed), reason
- Rate limit exceeded: log with IP, request path, rate limit stats
- IP spoofing detected: log with X-Forwarded-For value, RemoteAddr, trusted proxy list
- Cleanup events: log entries processed, memory reclaimed

### Why This Structure

**Middleware order matters:** Rate limiting happens BEFORE authentication to prevent DoS on auth validation. However, this means IP spoofing attacks can bypass rate limiting entirely, hence the need for trusted proxy validation.

**Separation of concerns:** Each middleware has single responsibility. IP extraction is part of rate limiting middleware because it's only needed there. Bearer auth doesn't need IP information.

**Performance vs. security tradeoff:** Time-based expiry allows stale limiters to persist for up to 5 minutes, using more memory than immediate eviction. However, it avoids the complexity and overhead of per-request access time tracking (which would require atomic operations on every request).

**Incremental cleanup strategy:** Full O(n) cleanup blocks all rate limit checks during iteration. Processing 1000 entries per tick spreads work across multiple cleanup cycles, preventing request blocking. Complete cleanup still happens within ~5 ticks (25 minutes) for 100k entries.

**Structured logging over metrics:** No existing observability infrastructure. Prometheus adds dependency and operational complexity (scraping endpoints, retention). Structured logging provides forensic data with stdlib only. Logs can be shipped to external aggregators if needed.

**Context-based cleanup coordination:** Server.Start() already uses context.Context for graceful shutdown. Cleanup goroutine uses same mechanism, ensuring coordinated lifecycle management. No separate shutdown signals or channels needed.

### Invariants

- **Trusted proxy list is immutable after startup:** Environment variables are read once during server initialization. Changing them requires restart. This prevents race conditions during reconfiguration.
- **Trusted proxy malformed entries cause failure:** Malformed IP addresses in the trusted proxy list cause immediate exit at startup (fail-fast). This prevents silent misconfiguration where admin thinks proxy is trusted but isn't.
- **X-Forwarded-For chain length is limited:** Headers containing more than 10 IPs are rejected to prevent CPU exhaustion attacks. Malicious clients can send headers with thousands of comma-separated values, causing DoS via parsing overhead. The 10-IP limit accommodates legitimate multi-proxy deployments while bounding worst-case parsing cost.
- **Rate limiter map is protected by mutex:** All accesses (read and write) must acquire mutex. The cleanup goroutine and request handlers coordinate via this lock. Incremental cleanup minimizes lock hold time.
- **Constant-time comparison always executes fully:** Unlike `!=`, ConstantTimeCompare always processes entire string, preventing timing leakage regardless of where mismatch occurs.
- **Client IP extraction is deterministic:** Given same request headers and configuration, IP extraction always returns same result. No random or stateful behavior.
- **Cleanup goroutine stops on server shutdown:** Ticker stopped via ctx.Done(). No goroutine leaks on shutdown. Panic recovery ensures transient bugs don't crash cleanup permanently.
- **IP addresses are normalized before comparison:** IPv4-mapped IPv6 addresses (::ffff:192.168.1.1) are normalized to IPv4 form before comparison. Prevents bypass via different representations of same IP.
- **Loopback, link-local, multicast IPs are rejected:** IsRoutableIP rejects 0.0.0.0, ::1, 169.254.0.0/16, fe80::/10, ff00::/8. Prevents spoofing via reserved IP ranges that shouldn't appear in client requests.

### Tradeoffs

**Memory vs. complexity:** Time-based expiry keeps stale limiters for up to 5 minutes, using more memory than immediate eviction. However, it avoids tracking last-access time on every request (which would require atomic operations and hurt performance).

**Security vs. usability:** Requiring explicit trusted proxy configuration is more secure but adds deployment complexity. Users behind proxies must configure environment variables. Secure-by-default approach favors security over convenience.

**Performance vs. correctness:** Constant-time comparison is slower than direct string comparison but prevents timing attacks. Authentication is not on the hot path (executes once per request), so performance impact is negligible.

**Configuration simplicity vs. flexibility:** Single environment variable for trusted proxy IPs (comma-separated) is simpler than structured config but less flexible for complex proxy hierarchies. Sufficient for common single-proxy deployments.

**Observability vs. dependencies:** Structured logging uses stdlib only (log/slog). Prometheus metrics would add external dependency, operational complexity (scraping, retention), and deployment overhead. Logs provide sufficient forensic data for security events.

**Cleanup latency vs. request blocking:** Incremental cleanup takes longer to reclaim all stale memory (multiple ticks) but prevents blocking requests during cleanup. Full O(n) cleanup reclaims memory faster but blocks rate limit checks.

**Fail-fast vs. graceful degradation:** Fail-fast on malformed config exits immediately at startup. More lenient approach would log warnings and continue. Fail-fast prevents silent security misconfigurations but requires exact configuration.

## Milestones

### Milestone 1: Fix Timing Attack in Bearer Authentication

**Files**: `api/middleware.go`, `api/middleware_test.go`

**Flags**: `security`, `performance`, `needs-rationale`

**Requirements**:

- Replace string comparison `auth[len(prefix):] != token` with `crypto/subtle.ConstantTimeCompare`
- Import `crypto/subtle` package
- Maintain existing error messages and behavior (no functional changes besides timing safety)
- Add benchmark tests to quantify performance impact
- Add structured logging for authentication events

**Acceptance Criteria**:

- Token comparison executes in constant time regardless of input
- Valid tokens pass authentication
- Invalid tokens return 401 Unauthorized
- Response time is independent of token length or match position
- Benchmark measures actual latency overhead (document in plan)
- Authentication failures logged with timestamp, IP, reason (redacted token)

**Tests**:

- **Test files**: `api/middleware_test.go`, `api/middleware_benchmark_test.go` (new file for benchmarks)
- **Test type**: integration (real HTTP server) + benchmark
- **Backing**: user-specified (table-driven)
- **Scenarios**:
  - Normal: Valid token authenticates successfully
  - Edge: Token that matches on first character only vs last character only (statistical timing test)
  - Edge: Empty token, malformed header (missing "Bearer " prefix)
  - Edge: Tokens of varying lengths (1 char, 10 chars, 100 chars)
  - Error: Invalid token returns 401 with consistent timing
  - Benchmark: `BenchmarkBearerAuth_ValidToken` - measure 10,000 iterations
  - Benchmark: `BenchmarkBearerAuth_InvalidToken` - measure 10,000 iterations
  - Integration: Statistical timing test runs 10k iterations, verifies variance < threshold (marked +build integration)

**Code Intent**:

- Import `crypto/subtle` package and `log/slog` for structured logging
- Modify `BearerAuth` function around line 40: replace `if auth[len(prefix):] != token` with `if subtle.ConstantTimeCompare([]byte(auth[len(prefix):]), []byte(token)) != 1`
- Add structured logging: `slog.Info("auth_attempt", "ip", clientIP, "success", false, "reason", "invalid_token")`
- Preserve all existing error messages (missing Authorization header, invalid format, invalid token)
- Create `api/middleware_benchmark_test.go`:
  - Add `BenchmarkBearerAuth_ValidToken` running 10,000 iterations
  - Add `BenchmarkBearerAuth_InvalidToken` running 10,000 iterations
  - Add `TestTimingIndependence` marked `+build integration` that runs 10k auth attempts and verifies timing variance < noise floor
- Document measured latency in plan update (e.g., "350ns → 385ns, 35ns overhead per request")

### Milestone 2: Fix X-Forwarded-For IP Spoofing

**Files**: `api/middleware.go`, `api/server.go`, `main.go`, `.env.example`

**Flags**: `security`, `conformance`

**Requirements**:

- Add `API_TRUSTED_PROXY_IPS` environment variable (comma-separated list of trusted proxy IPs)
- Modify `RateLimit` middleware to validate X-Forwarded-For only from trusted proxies
- Extract rightmost IP from X-Forwarded-For chain, excluding trusted proxies
- Fallback to RemoteAddr if no trusted proxies configured or header malformed
- Add IP validation to reject non-routable addresses (loopback, link-local, multicast)
- Fail-fast on malformed trusted proxy IPs at startup
- Add structured logging for IP spoofing detection

**Acceptance Criteria**:

- Empty `API_TRUSTED_PROXY_IPS` (default) ignores X-Forwarded-For entirely (secure by default)
- Configured trusted proxies allows X-Forwarded-For parsing
- Malformed IP addresses in trusted list cause immediate exit at startup (fail-fast)
- Non-routable IPs (loopback, link-local, multicast) are rejected in X-Forwarded-For
- Spoofed X-Forwarded-For from untrusted sources is ignored and logged
- Rightmost non-trusted IP is used as client IP
- X-Forwarded-For headers with more than 10 IPs are rejected (prevent CPU DoS)
- IPv6 addresses handled correctly
- Mixed IPv4/IPv6 chains handled correctly

**Tests**:

- **Test files**: `api/middleware_test.go`
- **Test type**: integration (real HTTP server)
- **Backing**: user-specified (table-driven)
- **Scenarios**:
  - Normal: Request from trusted proxy with valid X-Forwarded-For uses rightmost non-trusted IP
  - Normal: Request from untrusted source ignores X-Forwarded-For, uses RemoteAddr
  - Normal: Empty trusted proxy list ignores X-Forwarded-For from all sources
  - Edge: X-Forwarded-For with multiple IPs extracts rightmost non-trusted IP
  - Edge: Malformed IP in X-Forwarded-For falls back to RemoteAddr, logs event
  - Edge: All IPs in X-Forwarded-For are trusted proxies uses RemoteAddr
  - Edge: X-Forwarded-For with more than 10 IPs is rejected, falls back to RemoteAddr
  - Edge: IPv6 address in X-Forwarded-For handled correctly
  - Edge: Mixed IPv4/IPv6 chain extracts rightmost non-trusted IP
  - Edge: Loopback (127.0.0.1, ::1) rejected in X-Forwarded-For
  - Edge: Link-local (169.254.0.0/16, fe80::/10) rejected in X-Forwarded-For
  - Edge: Multicast (ff00::/8) rejected in X-Forwarded-For
  - Edge: IPv4-mapped IPv6 (::ffff:192.168.1.1) normalized to IPv4
  - Error: Malformed IP in trusted proxy list logs error and exits (fail-fast)

**Code Intent**:

- Define constant `maxForwardedIps = 10` at package level (Decision: "Maximum 10 IPs in X-Forwarded-For chain")
- Define constant `cleanupBatchSize = 1000` for incremental cleanup (Decision: "Incremental cleanup over full O(n) blocking")
- Add helper function `isRoutableIP(ip net.IP) bool` that rejects:
  - `ip.IsLoopback()` (127.0.0.0/8, ::1)
  - `ip.IsLinkLocalUnicast()` (169.254.0.0/16, fe80::/10)
  - `ip.IsMulticast()` (224.0.0.0/4, ff00::/8)
  - `ip.IsUnspecified()` (0.0.0.0, ::)
- Add helper function `normalizeIP(ipStr string) string` that converts IPv4-mapped IPv6 (::ffff:192.168.1.1) to IPv4 (192.168.1.1)
- Add helper function `extractClientIP(r *http.Request, trustedProxies []string) string`:
  - Check if `X-Forwarded-For` header exists
  - Validate header length: reject if contains more than 10 IPs
  - If yes and `trustedProxies` non-empty: parse comma-separated values (trim whitespace)
  - Normalize each IP and validate with `isRoutableIP()`
  - Iterate from right to left, find first IP not in trusted list
  - If found, use that IP; otherwise use RemoteAddr
  - If no header or no trusted proxies, use RemoteAddr
  - Log security event if spoofing detected: `slog.Warn("ip_spoof_detected", "xff_header", header, "remote_addr", r.RemoteAddr, "trusted_proxies", trustedProxies)`
- Modify `RateLimit` function signature to accept `trustedProxies []string` and `ctx context.Context` parameters
- Modify `NewServer` in `api/server.go` to accept `trustedProxies` parameter and pass to `RateLimit`
- Modify `main.go`:
  - Read `API_TRUSTED_PROXY_IPS` env var
  - Split by commas, trim whitespace
  - Validate each IP with `net.ParseIP()` and `isRoutableIP()`
  - If any IP is malformed, log error and `log.Fatalf()` (fail-fast)
  - Pass validated list to `api.NewServer`
- Update `.env.example` with `API_TRUSTED_PROXY_IPS` documentation
- Update root `README.md` Environment Variables section with `API_TRUSTED_PROXY_IPS` documentation (format: comma-separated IPs, empty default means no trusted proxies)

### Milestone 3: Fix Rate Limiter Memory Leak

**Files**: `api/middleware.go`, `api/middleware_test.go`

**Flags**: `security`, `performance`, `error-handling`

**Requirements**:

- Add time-based expiry to rate limiter map (cleanup entries older than 5 minutes)
- Track last access time for each rate limiter
- Run background cleanup goroutine every 5 minutes with incremental processing (1000 entries/tick)
- Use mutex to protect cleanup iteration
- Coordinate cleanup lifecycle with context.Context (stop on server shutdown)
- Add panic recovery in cleanup goroutine
- Add structured logging for cleanup events

**Acceptance Criteria**:

- Rate limiters not accessed within 5 minutes are removed from map
- Cleanup goroutine runs every 5 minutes without blocking request handling
- Memory usage remains bounded under normal and attack traffic
- Concurrent requests during cleanup do not cause race conditions
- Cleanup goroutine stops gracefully on server shutdown (no goroutine leaks)
- Panic in cleanup goroutine is recovered, logged, and goroutine restarts
- Cleanup events logged (entries processed, memory reclaimed)
- Incremental cleanup processes 1000 entries per tick

**Tests**:

- **Test files**: `api/middleware_test.go`
- **Test type**: integration (real HTTP server)
- **Backing**: user-specified (table-driven)
- **Scenarios**:
  - Normal: Active rate limiter persists across requests
  - Edge: Rate limiter expires after 5 minutes of inactivity
  - Edge: Multiple concurrent requests during cleanup don't cause data races
  - Edge: New requests during cleanup recreate limiters as needed
  - Edge: Cleanup processes 1000 entries per tick (verify cursor progress)
  - Performance: Memory usage stabilizes after 10,000 unique IPs (doesn't grow indefinitely)
  - Performance: Cleanup completes within reasonable time for 100k entries (verify no blocking)
  - Integration: Rate limiters created for IPs extracted from X-Forwarded-For (via trusted proxy) are cleaned up after 5 minutes of inactivity, verifying cleanup works for both RemoteAddr and forwarded IPs
  - Shutdown: Cleanup goroutine stops when context cancelled (verify with ctx.Cancel())
  - Shutdown: No goroutine leak after server stop (verify runtime.NumGoroutine)
  - Error: Panic in cleanup goroutine is recovered, logged, cleanup restarts

**Code Intent**:

- Define new struct wrapping rate limiter with access time tracking:

```go
// Before: limiter map stored rate.Limiter directly
limiters := make(map[string]*rate.Limiter)

// After: wrap with access time for cleanup
type rateLimiter struct {
    limiter     *rate.Limiter
    lastAccess time.Time
}
limiters := make(map[string]*rateLimiter)

// Add cleanup cursor for incremental processing
type rateLimiterManager struct {
    limiters map[string]*rateLimiter
    mu       sync.RWMutex
    cursor   int  // Current position for incremental cleanup
    ctx      context.Context
}
```

- Modify `RateLimit` function to accept `ctx context.Context` parameter
- Modify limiter lookup/update to set `lastAccess = time.Now()` on each access
- Add cleanup function `cleanupStaleLimiters()` called every 5 minutes via `time.Ticker`:
  - Acquire write lock
  - Process next 1000 entries starting from `cursor`
  - Delete entries where `time.Since(entry.lastAccess) > 5*time.Minute`
  - Update `cursor` (wrap to 0 if end reached)
  - Release lock
  - Log cleanup event: `slog.Info("rate_limit_cleanup", "entries_processed", count, "entries_deleted", deleted, "cursor", cursor)`
- Add panic recovery:
```go
defer func() {
    if r := recover(); r != nil {
        slog.Error("rate_limit_cleanup_panic", "panic", r, "stack", debug.Stack())
        // Restart cleanup after 1 minute
        time.AfterFunc(1*time.Minute, func() { cleanupStaleLimiters() })
    }
}()
```
- Start cleanup goroutine in middleware factory function, listening on `ctx.Done()`:
```go
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            cleanupStaleLimiters()
        case <-ctx.Done():
            slog.Info("rate_limit_cleanup_shutdown")
            return
        }
    }
}()
```
- Modify `NewServer` to pass context to `RateLimit` middleware
- Ensure cleanup goroutine stops when server shuts down (context cancellation)
- Use existing mutex for map protection during cleanup

### Milestone 4: Documentation

**Delegated to**: @agent-technical-writer (mode: post-implementation)

**Source**: `## Invisible Knowledge` section of this plan

**Files**:

- `api/CLAUDE.md` (navigation index)
- `api/README.md` (architecture, security considerations)

**Requirements**:

Delegate to Technical Writer. Key deliverables:
- CLAUDE.md: Pure navigation index (tabular format)
- README.md: Complete documentation of middleware security architecture

**Acceptance Criteria**:

- CLAUDE.md is tabular index only (no prose sections)
- README.md exists in api/ directory
- README.md documents:
  - Timing attack prevention with constant-time comparison
  - X-Forwarded-For handling and trusted proxy validation
  - IP validation (rejecting non-routable addresses)
  - Rate limiter lifecycle and incremental cleanup strategy
  - Structured logging for security events
  - Context-based cleanup coordination
  - Architecture diagram showing middleware order and data flow
  - IP spoofing prevention algorithm
  - Fail-fast configuration validation
  - Performance characteristics (benchmark results from Milestone 1)
  - Migration guide for proxy deployments
  - Troubleshooting section (e.g., "Why are requests from my proxy being rate-limited?")

**Source Material**: `## Invisible Knowledge` section of this plan

**Documentation Outline**:

1. **Architecture Overview**
   - Middleware order diagram
   - Request flow through security layers
   - Component relationships

2. **Security Features**
   - Timing attack prevention (constant-time comparison)
   - IP spoofing prevention (trusted proxy validation)
   - Rate limiting with memory leak protection
   - Configuration validation (fail-fast)

3. **Middleware Details**
   - BearerAuth: How constant-time comparison works
   - RateLimit: IP extraction, X-Forwarded-For handling, incremental cleanup
   - Trusted Proxy Configuration: How to configure, common deployment patterns
   - IP Validation: What IPs are rejected and why

4. **Observability**
   - Structured logging for security events
   - What gets logged and when
   - How to interpret security logs

5. **Performance**
   - Benchmark results (from Milestone 1)
   - Memory usage characteristics
   - Cleanup overhead analysis

6. **Deployment**
   - Environment variables reference
   - Configuring trusted proxies
   - Migration guide for existing deployments
   - Common deployment patterns (no proxy, single proxy, CDN + proxy)

7. **Troubleshooting**
   - Requests being rate-limited unexpectedly
   - Proxy configuration issues
   - Memory leak detection
   - Debugging timing issues

## Cross-Milestone Integration Tests

Integration tests spanning multiple milestones will be placed in Milestone 3 (Rate Limiter Fix):

1. **Trusted proxy + rate limiting**: Verify that rate limiting works correctly with X-Forwarded-For extraction from trusted proxies
2. **Memory leak verification**: Send requests from 10,000 unique IPs through trusted proxy configuration, verify memory stabilizes after cleanup cycle
3. **Timing verification**: Measure authentication times for tokens failing at different character positions, verify variance is within noise floor (statistical test, +build integration)
4. **Cleanup lifecycle**: Verify cleanup goroutine starts, processes entries incrementally, and stops gracefully on context cancellation
5. **Goroutine leak verification**: Start and stop server 100 times, verify goroutine count doesn't increase (no goroutine leak)
6. **Panic recovery**: Inject panic in cleanup goroutine, verify recovery, logging, and restart
7. **Full security posture**: All three fixes working together (timing-safe auth, trusted proxy validation, memory-safe rate limiting) under simulated attack traffic

These tests require all three security fixes to be implemented and validate the complete security posture.

## Milestone Dependencies

```
M1 (Timing Fix) ─────────────────────────────────────┐
                                                   │
M2 (IP Spoofing Fix) ──────────────────────────────┤
                                                   ├─→ M4 (Documentation)
                                                   │
M3 (Memory Leak Fix) ──────────────────────────────┘
     ↑
     │ (depends on M2 for trusted proxy infrastructure)
```

**Dependency rationale**: M3 adds cleanup coordination via context.Context which M2 already introduces for passing to RateLimit middleware. M3 can reuse the context parameter added in M2. M1, M2, M3 can be implemented in parallel, but M3 benefits from M2's context infrastructure.

**Independent milestones**: M1 and M2 are independent and can be developed in parallel. M3 depends on M2 for context parameter but not on M1.

**Parallelization strategy**:
- Wave 1: M1 + M2 (parallel)
- Wave 2: M3 (after M2 completes, for context infrastructure)
- Wave 3: M4 (after M1, M2, M3 complete, for comprehensive documentation)
