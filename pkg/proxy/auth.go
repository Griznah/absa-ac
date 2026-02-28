package proxy

import (
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// BasicAuth middleware validates HTTP Basic Auth credentials.
// DL-002: Uses HTTP Basic Auth (RFC 7617) for browser-native authentication
// DL-007: Constant-time password comparison prevents timing attacks
func BasicAuth(username, password string, logger *log.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// DL-008: Health endpoint bypasses auth (matches existing API pattern)
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			auth := r.Header.Get("Authorization")
			if auth == "" {
				// DL-002: 401 response includes WWW-Authenticate header for browser dialog
				w.Header().Set("WWW-Authenticate", `Basic realm="Proxy"`)
				writeProxyError(w, http.StatusUnauthorized, "Missing Authorization header")
				return
			}

			// Validate "Basic <base64(user:pass)>" format
			const prefix = "Basic "
			if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
				w.Header().Set("WWW-Authenticate", `Basic realm="Proxy"`)
				writeProxyError(w, http.StatusUnauthorized, "Invalid Authorization header format")
				return
			}

			// Decode base64 credentials
			decoded, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Basic realm="Proxy"`)
				writeProxyError(w, http.StatusUnauthorized, "Invalid credentials encoding")
				return
			}

			// Parse "user:pass" format
			credentials := string(decoded)
			colonIdx := strings.Index(credentials, ":")
			if colonIdx < 0 {
				w.Header().Set("WWW-Authenticate", `Basic realm="Proxy"`)
				writeProxyError(w, http.StatusUnauthorized, "Invalid credentials format")
				return
			}

			providedUser := credentials[:colonIdx]
			providedPass := credentials[colonIdx+1:]

			// DL-007: Constant-time comparison prevents timing attacks
			userMatch := subtle.ConstantTimeCompare([]byte(providedUser), []byte(username)) == 1
			passMatch := subtle.ConstantTimeCompare([]byte(providedPass), []byte(password)) == 1

			if !userMatch || !passMatch {
				// DL-007: Log auth failures with source IP for audit (R-002 mitigation)
				clientIP := getClientIP(r)
				logger.Printf("WARN: proxy auth failed from %s", clientIP)
				w.Header().Set("WWW-Authenticate", `Basic realm="Proxy"`)
				writeProxyError(w, http.StatusUnauthorized, "Invalid credentials")
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeProxyError writes a JSON error response.
// Uses json.Marshal to ensure proper escaping of special characters.
func writeProxyError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Use json.Marshal for proper JSON escaping (quotes, backslashes, control chars)
	data, _ := json.Marshal(map[string]string{"error": message})
	w.Write(data)
}

// getClientIP extracts client IP from request.
func getClientIP(r *http.Request) string {
	ip := r.RemoteAddr
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		if parts := strings.Split(forwarded, ","); len(parts) > 0 {
			ip = strings.TrimSpace(parts[0])
		}
	} else if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		ip = realIP
	}
	return ip
}
