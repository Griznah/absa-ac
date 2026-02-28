package proxy

import (
	"log"
	"net/http"
	"time"
)

// AccessLog middleware logs all requests at INFO level.
// DL-007: Extracts source IP from X-Forwarded-For (first hop) or X-Real-IP header
func AccessLog(next http.Handler, logger *log.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Wrap response writer to capture status code
		wrapped := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(wrapped, r)

		clientIP := getClientIP(r)

		duration := time.Since(start)
		logger.Printf("INFO: %s %s from %s - %d (%v)",
			r.Method,
			r.URL.Path,
			clientIP,
			wrapped.status,
			duration,
		)
	})
}

// loggingResponseWriter wraps http.ResponseWriter to capture status code.
type loggingResponseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *loggingResponseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}
