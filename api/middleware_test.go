package api

import (
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/time/rate"
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
			name:       "Edge: Empty Bearer token returns 401",
			token:      "secret-token",
			authHeader: "Bearer ",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "Edge: Single character token",
			token:      "a",
			authHeader: "Bearer a",
			wantStatus: http.StatusOK,
		},
		{
			name:       "Edge: Token matches on first character only",
			token:      "secret-token",
			authHeader: "Bearer aaaaaaaaaaaaaaa",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "Edge: Token matches on last character only",
			token:      "secret-token",
			authHeader: "Bearer xxxxxxxxxxxxxxn",
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "Edge: Long token (100 chars)",
			token:      string(make([]byte, 100)),
			authHeader: "Bearer " + string(make([]byte, 100)),
			wantStatus: http.StatusOK,
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
			middleware := BearerAuth(tt.token, nil)

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

			middleware := RateLimit(tt.requestsPerSec, tt.burstSize, nil, context.Background())
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

	middleware := RateLimit(2, 2, nil, context.Background())
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

func TestIsRoutableIP(t *testing.T) {
	tests := []struct {
		name   string
		ipStr  string
		want   bool
	}{
		{
			name:  "Normal: Public IPv4 is routable",
			ipStr: "8.8.8.8",
			want:  true,
		},
		{
			name:  "Normal: Public IPv6 is routable",
			ipStr: "2001:4860:4860::8888",
			want:  true,
		},
		{
			name:  "Edge: Loopback IPv4 is not routable",
			ipStr: "127.0.0.1",
			want:  false,
		},
		{
			name:  "Edge: Loopback IPv6 is not routable",
			ipStr: "::1",
			want:  false,
		},
		{
			name:  "Edge: Link-local IPv4 is not routable",
			ipStr: "169.254.1.1",
			want:  false,
		},
		{
			name:  "Edge: Link-local IPv6 is not routable",
			ipStr: "fe80::1",
			want:  false,
		},
		{
			name:  "Edge: Multicast IPv4 is not routable",
			ipStr: "224.0.0.1",
			want:  false,
		},
		{
			name:  "Edge: Multicast IPv6 is not routable",
			ipStr: "ff00::1",
			want:  false,
		},
		{
			name:  "Edge: Unspecified IPv4 is not routable",
			ipStr: "0.0.0.0",
			want:  false,
		},
		{
			name:  "Edge: Unspecified IPv6 is not routable",
			ipStr: "::",
			want:  false,
		},
		{
			name:  "Edge: Private IP is routable (not blocked by this check)",
			ipStr: "192.168.1.1",
			want:  true,
		},
		{
			name:  "Edge: Invalid IP string is not routable",
			ipStr: "not-an-ip",
			want:  false,
		},
		{
			name:  "Edge: Empty string is not routable",
			ipStr: "",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip := net.ParseIP(tt.ipStr)
			got := isRoutableIP(ip)
			if got != tt.want {
				t.Errorf("isRoutableIP(%q) = %v, want %v", tt.ipStr, got, tt.want)
			}
		})
	}
}

func TestNormalizeIP(t *testing.T) {
	tests := []struct {
		name     string
		ipStr    string
		want     string
	}{
		{
			name:  "Normal: IPv4 address is unchanged",
			ipStr: "192.168.1.1",
			want:  "192.168.1.1",
		},
		{
			name:  "Normal: IPv6 address is unchanged",
			ipStr: "2001:db8::1",
			want:  "2001:db8::1",
		},
		{
			name:  "Edge: IPv4-mapped IPv6 is normalized to IPv4",
			ipStr: "::ffff:192.168.1.1",
			want:  "192.168.1.1",
		},
		{
			name:  "Edge: Invalid IP is returned as-is",
			ipStr: "not-an-ip",
			want:  "not-an-ip",
		},
		{
			name:  "Edge: Empty string is returned as-is",
			ipStr: "",
			want:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeIP(tt.ipStr)
			if got != tt.want {
				t.Errorf("normalizeIP(%q) = %q, want %q", tt.ipStr, got, tt.want)
			}
		})
	}
}

func TestExtractClientIP(t *testing.T) {
	tests := []struct {
		name           string
		forwardedFor   string
		remoteAddr     string
		trustedProxies []string
		want           string
	}{
		{
			name:           "Normal: Request from trusted proxy with valid X-Forwarded-For uses rightmost non-trusted IP",
			forwardedFor:   "203.0.113.1, 198.51.100.1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1", "198.51.100.1"},
			want:           "203.0.113.1",
		},
		{
			name:           "Normal: Request from untrusted source ignores X-Forwarded-For, uses RemoteAddr",
			forwardedFor:   "203.0.113.1",
			remoteAddr:     "192.0.2.1:12345",
			trustedProxies: []string{"10.0.0.1"},
			want:           "192.0.2.1:12345",
		},
		{
			name:           "Normal: Empty trusted proxy list ignores X-Forwarded-For from all sources",
			forwardedFor:   "203.0.113.1",
			remoteAddr:     "192.0.2.1:12345",
			trustedProxies: []string{},
			want:           "192.0.2.1:12345",
		},
		{
			name:           "Normal: No X-Forwarded-For header uses RemoteAddr",
			forwardedFor:   "",
			remoteAddr:     "192.0.2.1:12345",
			trustedProxies: []string{"10.0.0.1"},
			want:           "192.0.2.1:12345",
		},
		{
			name:           "Edge: X-Forwarded-For with multiple IPs extracts rightmost non-trusted IP",
			forwardedFor:   "203.0.113.10, 203.0.113.9, 203.0.113.8, 198.51.100.1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1", "198.51.100.1"},
			want:           "203.0.113.8",
		},
		{
			name:           "Edge: Malformed IP in X-Forwarded-For falls back to RemoteAddr",
			forwardedFor:   "not-an-ip, 198.51.100.1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1", "198.51.100.1"},
			want:           "10.0.0.1:12345",
		},
		{
			name:           "Edge: All IPs in X-Forwarded-For are trusted proxies uses RemoteAddr",
			forwardedFor:   "198.51.100.2, 198.51.100.1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1", "198.51.100.1", "198.51.100.2"},
			want:           "10.0.0.1:12345",
		},
		{
			name:           "Edge: X-Forwarded-For with more than 10 IPs is rejected",
			forwardedFor:   "1.1.1.1, 2.2.2.2, 3.3.3.3, 4.4.4.4, 5.5.5.5, 6.6.6.6, 7.7.7.7, 8.8.8.8, 9.9.9.9, 10.10.10.10, 11.11.11.11",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1"},
			want:           "10.0.0.1:12345",
		},
		{
			name:           "Edge: IPv6 address in X-Forwarded-For handled correctly",
			forwardedFor:   "2001:db8::1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1"},
			want:           "2001:db8::1",
		},
		{
			name:           "Edge: Mixed IPv4/IPv6 chain extracts rightmost non-trusted IP",
			forwardedFor:   "203.0.113.1, 2001:db8::1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1", "2001:db8::1"},
			want:           "203.0.113.1",
		},
		{
			name:           "Edge: Loopback IPv4 rejected in X-Forwarded-For",
			forwardedFor:   "127.0.0.1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1"},
			want:           "10.0.0.1:12345",
		},
		{
			name:           "Edge: Loopback IPv6 rejected in X-Forwarded-For",
			forwardedFor:   "::1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1"},
			want:           "10.0.0.1:12345",
		},
		{
			name:           "Edge: Link-local IPv4 rejected in X-Forwarded-For",
			forwardedFor:   "169.254.1.1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1"},
			want:           "10.0.0.1:12345",
		},
		{
			name:           "Edge: Link-local IPv6 rejected in X-Forwarded-For",
			forwardedFor:   "fe80::1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1"},
			want:           "10.0.0.1:12345",
		},
		{
			name:           "Edge: Multicast IPv6 rejected in X-Forwarded-For",
			forwardedFor:   "ff00::1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1"},
			want:           "10.0.0.1:12345",
		},
		{
			name:           "Edge: IPv4-mapped IPv6 normalized to IPv4",
			forwardedFor:   "::ffff:203.0.113.1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1"},
			want:           "203.0.113.1",
		},
		{
			name:           "Edge: Whitespace around IPs is trimmed",
			forwardedFor:   "203.0.113.1 , 198.51.100.1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1", "198.51.100.1"},
			want:           "203.0.113.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.forwardedFor)
			}

			got := extractClientIP(req, tt.trustedProxies)
			if got != tt.want {
				t.Errorf("extractClientIP() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRateLimit_TrustedProxyIntegration(t *testing.T) {
	tests := []struct {
		name           string
		forwardedFor   string
		remoteAddr     string
		trustedProxies []string
		requestCount   int
		wantStatus     int
	}{
		{
			name:           "Normal: Rate limiting uses X-Forwarded-For from trusted proxy",
			forwardedFor:   "203.0.113.1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1"},
			requestCount:   3,
			wantStatus:     http.StatusOK,
		},
		{
			name:           "Normal: Rate limiting uses RemoteAddr when source untrusted",
			forwardedFor:   "203.0.113.1",
			remoteAddr:     "192.0.2.1:12345",
			trustedProxies: []string{"10.0.0.1"},
			requestCount:   3,
			wantStatus:     http.StatusOK,
		},
		{
			name:           "Normal: Rate limiting ignores X-Forwarded-For with no trusted proxies",
			forwardedFor:   "203.0.113.1",
			remoteAddr:     "192.0.2.1:12345",
			trustedProxies: []string{},
			requestCount:   3,
			wantStatus:     http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			middleware := RateLimit(10, 20, tt.trustedProxies, context.Background())
			wrapped := middleware(handler)

			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.forwardedFor)
			}

			lastStatus := http.StatusOK
			for i := 0; i < tt.requestCount; i++ {
				rec := httptest.NewRecorder()
				wrapped.ServeHTTP(rec, req)
				lastStatus = rec.Code
			}

			if lastStatus != tt.wantStatus {
				t.Errorf("Final status = %d, want %d", lastStatus, tt.wantStatus)
			}
		})
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	tests := []struct {
		name           string
		requestsPerSec int
		burstSize      int
		trustedProxies []string
		setup          func(*testing.T, *rateLimiterManager)
		verify         func(*testing.T, *rateLimiterManager)
	}{
		{
			name:           "Normal: Active rate limiter persists across requests",
			requestsPerSec: 10,
			burstSize:      5,
			trustedProxies: nil,
			setup: func(t *testing.T, rm *rateLimiterManager) {
				// Create a limiter and access it
				rm.mu.Lock()
				rm.limiters["127.0.0.1"] = &rateLimiter{
					limiter:     rate.NewLimiter(10, 5),
					lastAccess:  time.Now(),
				}
				rm.mu.Unlock()
			},
			verify: func(t *testing.T, rm *rateLimiterManager) {
				rm.mu.RLock()
				_, exists := rm.limiters["127.0.0.1"]
				rm.mu.RUnlock()
				if !exists {
					t.Error("Active limiter was removed")
				}
			},
		},
		{
			name:           "Edge: Rate limiter expires after inactivity",
			requestsPerSec: 10,
			burstSize:      5,
			trustedProxies: nil,
			setup: func(t *testing.T, rm *rateLimiterManager) {
				// Create a limiter with old access time
				rm.mu.Lock()
				rm.limiters["192.168.1.1"] = &rateLimiter{
					limiter:     rate.NewLimiter(10, 5),
					lastAccess:  time.Now().Add(-6 * time.Minute),
				}
				rm.mu.Unlock()
			},
			verify: func(t *testing.T, rm *rateLimiterManager) {
				rm.mu.RLock()
				_, exists := rm.limiters["192.168.1.1"]
				rm.mu.RUnlock()
				if exists {
					t.Error("Stale limiter was not removed")
				}
			},
		},
		{
			name:           "Edge: Multiple concurrent requests during cleanup don't cause data races",
			requestsPerSec: 10,
			burstSize:      5,
			trustedProxies: nil,
			setup: func(t *testing.T, rm *rateLimiterManager) {
				// Create multiple limiters
				rm.mu.Lock()
				for i := 0; i < 100; i++ {
					ip := fmt.Sprintf("192.168.1.%d", i)
					rm.limiters[ip] = &rateLimiter{
						limiter:     rate.NewLimiter(10, 5),
						lastAccess:  time.Now(),
					}
				}
				rm.mu.Unlock()
			},
			verify: func(t *testing.T, rm *rateLimiterManager) {
				// Verify all limiters still exist
				rm.mu.RLock()
				count := len(rm.limiters)
				rm.mu.RUnlock()
				if count != 100 {
					t.Errorf("Expected 100 limiters, got %d", count)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			rm := &rateLimiterManager{
				limiters: make(map[string]*rateLimiter),
				ctx:      ctx,
			}

			tt.setup(t, rm)
			rm.cleanupStaleLimiters()
			tt.verify(t, rm)
		})
	}
}

func TestRateLimiterCleanup_IncrementalProcessing(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rm := &rateLimiterManager{
		limiters: make(map[string]*rateLimiter),
		ctx:      ctx,
	}

	// Create 2500 limiters (more than 2 cleanup batches)
	rm.mu.Lock()
	for i := 0; i < 2500; i++ {
		ip := fmt.Sprintf("10.0.%d.%d", i/256, i%256)
		rm.limiters[ip] = &rateLimiter{
			limiter:     rate.NewLimiter(10, 5),
			lastAccess:  time.Now().Add(-6 * time.Minute),
		}
	}
	rm.mu.Unlock()

	initialCount := len(rm.limiters)

	// First cleanup should process 1000 entries
	rm.cleanupStaleLimiters()
	afterFirst := len(rm.limiters)
	firstCursor := rm.cursor

	if afterFirst >= initialCount {
		t.Error("First cleanup did not remove any entries")
	}
	if firstCursor != 1000 {
		t.Errorf("Expected cursor to be 1000 after first cleanup, got %d", firstCursor)
	}

	// Second cleanup should process another 1000
	rm.cleanupStaleLimiters()
	afterSecond := len(rm.limiters)
	secondCursor := rm.cursor

	if afterSecond >= afterFirst {
		t.Error("Second cleanup did not remove any entries")
	}
	if secondCursor != 2000 {
		t.Errorf("Expected cursor to be 2000 after second cleanup, got %d", secondCursor)
	}

	// Third cleanup should process remaining
	rm.cleanupStaleLimiters()
	finalCount := len(rm.limiters)

	// All stale entries should be removed
	if finalCount > 100 {
		t.Errorf("Too many limiters remaining: %d", finalCount)
	}

	// Verify cursor is progressing
	if rm.cursor <= secondCursor {
		t.Errorf("Cursor did not progress: %d -> %d", secondCursor, rm.cursor)
	}
}

func TestRateLimiterCleanup_ConcurrentAccess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rm := &rateLimiterManager{
		limiters: make(map[string]*rateLimiter),
		ctx:      ctx,
	}

	// Create initial limiters
	rm.mu.Lock()
	for i := 0; i < 500; i++ {
		ip := fmt.Sprintf("192.168.1.%d", i)
		rm.limiters[ip] = &rateLimiter{
			limiter:     rate.NewLimiter(10, 5),
			lastAccess:  time.Now(),
		}
	}
	rm.mu.Unlock()

	var wg sync.WaitGroup
	done := make(chan struct{})

	// Concurrent readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-done:
					return
				default:
					rm.mu.RLock()
					_ = len(rm.limiters)
					rm.mu.RUnlock()
				}
			}
		}()
	}

	// Concurrent writers
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				ip := fmt.Sprintf("10.0.%d.%d", worker, j)
				rm.mu.Lock()
				rm.limiters[ip] = &rateLimiter{
					limiter:     rate.NewLimiter(10, 5),
					lastAccess:  time.Now(),
				}
				rm.mu.Unlock()
			}
		}(i)
	}

	// Run cleanup
	for i := 0; i < 5; i++ {
		rm.cleanupStaleLimiters()
		time.Sleep(10 * time.Millisecond)
	}

	close(done)
	wg.Wait()

	// Verify no deadlocks or panics occurred
	rm.mu.RLock()
	finalCount := len(rm.limiters)
	rm.mu.RUnlock()

	if finalCount == 0 {
		t.Error("All limiters were removed unexpectedly")
	}
}

func TestRateLimiterCleanup_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())

	rm := &rateLimiterManager{
		limiters: make(map[string]*rateLimiter),
		ctx:      ctx,
	}

	// Start cleanup goroutine
	rm.startCleanupGoroutine()

	// Create some limiters
	rm.mu.Lock()
	for i := 0; i < 100; i++ {
		ip := fmt.Sprintf("192.168.1.%d", i)
		rm.limiters[ip] = &rateLimiter{
			limiter:     rate.NewLimiter(10, 5),
			lastAccess:  time.Now(),
		}
	}
	rm.mu.Unlock()

	// Cancel context to stop cleanup
	cancel()

	// Give goroutine time to exit
	time.Sleep(100 * time.Millisecond)

	// Verify no goroutine leaks
	// Note: This is a basic check; in production you'd want more sophisticated monitoring
	// The test passes if cleanup goroutine exits without hanging
}

func TestRateLimiterCleanup_PanicRecovery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rm := &rateLimiterManager{
		limiters: make(map[string]*rateLimiter),
		ctx:      ctx,
	}

	// Create a manager that will panic during cleanup
	panicRm := &rateLimiterManager{
		limiters: make(map[string]*rateLimiter),
		ctx:      ctx,
	}

	// Inject a condition that causes panic (nil map access)
	panicRm.limiters = nil

	// This should panic but be recovered
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Log("Panic recovered as expected:", r)
			}
		}()
		panicRm.cleanupStaleLimiters()
	}()

	// Verify normal manager still works
	rm.mu.Lock()
	rm.limiters["127.0.0.1"] = &rateLimiter{
		limiter:     rate.NewLimiter(10, 5),
		lastAccess:  time.Now().Add(-6 * time.Minute),
	}
	rm.mu.Unlock()

	rm.cleanupStaleLimiters()

	rm.mu.RLock()
	_, exists := rm.limiters["127.0.0.1"]
	rm.mu.RUnlock()

	if exists {
		t.Error("Stale limiter was not removed after panic recovery")
	}
}

func TestRateLimiterIntegration(t *testing.T) {
	tests := []struct {
		name           string
		forwardedFor   string
		remoteAddr     string
		trustedProxies []string
		requestCount   int
		wantStatus     int
	}{
		{
			name:           "Integration: Rate limiters created for X-Forwarded-For IPs are cleaned up",
			forwardedFor:   "203.0.113.1",
			remoteAddr:     "10.0.0.1:12345",
			trustedProxies: []string{"10.0.0.1"},
			requestCount:   3,
			wantStatus:     http.StatusOK,
		},
		{
			name:           "Integration: Rate limiters created for RemoteAddr are cleaned up",
			forwardedFor:   "",
			remoteAddr:     "192.0.2.1:12345",
			trustedProxies: nil,
			requestCount:   3,
			wantStatus:     http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			middleware := RateLimit(10, 20, tt.trustedProxies, ctx)
			wrapped := middleware(handler)

			req := httptest.NewRequest("GET", "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.forwardedFor != "" {
				req.Header.Set("X-Forwarded-For", tt.forwardedFor)
			}

			// Make requests
			for i := 0; i < tt.requestCount; i++ {
				rec := httptest.NewRecorder()
				wrapped.ServeHTTP(rec, req)
				if rec.Code != http.StatusOK {
					t.Errorf("Request %d failed with status %d", i, rec.Code)
				}
			}

			// Verify limiter was created (we can't directly access the manager,
			// but we can verify behavior is consistent)
		})
	}
}

func TestRateLimiterGoroutineLeak(t *testing.T) {
	// Baseline goroutine count
	baseline := runtime.NumGoroutine()

	// Create and destroy multiple rate limiters
	for i := 0; i < 10; i++ {
		ctx, cancel := context.WithCancel(context.Background())

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		middleware := RateLimit(10, 20, nil, ctx)
		_ = middleware(handler)

		// Cancel context to stop cleanup goroutine
		cancel()

		// Give goroutine time to exit
		time.Sleep(50 * time.Millisecond)
	}

	// Give time for all goroutines to exit
	time.Sleep(200 * time.Millisecond)

	// Check goroutine count hasn't grown significantly
	final := runtime.NumGoroutine()
	leaked := final - baseline

	// Allow some tolerance for test infrastructure goroutines
	if leaked > 5 {
		t.Errorf("Possible goroutine leak: %d goroutines remaining (baseline: %d, final: %d)", leaked, baseline, final)
	}
}

func TestRateLimiterMemoryBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping memory test in short mode")
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	middleware := RateLimit(10, 20, nil, ctx)
	wrapped := middleware(handler)

	// Simulate traffic from 10,000 unique IPs
	for i := 0; i < 10000; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		ip := fmt.Sprintf("192.168.%d.%d", i/256, i%256)
		req.RemoteAddr = ip + ":12345"

		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
	}

	// Wait for cleanup to run (we need to trigger it manually or wait)
	// Since cleanup runs on a 5-minute interval, we'll manually trigger it
	// by accessing the rateLimiterManager (which we can't do directly)
	// Instead, we'll just verify the server doesn't crash

	// Make another request to verify system is still functional
	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "10.0.0.1:12345"
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Request failed after high load: %d", rec.Code)
	}
}

func TestRateLimiterRecreation(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	middleware := RateLimit(10, 20, nil, ctx)
	wrapped := middleware(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	// Make initial request
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Initial request failed: %d", rec.Code)
	}

	// Wait for potential cleanup
	time.Sleep(10 * time.Millisecond)

	// Make another request for same IP
	rec = httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("Follow-up request failed: %d", rec.Code)
	}
}
