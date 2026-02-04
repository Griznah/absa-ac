package api

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	maxForwardedIps      = 10
	cleanupBatchSize     = 1000
	cleanupInterval      = 5 * time.Minute
	rateLimiterExpiry    = 5 * time.Minute
	cleanupRestartDelay  = 1 * time.Minute
)

// rateLimiter wraps a rate.Limiter with last access time for cleanup
type rateLimiter struct {
	limiter     *rate.Limiter
	lastAccess time.Time
}

// rateLimiterManager manages rate limiters with incremental cleanup
type rateLimiterManager struct {
	limiters map[string]*rateLimiter
	mu       sync.RWMutex
	cursor   int
	ctx      context.Context
}

// isRoutableIP checks if an IP address is routable (publicly addressable)
// Returns false for loopback, link-local, multicast, and unspecified addresses
// Prevents spoofing via reserved IP ranges that shouldn't appear in client requests
func isRoutableIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return false
	}
	if ip.IsLinkLocalUnicast() {
		return false
	}
	if ip.IsMulticast() {
		return false
	}
	if ip.IsUnspecified() {
		return false
	}
	return true
}

// normalizeIP converts IPv4-mapped IPv6 addresses to IPv4 format
// For example: ::ffff:192.168.1.1 -> 192.168.1.1
// This prevents bypass via different representations of the same IP
func normalizeIP(ipStr string) string {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ipStr
	}

	// Check if it's an IPv4-mapped IPv6 address
	if ip.To4() != nil {
		return ip.To4().String()
	}
	return ip.String()
}

// extractClientIP extracts the real client IP from X-Forwarded-For header
// only if the request comes from a trusted proxy. Otherwise uses RemoteAddr.
// Returns the rightmost non-trusted IP from the X-Forwarded-For chain.
// Falls back to RemoteAddr if:
// - No X-Forwarded-For header present
// - No trusted proxies configured
// - Request doesn't come from a trusted proxy
// - Header is malformed or contains too many IPs
// - All IPs in the chain are trusted proxies
func extractClientIP(r *http.Request, trustedProxies []string) string {
	forwardedFor := r.Header.Get("X-Forwarded-For")

	// If no X-Forwarded-For header or no trusted proxies configured, use RemoteAddr
	if forwardedFor == "" || len(trustedProxies) == 0 {
		return r.RemoteAddr
	}

	// Extract IP from RemoteAddr (strip port if present)
	remoteIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remoteIP = host
	}

	// Normalize remote IP (handle IPv4-mapped IPv6)
	normalizedRemoteIP := normalizeIP(remoteIP)

	// Build trusted proxy set for O(1) lookup
	trustedSet := make(map[string]bool, len(trustedProxies))
	for _, proxy := range trustedProxies {
		trustedSet[proxy] = true
	}

	// Check if request comes from a trusted proxy
	// If not, ignore X-Forwarded-For entirely (could be spoofed)
	if !trustedSet[normalizedRemoteIP] {
		slog.Warn("ip_spoof_detected",
			"reason", "xff_from_untrusted_source",
			"xff_header", forwardedFor,
			"remote_addr", r.RemoteAddr,
			"remote_ip", normalizedRemoteIP,
			"trusted_proxies", trustedProxies,
		)
		return r.RemoteAddr
	}

	// Request is from a trusted proxy, parse X-Forwarded-For
	parts := strings.Split(forwardedFor, ",")
	if len(parts) > maxForwardedIps {
		slog.Warn("ip_spoof_detected",
			"reason", "too_many_ips_in_xff",
			"xff_count", len(parts),
			"xff_header", forwardedFor,
			"remote_addr", r.RemoteAddr,
			"max_allowed", maxForwardedIps,
		)
		return r.RemoteAddr
	}

	// Iterate from right to left (last proxy appended is rightmost)
	// Find the first IP that is not a trusted proxy
	for i := len(parts) - 1; i >= 0; i-- {
		ipStr := strings.TrimSpace(parts[i])

		// Normalize IP (handle IPv4-mapped IPv6)
		normalizedIP := normalizeIP(ipStr)

		// Validate IP is routable (reject loopback, link-local, multicast)
		ip := net.ParseIP(normalizedIP)
		if ip == nil || !isRoutableIP(ip) {
			slog.Warn("ip_spoof_detected",
				"reason", "invalid_or_non_routable_ip",
				"xff_header", forwardedFor,
				"remote_addr", r.RemoteAddr,
				"invalid_ip", ipStr,
				"normalized_ip", normalizedIP,
			)
			return r.RemoteAddr
		}

		// If this IP is not in the trusted proxy list, it's the client IP
		if !trustedSet[normalizedIP] {
			// Found the real client IP (rightmost non-trusted IP)
			return normalizedIP
		}

		// This IP is a trusted proxy, continue checking left
	}

	// All IPs in the chain are trusted proxies, use RemoteAddr
	slog.Warn("ip_spoof_detected",
		"reason", "all_ips_are_trusted_proxies",
		"xff_header", forwardedFor,
		"remote_addr", r.RemoteAddr,
		"trusted_proxies", trustedProxies,
	)
	return r.RemoteAddr
}

// BearerAuth validates Bearer token authentication
// Returns 401 Unauthorized if token is missing or invalid
// Follows RFC 6750 OAuth2 Bearer Token specification
func BearerAuth(token string, trustedProxies []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Health check bypasses auth
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if auth == "" {
				WriteError(w, http.StatusUnauthorized, "Missing Authorization header",
					"Request requires Bearer token authentication")
				return
			}

			// Validate "Bearer <token>" format
			const prefix = "Bearer "
			if len(auth) < len(prefix) || auth[:len(prefix)] != prefix {
				WriteError(w, http.StatusUnauthorized, "Invalid Authorization header format",
					"Expected format: Authorization: Bearer <token>")
				return
			}

			// Use constant-time comparison to prevent timing attacks
			// subtle.ConstantTimeCompare returns 1 if equal, 0 otherwise
			if subtle.ConstantTimeCompare([]byte(auth[len(prefix):]), []byte(token)) != 1 {
				// Extract client IP for logging (with trusted proxy validation)
				clientIP := extractClientIP(r, trustedProxies)

				// Log authentication failure with structured logging (token redacted)
				slog.Info("auth_attempt",
					"success", false,
					"reason", "invalid_token",
					"ip", clientIP,
					"token", "<redacted>",
				)

				WriteError(w, http.StatusUnauthorized, "Invalid Bearer token",
					"The provided token is not valid")
				return
			}

			// Extract client IP for successful auth logging (with trusted proxy validation)
			clientIP := extractClientIP(r, trustedProxies)

			// Log successful authentication
			slog.Info("auth_attempt",
				"success", true,
				"ip", clientIP,
			)

			next.ServeHTTP(w, r)
		})
	}
}

// cleanupStaleLimiters incrementally removes stale rate limiters
// Processes cleanupBatchSize entries per call, maintaining cursor position
// Acquires write lock to safely delete stale entries during iteration
func (rm *rateLimiterManager) cleanupStaleLimiters() {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("rate_limit_cleanup_panic",
				"panic", r,
				"stack", string(debug.Stack()),
			)
			// Restart cleanup after delay
			time.AfterFunc(cleanupRestartDelay, func() {
				rm.cleanupStaleLimiters()
			})
		}
	}()

	rm.mu.Lock()
	defer rm.mu.Unlock()

	if len(rm.limiters) == 0 {
		return
	}

	processed := 0
	deleted := 0
	keys := make([]string, 0, cleanupBatchSize)

	// Collect keys to process in this batch
	for key := range rm.limiters {
		if processed >= cleanupBatchSize {
			break
		}
		keys = append(keys, key)
		processed++
	}

	// Check and delete stale entries
	now := time.Now()
	for _, key := range keys {
		rl := rm.limiters[key]
		if now.Sub(rl.lastAccess) > rateLimiterExpiry {
			delete(rm.limiters, key)
			deleted++
		}
	}

	// Track total processed count (cursor represents batches processed)
	rm.cursor += processed

	slog.Info("rate_limit_cleanup",
		"entries_processed", processed,
		"entries_deleted", deleted,
		"total_processed", rm.cursor,
		"total_entries", len(rm.limiters),
	)
}

// startCleanupGoroutine launches background cleanup for stale rate limiters
// Stops gracefully when context is cancelled
func (rm *rateLimiterManager) startCleanupGoroutine() {
	go func() {
		ticker := time.NewTicker(cleanupInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				rm.cleanupStaleLimiters()
			case <-rm.ctx.Done():
				slog.Info("rate_limit_cleanup_shutdown")
				return
			}
		}
	}()
}

// RateLimit implements token bucket rate limiting per client IP
// Prevents DoS attacks by limiting request frequency
// Applies to ALL endpoints including /health (prevents health check DoS)
// requestsPerSecond: steady-state rate limit
// burstSize: maximum burst size for bursty traffic
// trustedProxies: list of trusted proxy IPs for X-Forwarded-For validation
// ctx: context for cleanup goroutine lifecycle
func RateLimit(requestsPerSecond int, burstSize int, trustedProxies []string, ctx context.Context) func(http.Handler) http.Handler {
	rm := &rateLimiterManager{
		limiters: make(map[string]*rateLimiter),
		ctx:      ctx,
	}
	rm.startCleanupGoroutine()

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract client IP (strip port to make limiter per-IP not per-connection)
			clientIP := r.RemoteAddr
			// Parse host:port format to extract just the IP address
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				clientIP = host
			}

			// Extract client IP with trusted proxy validation
			clientIP := extractClientIP(r, trustedProxies)

			// Get or create limiter for this IP
			rm.mu.RLock()
			rl, exists := rm.limiters[clientIP]
			rm.mu.RUnlock()

			if !exists {
				rm.mu.Lock()
				// Double-check after acquiring write lock
				rl, exists = rm.limiters[clientIP]
				if !exists {
					rl = &rateLimiter{
						limiter:     rate.NewLimiter(rate.Limit(requestsPerSecond), burstSize),
						lastAccess: time.Now(),
					}
					rm.limiters[clientIP] = rl
				}
				rm.mu.Unlock()
			} else {
				// Update last access time
				rm.mu.Lock()
				rl.lastAccess = time.Now()
				rm.mu.Unlock()
			}

			// Check rate limit
			if !rl.limiter.Allow() {
				WriteError(w, http.StatusTooManyRequests, "Rate limit exceeded",
					fmt.Sprintf("Maximum of %d requests per second allowed", requestsPerSecond))
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// Logger logs all HTTP requests with method, path, status, and duration
// Redacts sensitive information like Bearer tokens from logs
func Logger(logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			// Wrap response writer to capture status code
			wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			// Log request (method, path, status, duration - no headers logged)
			duration := time.Since(start)
			logger.Printf("%s %s - %d (%v)",
				r.Method,
				r.URL.Path,
				wrapped.status,
				duration,
			)
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture status code
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

// CORS implements Cross-Origin Resource Sharing middleware
// allowedOrigins is a list of allowed origin URLs (e.g., "https://example.com")
// Empty list means no CORS headers are set (same-origin only)
// "*" allows all origins (use with caution)
func CORS(allowedOrigins []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			// If no Origin header, skip CORS (not a cross-origin request)
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Check if origin is allowed
			// Validate allowlist: reject "*" mixed with other origins (security risk)
			hasWildcard := false
			for _, origin := range allowedOrigins {
				if origin == "*" {
					hasWildcard = true
					break
				}
			}
			if hasWildcard && len(allowedOrigins) > 1 {
				WriteError(w, http.StatusInternalServerError, "CORS configuration error",
					"Wildcard '*' cannot be combined with specific origins")
				return
			}

			allowed := false
			for _, allowedOrigin := range allowedOrigins {
				if allowedOrigin == "*" || allowedOrigin == origin {
					allowed = true
					break
				}
			}

			if !allowed {
				// Origin not in allowlist - return 403 Forbidden
				WriteError(w, http.StatusForbidden, "Origin not allowed",
					fmt.Sprintf("Origin '%s' is not in the allowed origins list", origin))
				return
			}

			// Set CORS headers
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			// Handle preflight requests
			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// SecurityHeaders adds security-related HTTP headers to all responses
// Helps prevent XSS, clickjacking, and other security vulnerabilities
func SecurityHeaders() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Prevent MIME type sniffing
			w.Header().Set("X-Content-Type-Options", "nosniff")

			// Prevent clickjacking
			w.Header().Set("X-Frame-Options", "DENY")

			// XSS protection (legacy, but still useful for older browsers)
			w.Header().Set("X-XSS-Protection", "1; mode=block")

			// Content Security Policy (restricts sources of content)
			w.Header().Set("Content-Security-Policy", "default-src 'self'")

			// Referrer Policy
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")

			next.ServeHTTP(w, r)
		})
	}
}
