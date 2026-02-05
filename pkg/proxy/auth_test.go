package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuthMiddleware_ValidSession(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	session, err := store.Create("test-token", 0)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest("GET", "/proxy/api/config", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: session.ID,
	})

	rr := httptest.NewRecorder()

	handlerCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		session, ok := GetSession(r)
		if !ok {
			t.Error("Session not found in context")
		}
		if session == nil {
			t.Error("Session is nil")
		}
		// Use GetToken() to decrypt and verify
		token, err := store.GetToken(session.ID)
		if err != nil {
			t.Errorf("GetToken() error = %v", err)
		}
		if token != "test-token" {
			t.Errorf("Expected token 'test-token', got '%s'", token)
		}
	})

	middleware := AuthMiddleware(nextHandler, store)
	middleware.ServeHTTP(rr, req)

	if !handlerCalled {
		t.Error("Next handler was not called")
	}

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestAuthMiddleware_MissingCookie(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/proxy/api/config", nil)
	rr := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Next handler should not be called")
	})

	middleware := AuthMiddleware(nextHandler, store)
	middleware.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rr.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !strings.Contains(resp.Error, "session cookie required") {
		t.Errorf("Expected error message about missing cookie, got: %s", resp.Error)
	}
}

func TestAuthMiddleware_EmptySessionID(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/proxy/api/config", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: "",
	})

	rr := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Next handler should not be called")
	})

	middleware := AuthMiddleware(nextHandler, store)
	middleware.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rr.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !strings.Contains(resp.Error, "empty session ID") {
		t.Errorf("Expected error message about empty session ID, got: %s", resp.Error)
	}
}

func TestAuthMiddleware_InvalidSessionID(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/proxy/api/config", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: "invalid-session-id",
	})

	rr := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Next handler should not be called")
	})

	middleware := AuthMiddleware(nextHandler, store)
	middleware.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rr.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !strings.Contains(resp.Error, "invalid or expired session") {
		t.Errorf("Expected error message about invalid session, got: %s", resp.Error)
	}
}

func TestAuthMiddleware_ExpiredSession(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	session, err := store.Create("test-token", -1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest("GET", "/proxy/api/config", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: session.ID,
	})

	rr := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Next handler should not be called")
	})

	middleware := AuthMiddleware(nextHandler, store)
	middleware.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rr.Code)
	}
}

func TestLoginHandler_Success(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	botAPIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer valid-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"config": "data"}`))
	}))
	defer botAPIServer.Close()

	loginReq := LoginRequest{Token: "Bearer valid-token"}
	body, _ := json.Marshal(loginReq)

	req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := LoginHandler(store, botAPIServer.URL, false, 0)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != sessionCookieName {
		t.Errorf("Expected cookie name '%s', got '%s'", sessionCookieName, cookie.Name)
	}

	if cookie.Value == "" {
		t.Error("Expected non-empty cookie value")
	}

	if !cookie.HttpOnly {
		t.Error("Expected HttpOnly to be true")
	}

	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("Expected SameSiteStrictMode, got %v", cookie.SameSite)
	}

	if cookie.Path != "/" {
		t.Errorf("Expected Path '/', got '%s'", cookie.Path)
	}

	var resp LoginResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Message != "Login successful" {
		t.Errorf("Expected message 'Login successful', got '%s'", resp.Message)
	}

	// Verify session was created and token can be retrieved
	token, err := store.GetToken(cookie.Value)
	if err != nil {
		t.Fatalf("GetToken() error = %v", err)
	}
	if token != "Bearer valid-token" {
		t.Errorf("Expected token 'Bearer valid-token', got '%s'", token)
	}
}

func TestLoginHandler_InvalidBearerToken(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	botAPIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer botAPIServer.Close()

	loginReq := LoginRequest{Token: "Bearer invalid-token"}
	body, _ := json.Marshal(loginReq)

	req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := LoginHandler(store, botAPIServer.URL, false, 0)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rr.Code)
	}

	cookies := rr.Result().Cookies()
	if len(cookies) != 0 {
		t.Errorf("Expected no cookies, got %d", len(cookies))
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !strings.Contains(resp.Error, "invalid bearer token") {
		t.Errorf("Expected error message about invalid token, got: %s", resp.Error)
	}
}

func TestLoginHandler_MissingBearerPrefix(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	loginReq := LoginRequest{Token: "invalid-token-without-bearer"}
	body, _ := json.Marshal(loginReq)

	req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := LoginHandler(store, "http://localhost:3001", false, 0)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !strings.Contains(resp.Error, "Bearer prefix") {
		t.Errorf("Expected error message about Bearer prefix, got: %s", resp.Error)
	}
}

func TestLoginHandler_EmptyToken(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	loginReq := LoginRequest{Token: ""}
	body, _ := json.Marshal(loginReq)

	req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := LoginHandler(store, "http://localhost:3001", false, 0)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !strings.Contains(resp.Error, "bearer token is required") {
		t.Errorf("Expected error message about required token, got: %s", resp.Error)
	}
}

func TestLoginHandler_InvalidRequestBody(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/proxy/login", strings.NewReader("invalid json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := LoginHandler(store, "http://localhost:3001", false, 0)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", rr.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !strings.Contains(resp.Error, "invalid request body") {
		t.Errorf("Expected error message about invalid body, got: %s", resp.Error)
	}
}

func TestLoginHandler_WrongMethod(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/proxy/login", nil)
	rr := httptest.NewRecorder()

	handler := LoginHandler(store, "http://localhost:3001", false, 0)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rr.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !strings.Contains(resp.Error, "method not allowed") {
		t.Errorf("Expected error message about method, got: %s", resp.Error)
	}
}

func TestLogoutHandler_Success(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	session, err := store.Create("test-token", 0)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest("POST", "/proxy/logout", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: session.ID,
	})

	rr := httptest.NewRecorder()

	handler := LogoutHandler(store, false)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	_, err = store.Get(session.ID)
	if err == nil {
		t.Error("Session still exists after logout")
	}

	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != sessionCookieName {
		t.Errorf("Expected cookie name '%s', got '%s'", sessionCookieName, cookie.Name)
	}

	if cookie.MaxAge != -1 {
		t.Errorf("Expected MaxAge -1, got %d", cookie.MaxAge)
	}

	var resp LoginResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Message != "Logout successful" {
		t.Errorf("Expected message 'Logout successful', got '%s'", resp.Message)
	}
}

func TestLogoutHandler_NoCookie(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/proxy/logout", nil)
	rr := httptest.NewRecorder()

	handler := LogoutHandler(store, false)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.MaxAge != -1 {
		t.Errorf("Expected MaxAge -1, got %d", cookie.MaxAge)
	}
}

func TestLogoutHandler_WrongMethod(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/proxy/logout", nil)
	rr := httptest.NewRecorder()

	handler := LogoutHandler(store, false)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", rr.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !strings.Contains(resp.Error, "method not allowed") {
		t.Errorf("Expected error message about method, got: %s", resp.Error)
	}
}

func TestGetSession(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	session, err := store.Create("test-token", 0)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest("GET", "/proxy/api/config", nil)
	ctx := context.WithValue(req.Context(), SessionContextKey, session)
	req = req.WithContext(ctx)

	retrievedSession, ok := GetSession(req)
	if !ok {
		t.Error("GetSession returned false")
	}

	if retrievedSession == nil {
		t.Fatal("Retrieved session is nil")
	}

	if retrievedSession.ID != session.ID {
		t.Errorf("Expected session ID '%s', got '%s'", session.ID, retrievedSession.ID)
	}

	// Verify both decrypt to the same token using GetToken()
	retrievedToken, err := store.GetToken(retrievedSession.ID)
	if err != nil {
		t.Fatalf("GetToken() for retrieved session error = %v", err)
	}
	originalToken, err := store.GetToken(session.ID)
	if err != nil {
		t.Fatalf("GetToken() for original session error = %v", err)
	}
	if retrievedToken != originalToken {
		t.Errorf("Expected token '%s', got '%s'", originalToken, retrievedToken)
	}
}

func TestGetSession_NotFound(t *testing.T) {
	req := httptest.NewRequest("GET", "/proxy/api/config", nil)

	session, ok := GetSession(req)
	if ok {
		t.Error("GetSession should return false when session not found")
	}

	if session != nil {
		t.Error("Session should be nil when not found")
	}
}

func TestSetSessionCookie(t *testing.T) {
	rr := httptest.NewRecorder()

	SetSessionCookie(rr, "test-session-id", false)

	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != sessionCookieName {
		t.Errorf("Expected cookie name '%s', got '%s'", sessionCookieName, cookie.Name)
	}

	if cookie.Value != "test-session-id" {
		t.Errorf("Expected value 'test-session-id', got '%s'", cookie.Value)
	}

	if !cookie.HttpOnly {
		t.Error("Expected HttpOnly to be true")
	}

	if cookie.SameSite != http.SameSiteStrictMode {
		t.Errorf("Expected SameSiteStrictMode, got %v", cookie.SameSite)
	}

	if cookie.Path != "/" {
		t.Errorf("Expected Path '/', got '%s'", cookie.Path)
	}

	if cookie.MaxAge != int((4 * time.Hour).Seconds()) {
		t.Errorf("Expected MaxAge %d, got %d", int((4 * time.Hour).Seconds()), cookie.MaxAge)
	}
}

func TestClearSessionCookie(t *testing.T) {
	rr := httptest.NewRecorder()

	ClearSessionCookie(rr, false)

	cookies := rr.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("Expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != sessionCookieName {
		t.Errorf("Expected cookie name '%s', got '%s'", sessionCookieName, cookie.Name)
	}

	if cookie.Value != "" {
		t.Errorf("Expected empty value, got '%s'", cookie.Value)
	}

	if cookie.MaxAge != -1 {
		t.Errorf("Expected MaxAge -1, got %d", cookie.MaxAge)
	}
}

func TestConcurrentLoginAttempts(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	botAPIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "Bearer valid-token" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer botAPIServer.Close()

	const numConcurrent = 10
	results := make(chan int, numConcurrent)

	for i := 0; i < numConcurrent; i++ {
		go func() {
			loginReq := LoginRequest{Token: "Bearer valid-token"}
			body, _ := json.Marshal(loginReq)

			req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rr := httptest.NewRecorder()

			handler := LoginHandler(store, botAPIServer.URL, false, 0)
			handler.ServeHTTP(rr, req)

			results <- rr.Code
		}()
	}

	successCount := 0
	for i := 0; i < numConcurrent; i++ {
		code := <-results
		if code == http.StatusOK {
			successCount++
		}
	}

	if successCount != numConcurrent {
		t.Errorf("Expected %d successful logins, got %d", numConcurrent, successCount)
	}
}

func createTestSessionStore(t *testing.T) (*SessionStore, func()) {
	t.Helper()

	tmpDir := filepath.Join(os.TempDir(), "sessions-test-"+t.Name())
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		t.Fatalf("Failed to create test sessions directory: %v", err)
	}

	// Create encryption key file in test directory
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
	keyFile := filepath.Join(tmpDir, ".session_key")
	if err := os.WriteFile(keyFile, []byte(key), 0600); err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to write test encryption key: %v", err)
	}
	t.Setenv("SESSION_KEY_FILE", keyFile)

	store, err := NewSessionStore(tmpDir)
	if err != nil {
		os.RemoveAll(tmpDir)
		t.Fatalf("Failed to create session store: %v", err)
	}

	cleanup := func() {
		store.StopBackgroundCleanup()
		os.RemoveAll(tmpDir)
	}

	return store, cleanup
}

func TestSetSessionCookie_HTTPS(t *testing.T) {
	tests := []struct {
		name     string
		useHTTPS bool
		secure   bool
	}{
		{"PROXY_HTTPS=true sets Secure flag", true, true},
		{"PROXY_HTTPS=false clears Secure flag", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()

			SetSessionCookie(rr, "test-session-id", tt.useHTTPS)

			cookies := rr.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("Expected 1 cookie, got %d", len(cookies))
			}

			cookie := cookies[0]
			if cookie.Secure != tt.secure {
				t.Errorf("Expected Secure=%v, got Secure=%v", tt.secure, cookie.Secure)
			}
		})
	}
}

func TestClearSessionCookie_HTTPS(t *testing.T) {
	tests := []struct {
		name     string
		useHTTPS bool
		secure   bool
	}{
		{"PROXY_HTTPS=true sets Secure flag", true, true},
		{"PROXY_HTTPS=false clears Secure flag", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()

			ClearSessionCookie(rr, tt.useHTTPS)

			cookies := rr.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("Expected 1 cookie, got %d", len(cookies))
			}

			cookie := cookies[0]
			if cookie.Secure != tt.secure {
				t.Errorf("Expected Secure=%v, got Secure=%v", tt.secure, cookie.Secure)
			}
		})
	}
}

func TestValidateBearerTokenWithBotAPI_Timeout(t *testing.T) {
	tests := []struct {
		name           string
		timeout        time.Duration
		expectedStatus int
		description    string
	}{
		{
			name:           "PROXY_UPSTREAM_TIMEOUT=30s uses 30 second timeout",
			timeout:        30 * time.Second,
			expectedStatus: http.StatusOK,
			description:    "should complete within 30s",
		},
		{
			name:           "PROXY_UPSTREAM_TIMEOUT unset defaults to 10s",
			timeout:        0,
			expectedStatus: http.StatusOK,
			description:    "should use default 10s timeout",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				auth := r.Header.Get("Authorization")
				if auth != "Bearer test-token" {
					w.WriteHeader(http.StatusUnauthorized)
					return
				}
				w.WriteHeader(http.StatusOK)
				w.Write([]byte(`{"config": "data"}`))
			}))
			defer server.Close()

			err := validateBearerTokenWithBotAPI("Bearer test-token", server.URL, tt.timeout)

			if tt.expectedStatus == http.StatusOK && err != nil {
				t.Errorf("Expected success, got error: %v", err)
			}
			if tt.expectedStatus != http.StatusOK && err == nil {
				t.Error("Expected error, got nil")
			}
		})
	}
}

func TestRateLimit_FiveFailedAttemptsAllowed_SixthReturns429(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	botAPIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer botAPIServer.Close()

	testIP := "192.168.1.100:12345"

	for i := 0; i < 5; i++ {
		loginReq := LoginRequest{Token: "Bearer invalid-token"}
		body, _ := json.Marshal(loginReq)

		req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
		req.RemoteAddr = testIP
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler := LoginHandler(store, botAPIServer.URL, false, 0)
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Attempt %d: Expected status 401, got %d", i+1, rr.Code)
		}
	}

	loginReq := LoginRequest{Token: "Bearer invalid-token"}
	body, _ := json.Marshal(loginReq)

	req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
	req.RemoteAddr = testIP
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := LoginHandler(store, botAPIServer.URL, false, 0)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("Expected status 429 on 6th attempt, got %d", rr.Code)
	}

	retryAfter := rr.Header().Get("Retry-After")
	if retryAfter != "60" {
		t.Errorf("Expected Retry-After header '60', got '%s'", retryAfter)
	}
}

func TestRateLimit_SuccessfulLoginResetsCounter(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	botAPIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth == "Bearer valid-token" {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))
	defer botAPIServer.Close()

	testIP := "192.168.1.101:12345"

	for i := 0; i < 4; i++ {
		loginReq := LoginRequest{Token: "Bearer invalid-token"}
		body, _ := json.Marshal(loginReq)

		req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
		req.RemoteAddr = testIP
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler := LoginHandler(store, botAPIServer.URL, false, 0)
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Failed attempt %d: Expected status 401, got %d", i+1, rr.Code)
		}
	}

	loginReq := LoginRequest{Token: "Bearer valid-token"}
	body, _ := json.Marshal(loginReq)

	req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
	req.RemoteAddr = testIP
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := LoginHandler(store, botAPIServer.URL, false, 0)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Successful login failed: Expected status 200, got %d", rr.Code)
	}

	for i := 0; i < 5; i++ {
		loginReq := LoginRequest{Token: "Bearer invalid-token"}
		body, _ := json.Marshal(loginReq)

		req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
		req.RemoteAddr = testIP
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler := LoginHandler(store, botAPIServer.URL, false, 0)
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("After reset, attempt %d: Expected status 401, got %d", i+1, rr.Code)
		}
	}

	loginReq = LoginRequest{Token: "Bearer invalid-token"}
	body, _ = json.Marshal(loginReq)

	req = httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
	req.RemoteAddr = testIP
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()

	handler = LoginHandler(store, botAPIServer.URL, false, 0)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("After reset, 6th attempt should return 429, got %d", rr.Code)
	}
}

func TestRateLimit_CounterExpiresAfter60Seconds(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	botAPIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer botAPIServer.Close()

	testIP := "192.168.1.102:12345"

	limiter := newRateLimiter()
	limiter.windowDuration = 100 * time.Millisecond

	for i := 0; i < 5; i++ {
		loginReq := LoginRequest{Token: "Bearer invalid-token"}
		body, _ := json.Marshal(loginReq)

		req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
		req.RemoteAddr = testIP
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		oldLimiter := loginRateLimiter
		loginRateLimiter = limiter
		handler := LoginHandler(store, botAPIServer.URL, false, 0)
		handler.ServeHTTP(rr, req)
		loginRateLimiter = oldLimiter

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Attempt %d: Expected status 401, got %d", i+1, rr.Code)
		}
	}

	time.Sleep(150 * time.Millisecond)

	loginReq := LoginRequest{Token: "Bearer invalid-token"}
	body, _ := json.Marshal(loginReq)

	req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
	req.RemoteAddr = testIP
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	oldLimiter := loginRateLimiter
	loginRateLimiter = limiter
	handler := LoginHandler(store, botAPIServer.URL, false, 0)
	handler.ServeHTTP(rr, req)
	loginRateLimiter = oldLimiter

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("After expiration, should allow request: Expected status 401, got %d", rr.Code)
	}
}

func TestRateLimit_DifferentIPsTrackedIndependently(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	botAPIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer botAPIServer.Close()

	ip1 := "192.168.1.103:12345"
	ip2 := "192.168.1.104:12345"

	for i := 0; i < 6; i++ {
		loginReq := LoginRequest{Token: "Bearer invalid-token"}
		body, _ := json.Marshal(loginReq)

		req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
		req.RemoteAddr = ip1
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler := LoginHandler(store, botAPIServer.URL, false, 0)
		handler.ServeHTTP(rr, req)

		if i < 5 && rr.Code != http.StatusUnauthorized {
			t.Errorf("IP1 attempt %d: Expected status 401, got %d", i+1, rr.Code)
		}
		if i == 5 && rr.Code != http.StatusTooManyRequests {
			t.Errorf("IP1 attempt 6: Expected status 429, got %d", rr.Code)
		}
	}

	loginReq := LoginRequest{Token: "Bearer invalid-token"}
	body, _ := json.Marshal(loginReq)

	req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
	req.RemoteAddr = ip2
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := LoginHandler(store, botAPIServer.URL, false, 0)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("IP2 first attempt: Expected status 401 (independent tracking), got %d", rr.Code)
	}
}

func TestRateLimit_XForwardedFor_IsUsed(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	botAPIServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer botAPIServer.Close()

	testIP := "192.168.1.105:12345"
	forwardedIP := "10.0.0.1"

	// Make 5 failed login attempts with X-Forwarded-For header
	for i := 0; i < 5; i++ {
		loginReq := LoginRequest{Token: "Bearer invalid-token"}
		body, _ := json.Marshal(loginReq)

		req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
		req.RemoteAddr = testIP
		req.Header.Set("X-Forwarded-For", forwardedIP)
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()

		handler := LoginHandler(store, botAPIServer.URL, false, 0)
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("Attempt %d: Expected status 401, got %d", i+1, rr.Code)
		}
	}

	// 6th attempt with same X-Forwarded-For should be rate limited
	loginReq := LoginRequest{Token: "Bearer invalid-token"}
	body, _ := json.Marshal(loginReq)

	req := httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
	req.RemoteAddr = "192.168.1.106:99999"  // Different RemoteAddr
	req.Header.Set("X-Forwarded-For", forwardedIP)  // Same X-Forwarded-For
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler := LoginHandler(store, botAPIServer.URL, false, 0)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("Expected rate limit to apply to X-Forwarded-For: got status %d", rr.Code)
	}

	// Request with different RemoteAddr and NO X-Forwarded-For should be allowed
	loginReq = LoginRequest{Token: "Bearer invalid-token"}
	body, _ = json.Marshal(loginReq)

	req = httptest.NewRequest("POST", "/proxy/login", bytes.NewReader(body))
	req.RemoteAddr = "192.168.1.107:12345"  // Different RemoteAddr, no X-Forwarded-For
	req.Header.Set("Content-Type", "application/json")
	rr = httptest.NewRecorder()

	handler = LoginHandler(store, botAPIServer.URL, false, 0)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Request without X-Forwarded-For should be rate limited by RemoteAddr: got status %d", rr.Code)
	}
}

func TestCSRFMiddleware_ValidToken_AllowsPOST(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	session, err := store.Create("test-token", 0)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest("POST", "/proxy/api/config", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: session.ID,
	})
	req.Header.Set("X-CSRF-Token", session.CSRFToken)

	rr := httptest.NewRecorder()

	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		session, ok := GetSession(r)
		if !ok {
			t.Error("Session not found in context")
		}
		if session == nil {
			t.Error("Session is nil")
		}
		token, err := store.GetToken(session.ID)
		if err != nil {
			t.Errorf("GetToken() error = %v", err)
		}
		if token != "test-token" {
			t.Errorf("Expected token 'test-token', got '%s'", token)
		}
	})

	middleware := CSRFMiddleware(nextHandler, store)
	middleware.ServeHTTP(rr, req)

	if !nextCalled {
		t.Error("Next handler was not called")
	}

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestCSRFMiddleware_MissingToken_Returns403(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	session, err := store.Create("test-token", 0)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest("POST", "/proxy/api/config", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: session.ID,
	})

	rr := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Next handler should not be called")
	})

	middleware := CSRFMiddleware(nextHandler, store)
	middleware.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rr.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !strings.Contains(resp.Error, "CSRF token required") {
		t.Errorf("Expected error message about CSRF token, got: %s", resp.Error)
	}
}

func TestCSRFMiddleware_MismatchedToken_Returns403(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	session, err := store.Create("test-token", 0)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest("POST", "/proxy/api/config", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: session.ID,
	})
	req.Header.Set("X-CSRF-Token", "wrong-token")

	rr := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Next handler should not be called")
	})

	middleware := CSRFMiddleware(nextHandler, store)
	middleware.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", rr.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !strings.Contains(resp.Error, "CSRF token mismatch") {
		t.Errorf("Expected error message about CSRF mismatch, got: %s", resp.Error)
	}
}

func TestCSRFMiddleware_GETRequest_BypassesCSRF(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	session, err := store.Create("test-token", 0)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest("GET", "/proxy/api/config", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: session.ID,
	})

	rr := httptest.NewRecorder()

	nextCalled := false
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		session, ok := GetSession(r)
		if !ok {
			t.Error("Session not found in context")
		}
		if session == nil {
			t.Error("Session is nil")
		}
	})

	middleware := CSRFMiddleware(nextHandler, store)
	middleware.ServeHTTP(rr, req)

	if !nextCalled {
		t.Error("Next handler was not called for GET request")
	}

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}
}

func TestCSRFMiddleware_EmptySessionCookie_Returns401(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	req := httptest.NewRequest("POST", "/proxy/api/config", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: "",
	})
	req.Header.Set("X-CSRF-Token", "")

	rr := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Next handler should not be called")
	})

	middleware := CSRFMiddleware(nextHandler, store)
	middleware.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rr.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !strings.Contains(resp.Error, "empty session ID") {
		t.Errorf("Expected error message about empty session, got: %s", resp.Error)
	}
}

func TestCSRFMiddleware_ExpiredSession_Returns401(t *testing.T) {
	store, cleanup := createTestSessionStore(t)
	defer cleanup()

	session, err := store.Create("test-token", -1*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	req := httptest.NewRequest("POST", "/proxy/api/config", nil)
	req.AddCookie(&http.Cookie{
		Name:  sessionCookieName,
		Value: session.ID,
	})
	req.Header.Set("X-CSRF-Token", session.CSRFToken)

	rr := httptest.NewRecorder()

	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("Next handler should not be called")
	})

	middleware := CSRFMiddleware(nextHandler, store)
	middleware.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", rr.Code)
	}

	var resp ErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !strings.Contains(resp.Error, "invalid or expired session") {
		t.Errorf("Expected error message about expired session, got: %s", resp.Error)
	}
}

