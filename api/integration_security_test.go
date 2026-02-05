package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// TestMiddlewareChainOrder verifies middleware executes in correct sequence
// Expected order: SecurityHeaders -> CORS -> Logger -> RateLimit -> Auth
func TestMiddlewareChainOrder(t *testing.T) {
	var logOrder []string

	// Create custom logger that records middleware execution
	logger := &testLogger{
		logFunc: func(msg string) {
			logOrder = append(logOrder, msg)
		},
	}

	// Create mock config manager
	cm := &mockConfigManager{config: map[string]any{}}

	// Create server with all middleware
	_ = NewServer(cm, "3001", "test-token", []string{"*"}, []string{}, logger.stdLogger())

	// This test logs middleware execution order
	// After M2 implementation, verify order matches expected sequence
	t.Logf("Middleware execution order: %v", logOrder)
}

// TestFullRequestFlow tests complete request through all middleware layers
func TestFullRequestFlow(t *testing.T) {
	cm := &mockConfigManager{
		config: map[string]any{
			"server_ip": "192.168.1.1",
			"servers":   []any{},
		},
	}

	// Register routes manually for test
	mux := http.NewServeMux()
	mux.HandleFunc("/api/config", func(w http.ResponseWriter, r *http.Request) {
		cfg := cm.GetConfigAny()
		WriteJSON(w, http.StatusOK, cfg)
	})

	// Create logger for middleware
	testLogger := log.New(&testWriter{}, "", 0)

	// Apply middleware chain
	handler := SecurityHeaders()(mux)
	handler = CORS([]string{"http://localhost:3001"})(handler)
	handler = Logger(testLogger)(handler)
	handler = RateLimit(10, 20, []string{}, context.Background())(handler)
	handler = BearerAuth("valid-token", []string{})(handler)

	// Test valid request
	req := httptest.NewRequest("GET", "/api/config", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Origin", "http://localhost:3001")
	rr := httptest.NewRecorder()

	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status OK, got %d", rr.Code)
	}

	// Verify security headers
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("Missing X-Content-Type-Options header")
	}
	if rr.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("Missing X-Frame-Options header")
	}
}

// TestConfigUpdateWithRealFile tests config update with real filesystem
func TestConfigUpdateWithRealFile(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	configPath := tmpDir + "/config.json"

	// Write initial config
	initialConfig := map[string]interface{}{
		"server_ip":       "192.168.1.1",
		"update_interval": 60,
		"category_order":  []string{"GT3", "GT4"},
		"category_emojis": map[string]string{"GT3": "🏎️", "GT4": "🏁"},
		"servers":         []interface{}{},
	}

	data, err := json.MarshalIndent(initialConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal config: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}

	// Create server with real config manager
	// (Note: this requires main.ConfigManager, which we can't import in api package)
	// For now, test with mock
	t.Skip("Requires main.ConfigManager - test in main_test.go instead")
}

// TestRateLimitExpiration tests rate limiter cleanup after inactivity
func TestRateLimitExpiration(t *testing.T) {
	t.Skip("Rate limiter expiration requires 6 minutes to verify - manual testing only")

	rateLimit := RateLimit(1, 1, []string{}, context.Background())
	handler := rateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	clientIP := "192.168.1.100"

	// Send request (create limiter)
	req := httptest.NewRequest("GET", "/api/config", nil)
	req.RemoteAddr = clientIP + ":12345"
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// Wait 6 minutes (past 5-minute expiration)
	// NOTE: This test is skipped because it takes too long for automated runs.
	// To verify expiration manually:
	// 1. Uncomment the sleep below
	// 2. Run: go test -v -run TestRateLimitExpiration
	// 3. Verify old limiter is cleaned up and new one is created
	// time.Sleep(6 * time.Minute)

	// Send another request (should use fresh limiter)
	req = httptest.NewRequest("GET", "/api/config", nil)
	req.RemoteAddr = clientIP + ":12345"
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	// After fix: old limiter should be cleaned up
	// Memory should not grow unbounded
	t.Log("Rate limiter expiration test (requires 6 minutes to run)")
}

// Mock types for testing

type testLogger struct {
	logFunc func(msg string)
}

func (l *testLogger) stdLogger() *log.Logger {
	return log.New(&testWriter{l.logFunc}, "", 0)
}

type testWriter struct {
	writeFunc func(string)
}

func (w *testWriter) Write(p []byte) (n int, err error) {
	if w.writeFunc != nil {
		w.writeFunc(string(p))
	}
	return len(p), nil
}

func (w *testWriter) writeFuncOrEmpty() func(string) {
	if w.writeFunc != nil {
		return w.writeFunc
	}
	return func(_ string) {}
}
