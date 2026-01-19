package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProxyHandler_Normal_GET(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if r.URL.Path != "/api/config" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		query := r.URL.Query().Get("test")
		if query != "value" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer upstream.Close()

	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}
	defer store.StopBackgroundCleanup()

	session, err := store.Create("test-token", 4*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	proxyHandler := AuthMiddleware(ProxyHandler(upstream.URL, store), store)

	req := httptest.NewRequest("GET", "/api/config?test=value", nil)
	req.Header.Set("Cookie", "proxy_session="+session.ID)

	w := httptest.NewRecorder()
	proxyHandler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var data map[string]string
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if data["status"] != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", data["status"])
	}
}

func TestProxyHandler_Normal_POST(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer test-token" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}

		if r.Method != "POST" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		if r.URL.Path != "/api/config" {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		var data map[string]string
		if err := json.NewDecoder(r.Body).Decode(&data); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if data["key"] != "value" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(map[string]string{"result": "created"})
	}))
	defer upstream.Close()

	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}
	defer store.StopBackgroundCleanup()

	session, err := store.Create("test-token", 4*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	proxyHandler := AuthMiddleware(ProxyHandler(upstream.URL, store), store)

	body := `{"key":"value"}`
	req := httptest.NewRequest("POST", "/api/config", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Cookie", "proxy_session="+session.ID)

	w := httptest.NewRecorder()
	proxyHandler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected status 201, got %d", resp.StatusCode)
	}

	respBody, _ := io.ReadAll(resp.Body)
	var data map[string]string
	if err := json.Unmarshal(respBody, &data); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if data["result"] != "created" {
		t.Errorf("Expected result 'created', got '%s'", data["result"])
	}
}

func TestProxyHandler_Edge_LargeRequestBody(t *testing.T) {
	largeBody := strings.Repeat("a", 1024*1024)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}

		if len(body) != len(largeBody) {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}
	defer store.StopBackgroundCleanup()

	session, err := store.Create("test-token", 4*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	proxyHandler := AuthMiddleware(ProxyHandler(upstream.URL, store), store)

	req := httptest.NewRequest("POST", "/api/config", strings.NewReader(largeBody))
	req.Header.Set("Cookie", "proxy_session="+session.ID)

	w := httptest.NewRecorder()
	proxyHandler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestProxyHandler_Edge_SpecialCharactersInQuery(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("key") != "value with spaces" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		if query.Get("emoji") != "😀🚀" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}
	defer store.StopBackgroundCleanup()

	session, err := store.Create("test-token", 4*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	proxyHandler := AuthMiddleware(ProxyHandler(upstream.URL, store), store)

	req := httptest.NewRequest("GET", "/api/config?key=value+with+spaces&emoji=😀🚀", nil)
	req.Header.Set("Cookie", "proxy_session="+session.ID)

	w := httptest.NewRecorder()
	proxyHandler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestProxyHandler_Error_UpstreamUnreachable(t *testing.T) {
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}
	defer store.StopBackgroundCleanup()

	session, err := store.Create("test-token", 4*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	proxyHandler := AuthMiddleware(ProxyHandler("http://localhost:9999", store), store)

	req := httptest.NewRequest("GET", "/api/config", nil)
	req.Header.Set("Cookie", "proxy_session="+session.ID)

	w := httptest.NewRecorder()
	proxyHandler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Service unavailable") {
		t.Errorf("Expected error message in body, got: %s", string(body))
	}
}

func TestProxyHandler_Error_NoSession(t *testing.T) {
	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}
	defer store.StopBackgroundCleanup()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	proxyHandler := AuthMiddleware(ProxyHandler(upstream.URL, store), store)

	req := httptest.NewRequest("GET", "/api/config", nil)

	w := httptest.NewRecorder()
	proxyHandler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", resp.StatusCode)
	}
}

func TestProxyHandler_Error_UpstreamReturnsError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "Access denied"})
	}))
	defer upstream.Close()

	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}
	defer store.StopBackgroundCleanup()

	session, err := store.Create("test-token", 4*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	proxyHandler := AuthMiddleware(ProxyHandler(upstream.URL, store), store)

	req := httptest.NewRequest("GET", "/api/config", nil)
	req.Header.Set("Cookie", "proxy_session="+session.ID)

	w := httptest.NewRecorder()
	proxyHandler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("Expected status 403, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var data map[string]string
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if data["error"] != "Access denied" {
		t.Errorf("Expected error 'Access denied', got '%s'", data["error"])
	}
}

func TestProxyHandler_PathPreserved(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/config/servers" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	store, err := NewSessionStore(t.TempDir())
	if err != nil {
		t.Fatalf("Failed to create session store: %v", err)
	}
	defer store.StopBackgroundCleanup()

	session, err := store.Create("test-token", 4*time.Hour)
	if err != nil {
		t.Fatalf("Failed to create session: %v", err)
	}

	proxyHandler := AuthMiddleware(ProxyHandler(upstream.URL, store), store)

	req := httptest.NewRequest("GET", "/api/config/servers", nil)
	req.Header.Set("Cookie", "proxy_session="+session.ID)

	w := httptest.NewRecorder()
	proxyHandler.ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}
