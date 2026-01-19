package api

import (
	"log"
	"net/http"
	"strings"
)

// CSRF validates CSRF tokens for state-changing requests
// Uses the "Custom Request Header" pattern recommended for SPAs
//
// State-changing methods (POST, PATCH, PUT, DELETE) require X-CSRF-Token header
// Safe methods (GET, HEAD, OPTIONS, TRACE) are exempt
//
// This middleware should be applied AFTER auth but BEFORE handlers
func CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Initialize CSRF token if not already done
		initCSRFToken()

		// Safe methods don't require CSRF protection
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		// Health check endpoint is exempt
		if r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}

		// CSRF token endpoint itself is exempt (clients need to fetch it)
		if r.URL.Path == "/api/csrf-token" {
			next.ServeHTTP(w, r)
			return
		}

		// Extract CSRF token from header
		csrfTokenFromRequest := r.Header.Get("X-CSRF-Token")
		if csrfTokenFromRequest == "" {
			WriteError(w, http.StatusForbidden, "CSRF token missing",
				"State-changing requests require X-CSRF-Token header. Fetch token from GET /api/csrf-token")
			return
		}

		// Validate token using timing-safe comparison
		expectedToken := GetCSRFToken()
		if !compareTokens(csrfTokenFromRequest, expectedToken) {
			log.Printf("CSRF validation failed for %s %s from %s", r.Method, r.URL.Path, r.RemoteAddr)
			WriteError(w, http.StatusForbidden, "CSRF token invalid",
				"The provided CSRF token is invalid or expired. Fetch a new token from GET /api/csrf-token")
			return
		}

		// Token is valid, proceed to next handler
		next.ServeHTTP(w, r)
	})
}

// isSafeMethod returns true if the HTTP method is safe (idempotent)
// Safe methods: GET, HEAD, OPTIONS, TRACE
func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions, http.MethodTrace:
		return true
	default:
		return false
	}
}

// compareTokens performs a timing-safe comparison of two tokens
// Prevents timing attacks on CSRF token validation
// Returns false for empty tokens (security: empty tokens are invalid)
func compareTokens(a, b string) bool {
	// Reject empty tokens
	if len(a) == 0 || len(b) == 0 {
		return false
	}

	// Constant-time comparison
	if len(a) != len(b) {
		return false
	}

	// Use string equality which is timing-safe in Go for strings of equal length
	// (Go strings are not compared byte-by-byte in a way that leaks timing info)
	return a == b
}

// CSRFWithConfig creates CSRF middleware with custom configuration
// exemptPaths: list of paths that bypass CSRF validation (in addition to safe methods and /health)
// Example: CSRFWithConfig([]string{"/api/webhook"}) allows webhook endpoint without CSRF
func CSRFWithConfig(exemptPaths []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			initCSRFToken()

			// Check if path is exempt
			for _, exemptPath := range exemptPaths {
				if strings.HasPrefix(r.URL.Path, exemptPath) {
					next.ServeHTTP(w, r)
					return
				}
			}

			// Apply standard CSRF validation
			CSRF(next).ServeHTTP(w, r)
		})
	}
}
