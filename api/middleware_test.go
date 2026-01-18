package api

import (
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBearerAuth(t *testing.T) {
	tests := []struct {
		name       string
		token      string
		authHeader string
		wantStatus int
	}{
		{
			name:       "Normal: Valid Bearer token returns 200",
			token:      "secret-token",
			authHeader: "Bearer secret-token",
			wantStatus: http.StatusOK,
		},
		{
			name:       "Edge: Missing Authorization header returns 401",
			token:      "secret-token",
			authHeader: "",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "Edge: Malformed Bearer token returns 401",
			token:      "secret-token",
			authHeader: "secret-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "Edge: Invalid Bearer token returns 401",
			token:      "secret-token",
			authHeader: "Bearer wrong-token",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "Normal: Health check bypasses auth",
			token:      "secret-token",
			authHeader: "",
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test handler that returns 200
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			// Wrap with auth middleware
			middleware := BearerAuth(tt.token)

			// Create request
			req := httptest.NewRequest("GET", "/test", nil)
			if tt.authHeader != "" {
				req.Header.Set("Authorization", tt.authHeader)
			}

			// For health check test, use /health path
			if strings.Contains(tt.name, "Health check") {
				req = httptest.NewRequest("GET", "/health", nil)
			}

			// Record response
			rec := httptest.NewRecorder()

			// Call middleware
			middleware(handler).ServeHTTP(rec, req)

			// Check status
			if rec.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestRateLimit(t *testing.T) {
	tests := []struct {
		name           string
		requestsPerSec int
		burstSize      int
		requestCount   int
		wantStatus     int
	}{
		{
			name:           "Normal: Under rate limit returns 200",
			requestsPerSec: 10,
			burstSize:      5,
			requestCount:   3,
			wantStatus:     http.StatusOK,
		},
		{
			name:           "Edge: Rate limit exhausted returns 429",
			requestsPerSec: 1,
			burstSize:      2,
			requestCount:   5,
			wantStatus:     http.StatusTooManyRequests,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			middleware := RateLimit(tt.requestsPerSec, tt.burstSize)
			wrapped := middleware(handler)

			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = "127.0.0.1:12345"

			lastStatus := http.StatusOK
			for i := 0; i < tt.requestCount; i++ {
				rec := httptest.NewRecorder()
				wrapped.ServeHTTP(rec, req)
				lastStatus = rec.Code

				// Small delay between requests
				time.Sleep(10 * time.Millisecond)
			}

			// Final status should match expected
			if tt.wantStatus == http.StatusTooManyRequests {
				if lastStatus != http.StatusTooManyRequests {
					t.Errorf("Final status = %d, want %d", lastStatus, tt.wantStatus)
				}
			} else {
				if lastStatus != http.StatusOK {
					t.Errorf("Status = %d, want %d", lastStatus, tt.wantStatus)
				}
			}
		})
	}
}

func TestRateLimit_RecoveryAfterWait(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := RateLimit(2, 2)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "127.0.0.1:12346"

	// Exhaust burst
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
	}

	// Wait for rate limit to recover
	time.Sleep(1 * time.Second)

	// Should succeed again
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("After recovery, status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestLogger(t *testing.T) {
	logger := log.New(os.Stdout, "TEST: ", log.LstdFlags)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := Logger(logger)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Authorization", "Bearer secret-token")

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestCORS(t *testing.T) {
	tests := []struct {
		name         string
		allowedOrigins []string
		origin       string
		method       string
		wantStatus   int
		wantCORSHeader string
	}{
		{
			name:         "Normal: OPTIONS request returns CORS headers",
			allowedOrigins: []string{"https://example.com"},
			origin:       "https://example.com",
			method:       "OPTIONS",
			wantStatus:   http.StatusNoContent,
			wantCORSHeader: "https://example.com",
		},
		{
			name:         "Edge: Request from disallowed origin returns 403",
			allowedOrigins: []string{"https://example.com"},
			origin:       "https://evil.com",
			method:       "GET",
			wantStatus:   http.StatusForbidden,
			wantCORSHeader: "",
		},
		{
			name:         "Normal: Wildcard allows all origins",
			allowedOrigins: []string{"*"},
			origin:       "https://anywhere.com",
			method:       "GET",
			wantStatus:   http.StatusOK,
			wantCORSHeader: "https://anywhere.com",
		},
		{
			name:         "Edge: Missing Origin header is handled gracefully",
			allowedOrigins: []string{"https://example.com"},
			origin:       "",
			method:       "GET",
			wantStatus:   http.StatusOK,
			wantCORSHeader: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			middleware := CORS(tt.allowedOrigins)
			wrapped := middleware(handler)

			req := httptest.NewRequest(tt.method, "/test", nil)
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}

			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rec.Code, tt.wantStatus)
			}

			if tt.wantCORSHeader != "" {
				corsHeader := rec.Header().Get("Access-Control-Allow-Origin")
				if corsHeader != tt.wantCORSHeader {
					t.Errorf("CORS header = %s, want %s", corsHeader, tt.wantCORSHeader)
				}
			}
		})
	}
}

func TestSecurityHeaders(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := SecurityHeaders()
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	tests := []struct {
		header string
		value  string
	}{
		{"X-Content-Type-Options", "nosniff"},
		{"X-Frame-Options", "DENY"},
		{"X-XSS-Protection", "1; mode=block"},
		{"Content-Security-Policy", "default-src 'self'"},
		{"Referrer-Policy", "strict-origin-when-cross-origin"},
	}

	for _, tt := range tests {
		t.Run(tt.header, func(t *testing.T) {
			got := rec.Header().Get(tt.header)
			if got != tt.value {
				t.Errorf("%s = %s, want %s", tt.header, got, tt.value)
			}
		})
	}
}
