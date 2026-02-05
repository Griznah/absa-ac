package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestTimingAttackMeasurement measures response time consistency for token comparison
// Current implementation: string comparison is NOT timing-safe (vulnerable)
// After fix: crypto/subtle.ConstantTimeCompare prevents timing attacks
func TestTimingAttackMeasurement(t *testing.T) {
	token := "valid-bearer-token-12345678"
	auth := BearerAuth(token, []string{})

	// Create test handler
	handler := auth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Measure valid token response time (100 iterations)
	validTimes := make([]time.Duration, 100)
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/api/config", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()

		start := time.Now()
		handler.ServeHTTP(rr, req)
		validTimes[i] = time.Since(start)
	}

	// Measure invalid token response time (100 iterations)
	// Wrong token: same length but different characters
	wrongToken := "valid-bearer-token-12345679"
	invalidTimes := make([]time.Duration, 100)
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest("GET", "/api/config", nil)
		req.Header.Set("Authorization", "Bearer "+wrongToken)
		rr := httptest.NewRecorder()

		start := time.Now()
		handler.ServeHTTP(rr, req)
		invalidTimes[i] = time.Since(start)
	}

	// Calculate average durations
	var avgValid, avgInvalid time.Duration
	for _, d := range validTimes {
		avgValid += d
	}
	avgValid /= time.Duration(len(validTimes))

	for _, d := range invalidTimes {
		avgInvalid += d
	}
	avgInvalid /= time.Duration(len(invalidTimes))

	// Current implementation: times will differ significantly (vulnerable)
	// After fix: times should be similar (within 20%)
	ratio := float64(avgInvalid) / float64(avgValid)
	t.Logf("Valid token avg: %v, Invalid token avg: %v, Ratio: %.2f", avgValid, avgInvalid, ratio)

	// This test demonstrates the vulnerability
	// After M2 implementation, ratio should approach 1.0
	if ratio < 0.8 || ratio > 1.2 {
		t.Log("WARNING: Token comparison times vary significantly (timing attack vulnerability)")
	}
}

// TestRateLimitIPSpoofing attempts to bypass rate limiting via X-Forwarded-For
// Current implementation: uses leftmost IP (can be spoofed by client)
// After fix: uses rightmost IP (last proxy, trusted)
func TestRateLimitIPSpoofing(t *testing.T) {
	rateLimit := RateLimit(2, 2, []string{}, context.Background()) // 2 req/sec, burst 2
	handler := rateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Spoofed X-Forwarded-For with multiple IPs
	// Format: "client-ip, proxy1-ip, proxy2-ip"
	spoofedHeader := "1.2.3.4, 10.0.0.1, 192.168.1.1"

	// Send 5 requests rapidly (should exceed rate limit)
	successCount := 0
	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/api/config", nil)
		req.Header.Set("X-Forwarded-For", spoofedHeader)
		rr := httptest.NewRecorder()

		handler.ServeHTTP(rr, req)

		if rr.Code == http.StatusOK {
			successCount++
		}
	}

	// Current implementation: extracts "1.2.3.4" (leftmost - spoofable)
	// Attacker can rotate leftmost IP to bypass rate limit
	if successCount > 2 {
		t.Logf("VULNERABLE: %d/5 requests succeeded (rate limit bypassed via X-Forwarded-For spoofing)", successCount)
	}

	// After fix: should extract "192.168.1.1" (rightmost - trusted proxy)
	// All requests from same rightmost IP should be rate limited
	t.Logf("Success count: %d/5", successCount)
}

// TestRateLimitMemoryBomb tests memory exhaustion via unique IPs
// Current implementation: map never cleaned up (unbounded growth)
// After fix: sync.Pool with 5-minute expiration
func TestRateLimitMemoryBomb(t *testing.T) {
	rateLimit := RateLimit(100, 100, []string{}, context.Background())
	handler := rateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Simulate 10,000 unique IP addresses
	uniqueIPs := make([]string, 10000)
	for i := 0; i < 10000; i++ {
		// Generate valid IP addresses by distributing across the fourth octet
		// and using the third octet for overflow (i / 256 gives 0-39 range)
		thirdOctet := i / 256
		fourthOctet := i % 256
		uniqueIPs[i] = fmt.Sprintf("192.168.%d.%d", thirdOctet, fourthOctet)
	}

	var wg sync.WaitGroup
	startTime := time.Now()

	// Send requests from all unique IPs concurrently
	for _, ip := range uniqueIPs {
		wg.Add(1)
		go func(clientIP string) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/api/config", nil)
			req.RemoteAddr = clientIP + ":12345"
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
		}(ip)
	}

	wg.Wait()
	elapsed := time.Since(startTime)

	t.Logf("Created rate limiters for 10,000 unique IPs in %v", elapsed)
	t.Log("WARNING: Limiters map grows unbounded (memory exhaustion risk)")

	// After fix: old limiters should be cleaned up after 5 minutes
	// Memory growth should be bounded
}

func toString(i int) string {
	return strconv.Itoa(i)
}

// TestCORSWildcardBypass tests CORS allowlist validation
// Current implementation: "*" allows all origins without validation
// After fix: strict allowlist, reject origins not explicitly allowed
func TestCORSWildcardBypass(t *testing.T) {
	// CORS with wildcard (should be rejected for security)
	cors := CORS([]string{"*"})
	handler := cors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	testCases := []struct {
		name   string
		origin string
		wantOK bool
	}{
		{"Same origin", "http://localhost:3001", true},
		{"Malicious origin", "http://evil.com", true}, // VULNERABLE: wildcard allows all
		{"Another malicious origin", "http://attacker.net", true}, // VULNERABLE
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/config", nil)
			req.Header.Set("Origin", tc.origin)
			rr := httptest.NewRecorder()

			handler.ServeHTTP(rr, req)

			if tc.wantOK && rr.Code != http.StatusOK {
				t.Errorf("Expected status OK for origin %s, got %d", tc.origin, rr.Code)
			}

			// Check if CORS headers were set
			corsHeader := rr.Header().Get("Access-Control-Allow-Origin")
			if corsHeader == tc.origin {
				t.Logf("CORS header set to: %s (wildcard allows malicious origin)", corsHeader)
			}
		})
	}
}

// TestOversizedPayload tests request size limits
// Current implementation: no size limit (memory exhaustion)
// After fix: 1MB limit, return 413 before decode
func TestOversizedPayload(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate JSON decode (vulnerable to memory exhaustion)
		w.WriteHeader(http.StatusOK)
	})

	// Create 10MB payload
	largePayload := strings.Repeat("x", 10*1024*1024)

	req := httptest.NewRequest("POST", "/api/config", strings.NewReader(largePayload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Length", toString(10*1024*1024))
	rr := httptest.NewRecorder()

	start := time.Now()
	handler.ServeHTTP(rr, req)
	elapsed := time.Since(start)

	t.Logf("Processed 10MB payload in %v", elapsed)
	t.Log("WARNING: No size limit (memory exhaustion risk)")

	// After fix: should return 413 Payload Too Large immediately
	// without reading entire body
}

// TestMalformedJSON tests decoder panic prevention
func TestMalformedJSON(t *testing.T) {
	malformedPayloads := []struct {
		name string
		body string
	}{
		{"Unclosed object", `{{`},
		{"Invalid escape", `{"x": "\u"}`},
		{"Trailing comma", `{"x": 1,}`},
	}

	for _, tc := range malformedPayloads {
		t.Run(tc.name, func(t *testing.T) {
			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var data map[string]interface{}
				decoder := json.NewDecoder(r.Body)
				if err := decoder.Decode(&data); err != nil {
					// Expected: should return error, not panic
					WriteError(w, http.StatusBadRequest, "Invalid JSON", err.Error())
					return
				}
				WriteJSON(w, http.StatusOK, data)
			})

			req := httptest.NewRequest("POST", "/api/config", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			// Should not panic
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Errorf("Handler panicked with: %v", r)
					}
				}()
				handler.ServeHTTP(rr, req)
			}()

			if rr.Code != http.StatusBadRequest {
				t.Errorf("Expected 400 for malformed JSON, got %d", rr.Code)
			}
		})
	}
}

// TestMaxBytesReader tests payload size limiting
func TestMaxBytesReader(t *testing.T) {
	// Test with MaxBytesReader (demonstrates current vulnerability)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const maxBodySize = 1 << 20 // 1MB
		r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

		var data map[string]interface{}
		decoder := json.NewDecoder(r.Body)
		if err := decoder.Decode(&data); err != nil {
			// MaxBytesReader returns "http: request body too large" error
			if err.Error() == "http: request body too large" || strings.Contains(err.Error(), "too large") {
				WriteError(w, http.StatusRequestEntityTooLarge, "Request body too large",
					"Maximum size is 1MB")
				return
			}
			WriteError(w, http.StatusBadRequest, "Invalid JSON", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, data)
	})

	// Create valid JSON object with oversized string field (2MB)
	largeValue := bytes.Repeat([]byte("x"), 2*1024*1024)
	largePayload := []byte(`{"large":"` + string(largeValue) + `"}`)

	req := httptest.NewRequest("POST", "/api/config", bytes.NewReader(largePayload))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	// After M2 fix: should return 413
	// Current: returns 400 (invalid JSON) or processes entire payload
	t.Logf("MaxBytesReader test returned status %d", rr.Code)
	if rr.Code == http.StatusRequestEntityTooLarge {
		t.Log("PASS: MaxBytesReader correctly rejected oversized payload")
	} else {
		t.Log("Current behavior: payload size limiting not fully effective (expected after M2)")
	}
}
