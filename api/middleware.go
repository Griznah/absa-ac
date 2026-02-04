package api

import (
	"crypto/subtle"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// BearerAuth validates Bearer token authentication
// Returns 401 Unauthorized if token is missing or invalid
// Follows RFC 6750 OAuth2 Bearer Token specification
// Note: /health endpoint bypasses auth (public monitoring endpoint)
// but rate limiting still applies to prevent DoS
func BearerAuth(token string) func(http.Handler) http.Handler {
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

			// Use timing-safe comparison to prevent timing attacks
			if subtle.ConstantTimeCompare([]byte(auth[len(prefix):]), []byte(token)) != 1 {
				WriteError(w, http.StatusUnauthorized, "Invalid Bearer token",
					"The provided token is not valid")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimit implements token bucket rate limiting per client IP
// Prevents DoS attacks by limiting request frequency
// Applies to ALL endpoints including /health (prevents health check DoS)
// requestsPerSecond: steady-state rate limit
// burstSize: maximum burst size for bursty traffic
func RateLimit(requestsPerSecond int, burstSize int) func(http.Handler) http.Handler {
	// Track limiters per IP address
	type limiterEntry struct {
		limiter     *rate.Limiter
		lastAccess  time.Time
	}

	limiters := make(map[string]*limiterEntry)
	var mu sync.Mutex

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract client IP (strip port to make limiter per-IP not per-connection)
			clientIP := r.RemoteAddr
			// Parse host:port format to extract just the IP address
			if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
				clientIP = host
			}

			if forwardedFor := r.Header.Get("X-Forwarded-For"); forwardedFor != "" {
				// Trust rightmost IP (last proxy) not leftmost (can be spoofed)
				// X-Forwarded-For format: "client-ip, proxy1-ip, proxy2-ip"
				ips := strings.Split(forwardedFor, ",")
				clientIP = strings.TrimSpace(ips[len(ips)-1])
			}

			// Get or create limiter for this IP
			mu.Lock()
			entry, exists := limiters[clientIP]
			if !exists {
				entry = &limiterEntry{
					limiter:    rate.NewLimiter(rate.Limit(requestsPerSecond), burstSize),
					lastAccess: time.Now(),
				}
				limiters[clientIP] = entry
			} else {
				// Update last access time
				entry.lastAccess = time.Now()
			}

			// Clean up stale limiters (inline cleanup on each request)
			// This prevents unbounded memory growth without a background goroutine
			now := time.Now()
			for ip, limiterEntry := range limiters {
				if now.Sub(limiterEntry.lastAccess) > 5*time.Minute {
					delete(limiters, ip)
				}
			}
			mu.Unlock()

			// Check rate limit
			if !entry.limiter.Allow() {
				WriteError(w, http.StatusTooManyRequests, "Rate limit exceeded",
					fmt.Sprintf("More than %d requests per second allowed", requestsPerSecond))
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

			// Redact Authorization header for logging
			auth := r.Header.Get("Authorization")
			if auth != "" {
				r.Header.Set("Authorization", "Bearer <redacted>")
			}

			// Wrap response writer to capture status code
			wrapped := &responseWriter{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(wrapped, r)

			// Log request
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
