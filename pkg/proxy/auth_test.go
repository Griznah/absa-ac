package proxy

import (
	"bytes"
	"context"
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
		if session.Token != "test-token" {
			t.Errorf("Expected token 'test-token', got '%s'", session.Token)
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

	handler := LoginHandler(store, botAPIServer.URL)
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

	if cookie.Path != "/proxy" {
		t.Errorf("Expected Path '/proxy', got '%s'", cookie.Path)
	}

	var resp LoginResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if resp.Message != "Login successful" {
		t.Errorf("Expected message 'Login successful', got '%s'", resp.Message)
	}

	session, exists := store.Get(cookie.Value)
	if !exists {
		t.Error("Session not found in store")
	}

	if session.Token != "Bearer valid-token" {
		t.Errorf("Expected token 'Bearer valid-token', got '%s'", session.Token)
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

	handler := LoginHandler(store, botAPIServer.URL)
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

	handler := LoginHandler(store, "http://localhost:8080")
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

	handler := LoginHandler(store, "http://localhost:8080")
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

	handler := LoginHandler(store, "http://localhost:8080")
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

	handler := LoginHandler(store, "http://localhost:8080")
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

	handler := LogoutHandler(store)
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rr.Code)
	}

	_, exists := store.Get(session.ID)
	if exists {
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

	handler := LogoutHandler(store)
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

	handler := LogoutHandler(store)
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

	if retrievedSession.Token != session.Token {
		t.Errorf("Expected token '%s', got '%s'", session.Token, retrievedSession.Token)
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

	SetSessionCookie(rr, "test-session-id")

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

	if cookie.Path != "/proxy" {
		t.Errorf("Expected Path '/proxy', got '%s'", cookie.Path)
	}

	if cookie.MaxAge != int((4 * time.Hour).Seconds()) {
		t.Errorf("Expected MaxAge %d, got %d", int((4 * time.Hour).Seconds()), cookie.MaxAge)
	}
}

func TestClearSessionCookie(t *testing.T) {
	rr := httptest.NewRecorder()

	ClearSessionCookie(rr)

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

			handler := LoginHandler(store, botAPIServer.URL)
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
