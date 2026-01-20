package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// BenchmarkBearerAuth measures timing-safe comparison overhead
func BenchmarkBearerAuth(b *testing.B) {
	token := "valid-bearer-token-12345678"
	authMiddleware := BearerAuth(token)

	handler := authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/config", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

// BenchmarkRateLimit measures rate limiter performance
func BenchmarkRateLimit(b *testing.B) {
	rateLimit := RateLimit(100, 100)
	handler := rateLimit(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/config", nil)
	req.RemoteAddr = "192.168.1.1:12345"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
	}
}

// BenchmarkJSONEncode measures JSON encoding performance
func BenchmarkJSONEncode(b *testing.B) {
	data := map[string]interface{}{
		"server_ip":       "192.168.1.1",
		"update_interval": 60,
		"category_order":  []string{"GT3", "GT4", "GT2"},
		"category_emojis": map[string]string{"GT3": "🏎️", "GT4": "🏁", "GT2": "🚗"},
		"servers":         []interface{}{},
	}

	// Add 100 servers
	servers := make([]map[string]interface{}, 100)
	for i := 0; i < 100; i++ {
		servers[i] = map[string]interface{}{
			"name":     "server-test",
			"ip":       "192.168.1.1",
			"port":     3001 + i,
			"category": "GT3",
		}
	}
	data["servers"] = servers

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rr := httptest.NewRecorder()
		WriteJSON(rr, http.StatusOK, data)
	}
}

// BenchmarkJSONDecode measures JSON decoding performance
func BenchmarkJSONDecode(b *testing.B) {
	// Create 100-server config
	servers := make([]map[string]interface{}, 100)
	for i := 0; i < 100; i++ {
		servers[i] = map[string]interface{}{
			"name":     "server-test",
			"ip":       "192.168.1.1",
			"port":     3001 + i,
			"category": "GT3",
		}
	}
	data := map[string]interface{}{
		"server_ip":       "192.168.1.1",
		"update_interval": 60,
		"category_order":  []string{"GT3", "GT4", "GT2"},
		"category_emojis": map[string]string{"GT3": "🏎️", "GT4": "🏁", "GT2": "🚗"},
		"servers":         servers,
	}

	jsonData, _ := json.Marshal(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var result map[string]interface{}
		json.Unmarshal(jsonData, &result)
	}
}
