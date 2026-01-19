package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestCSRFTokenGeneration verifies CSRF token is generated and accessible
func TestCSRFTokenGeneration(t *testing.T) {
	// Reset token for test
	csrfTokenOnce = *new(sync.Once)

	initCSRFToken()

	token := GetCSRFToken()
	if token == "" {
		t.Errorf("CSRF token should not be empty")
	}

	if len(token) < 32 {
		t.Errorf("CSRF token should be at least 32 chars, got %d", len(token))
	}
}

// TestCSRFTokenRotation verifies token rotation works
func TestCSRFTokenRotation(t *testing.T) {
	// Reset token for test
	csrfTokenOnce = *new(sync.Once)
	initCSRFToken()

	oldToken := GetCSRFToken()
	newToken := RotateCSRFToken()

	if oldToken == newToken {
		t.Errorf("Rotated token should be different from original")
	}

	if GetCSRFToken() != newToken {
		t.Errorf("GetCSRFToken should return rotated token")
	}
}

// TestCSRFTokenEndpoint verifies /api/csrf-token returns token
func TestCSRFTokenEndpoint(t *testing.T) {
	cm := &mockConfigManager{}
	server := NewServer(cm, "8080", "test-token", []string{"*"}, nil)
	mux := http.NewServeMux()
	RegisterRoutes(mux, server)

	// Apply auth middleware (CSRF token endpoint requires auth)
	authMiddleware := BearerAuth("test-token")
	wrappedMux := authMiddleware(mux)

	req := httptest.NewRequest("GET", "/api/csrf-token", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	wrappedMux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	csrfToken, ok := response["csrf_token"].(string)
	if !ok || csrfToken == "" {
		t.Errorf("Response should contain csrf_token")
	}
}

// TestCSRFMiddleware_ValidToken accepts valid token
func TestCSRFMiddleware_ValidToken(t *testing.T) {
	csrfTokenOnce = *new(sync.Once)
	initCSRFToken()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	middleware := CSRF(handler)

	req := httptest.NewRequest("PATCH", "/api/config", nil)
	req.Header.Set("X-CSRF-Token", GetCSRFToken())
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200 with valid token, got %d", rec.Code)
	}

	body := rec.Body.String()
	if body != "success" {
		t.Errorf("Expected body 'success', got '%s'", body)
	}
}

// TestCSRFMiddleware_MissingToken rejects missing token
func TestCSRFMiddleware_MissingToken(t *testing.T) {
	csrfTokenOnce = *new(sync.Once)
	initCSRFToken()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := CSRF(handler)

	req := httptest.NewRequest("PATCH", "/api/config", nil)
	// No X-CSRF-Token header
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403 without token, got %d", rec.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["error"] != "CSRF token missing" {
		t.Errorf("Expected error 'CSRF token missing', got '%v'", response["error"])
	}
}

// TestCSRFMiddleware_InvalidToken rejects invalid token
func TestCSRFMiddleware_InvalidToken(t *testing.T) {
	csrfTokenOnce = *new(sync.Once)
	initCSRFToken()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := CSRF(handler)

	req := httptest.NewRequest("PATCH", "/api/config", nil)
	req.Header.Set("X-CSRF-Token", "invalid-token-12345")
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected status 403 with invalid token, got %d", rec.Code)
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response["error"] != "CSRF token invalid" {
		t.Errorf("Expected error 'CSRF token invalid', got '%v'", response["error"])
	}
}

// TestCSRFMiddleware_SafeMethodsBypassCSRF verifies safe methods don't require token
func TestCSRFMiddleware_SafeMethodsBypassCSRF(t *testing.T) {
	csrfTokenOnce = *new(sync.Once)
	initCSRFToken()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	middleware := CSRF(handler)

	safeMethods := []string{"GET", "HEAD", "OPTIONS"}

	for _, method := range safeMethods {
		req := httptest.NewRequest(method, "/api/config", nil)
		// No X-CSRF-Token header
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("Safe method %s should bypass CSRF, got status %d", method, rec.Code)
		}
	}
}

// TestCSRFMiddleware_HealthCheckBypass verifies /health bypasses CSRF
func TestCSRFMiddleware_HealthCheckBypass(t *testing.T) {
	csrfTokenOnce = *new(sync.Once)
	initCSRFToken()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	middleware := CSRF(handler)

	req := httptest.NewRequest("POST", "/health", nil)
	// No X-CSRF-Token header, POST method but /health path
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("/health should bypass CSRF, got status %d", rec.Code)
	}
}

// TestCSRFMiddleware_CSRFTokenEndpointBypass verifies /api/csrf-token bypasses CSRF
func TestCSRFMiddleware_CSRFTokenEndpointBypass(t *testing.T) {
	csrfTokenOnce = *new(sync.Once)
	initCSRFToken()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("token"))
	})

	middleware := CSRF(handler)

	req := httptest.NewRequest("POST", "/api/csrf-token", nil)
	// No X-CSRF-Token header, POST method but /api/csrf-token path
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("/api/csrf-token should bypass CSRF, got status %d", rec.Code)
	}
}

// TestCSRFMiddleware_StateChangingMethodsRequireToken verifies POST/PATCH/PUT/DELETE require token
func TestCSRFMiddleware_StateChangingMethodsRequireToken(t *testing.T) {
	csrfTokenOnce = *new(sync.Once)
	initCSRFToken()
	validToken := GetCSRFToken()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := CSRF(handler)

	stateChangingMethods := []string{"POST", "PATCH", "PUT", "DELETE"}

	for _, method := range stateChangingMethods {
		// Test without token - should fail
		req := httptest.NewRequest(method, "/api/config", nil)
		rec := httptest.NewRecorder()
		middleware.ServeHTTP(rec, req)

		if rec.Code != http.StatusForbidden {
			t.Errorf("%s without token should return 403, got %d", method, rec.Code)
		}

		// Test with valid token - should succeed
		req2 := httptest.NewRequest(method, "/api/config", nil)
		req2.Header.Set("X-CSRF-Token", validToken)
		rec2 := httptest.NewRecorder()
		middleware.ServeHTTP(rec2, req2)

		if rec2.Code != http.StatusOK {
			t.Errorf("%s with valid token should return 200, got %d", method, rec2.Code)
		}
	}
}

// TestCSRFIntegration_FullRequestFlow tests end-to-end CSRF in API request
func TestCSRFIntegration_FullRequestFlow(t *testing.T) {
	cm := &mockConfigManager{}
	server := NewServer(cm, "8080", "test-token", []string{"*"}, nil)
	mux := http.NewServeMux()
	RegisterRoutes(mux, server)

	// Apply middleware chain (same as server.Start)
	authMiddleware := BearerAuth("test-token")
	csrfMiddleware := CSRF

	wrappedMux := csrfMiddleware(authMiddleware(mux))

	// Step 1: Fetch CSRF token (requires auth)
	req1 := httptest.NewRequest("GET", "/api/csrf-token", nil)
	req1.Header.Set("Authorization", "Bearer test-token")
	rec1 := httptest.NewRecorder()
	wrappedMux.ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Fatalf("Failed to fetch CSRF token: status %d", rec1.Code)
	}

	var tokenResponse map[string]interface{}
	if err := json.Unmarshal(rec1.Body.Bytes(), &tokenResponse); err != nil {
		t.Fatalf("Failed to parse token response: %v", err)
	}

	csrfToken, ok := tokenResponse["csrf_token"].(string)
	if !ok || csrfToken == "" {
		t.Fatalf("Invalid CSRF token response")
	}

	// Step 2: Use token for PATCH request
	req2 := httptest.NewRequest("PATCH", "/api/config", strings.NewReader(`{"update_interval": 60}`))
	req2.Header.Set("Authorization", "Bearer test-token")
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("X-CSRF-Token", csrfToken)
	rec2 := httptest.NewRecorder()
	wrappedMux.ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusOK {
		t.Errorf("PATCH with valid CSRF token should succeed, got status %d", rec2.Code)
	}

	// Step 3: Try PATCH without token - should fail
	req3 := httptest.NewRequest("PATCH", "/api/config", strings.NewReader(`{"update_interval": 30}`))
	req3.Header.Set("Authorization", "Bearer test-token")
	req3.Header.Set("Content-Type", "application/json")
	// No X-CSRF-Token header
	rec3 := httptest.NewRecorder()
	wrappedMux.ServeHTTP(rec3, req3)

	if rec3.Code != http.StatusForbidden {
		t.Errorf("PATCH without CSRF token should fail, got status %d", rec3.Code)
	}

	var errorResponse map[string]interface{}
	if err := json.Unmarshal(rec3.Body.Bytes(), &errorResponse); err != nil {
		t.Fatalf("Failed to parse error response: %v", err)
	}

	errorMsg, ok := errorResponse["error"].(string)
	if !ok || !strings.Contains(errorMsg, "CSRF") {
		t.Errorf("Expected CSRF error, got: %v", errorResponse)
	}
}

// TestCSRFTokenTimingSafety verifies timing-safe comparison works
func TestCSRFTokenTimingSafety(t *testing.T) {
	// Test equal tokens
	if !compareTokens("abc123", "abc123") {
		t.Errorf("Equal tokens should return true")
	}

	// Test different tokens same length
	if compareTokens("abc123", "xyz789") {
		t.Errorf("Different tokens should return false")
	}

	// Test different lengths
	if compareTokens("abc123", "abc") {
		t.Errorf("Tokens of different lengths should return false")
	}

	// Test empty tokens (should be rejected for security)
	if compareTokens("", "") {
		t.Errorf("Empty tokens should return false (security: empty tokens invalid)")
	}
}

// TestCSRFMiddleware_WithCustomExemptPaths tests CSRFWithConfig
func TestCSRFMiddleware_WithCustomExemptPaths(t *testing.T) {
	csrfTokenOnce = *new(sync.Once)
	initCSRFToken()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := CSRFWithConfig([]string{"/api/webhook"})

	// Test exempt path
	req1 := httptest.NewRequest("POST", "/api/webhook", nil)
	rec1 := httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec1, req1)

	if rec1.Code != http.StatusOK {
		t.Errorf("Exempt path /api/webhook should bypass CSRF, got status %d", rec1.Code)
	}

	// Test non-exempt path
	req2 := httptest.NewRequest("POST", "/api/config", nil)
	rec2 := httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec2, req2)

	if rec2.Code != http.StatusForbidden {
		t.Errorf("Non-exempt path should require CSRF token, got status %d", rec2.Code)
	}
}
