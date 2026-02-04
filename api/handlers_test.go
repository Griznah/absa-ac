package api

import (
	"encoding/json"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// mockConfigManagerWithWrites is a test double that supports write operations
type mockConfigManagerWithWrites struct {
	config    any
	writeErr  error
	updateErr error
}

func (m *mockConfigManagerWithWrites) GetConfigAny() any {
	return m.config
}

func (m *mockConfigManagerWithWrites) WriteConfigAny(cfg any) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	m.config = cfg
	return nil
}

func (m *mockConfigManagerWithWrites) UpdateConfig(partial map[string]interface{}) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	// Merge partial into config
	if m.config == nil {
		m.config = make(map[string]interface{})
	}
	cfgMap, ok := m.config.(map[string]interface{})
	if !ok {
		// Convert to map
		data, _ := json.Marshal(m.config)
		json.Unmarshal(data, &cfgMap)
	}
	for k, v := range partial {
		cfgMap[k] = v
	}
	m.config = cfgMap
	return nil
}

func TestHandlers_GetConfig(t *testing.T) {
	tests := []struct {
		name       string
		config     any
		wantStatus int
	}{
		{
			name: "Normal: Returns current config",
			config: map[string]interface{}{
				"server_ip": "192.168.1.1",
				"update_interval": 60,
				"servers": []map[string]interface{}{
					{"name": "Server1", "port": 8081},
				},
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &mockConfigManagerWithWrites{config: tt.config}
			s := NewServer(cm, "18080", "test-token", []string{}, []string{}, log.New(os.Stdout, "TEST: ", log.LstdFlags))

			req := httptest.NewRequest("GET", "/api/config", nil)
			rec := httptest.NewRecorder()

			s.GetConfig(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rec.Code, tt.wantStatus)
			}

			// Verify response contains expected config
			var gotConfig map[string]interface{}
			if err := json.NewDecoder(rec.Body).Decode(&gotConfig); err != nil {
				t.Errorf("Failed to decode response: %v", err)
				return
			}

			if gotConfig["server_ip"] != tt.config.(map[string]interface{})["server_ip"] {
				t.Errorf("server_ip = %v, want %v", gotConfig["server_ip"], tt.config.(map[string]interface{})["server_ip"])
			}
		})
	}
}

func TestHandlers_PatchConfig(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "Normal: Partial update merges changes",
			body:       `{"update_interval": 120}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "Edge: Empty body returns 400",
			body:       "",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "Edge: Invalid JSON returns 400",
			body:       `{invalid json}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &mockConfigManagerWithWrites{
				config: map[string]interface{}{
					"server_ip": "192.168.1.1",
					"update_interval": 60,
				},
			}
			s := NewServer(cm, "18080", "test-token", []string{}, []string{}, log.New(os.Stdout, "TEST: ", log.LstdFlags))

			var body string
			if tt.body != "" {
				body = tt.body
			}

			req := httptest.NewRequest("PATCH", "/api/config", strings.NewReader(body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()

			s.PatchConfig(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandlers_PutConfig(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name: "Normal: Full config replacement",
			body: `{"server_ip":"10.0.0.1","update_interval":30,"category_order":["Race"],"category_emojis":{"Race":"🏎️"},"servers":[]}`,
			wantStatus: http.StatusOK,
		},
		{
			name:       "Edge: Empty body returns 400",
			body:       "",
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &mockConfigManagerWithWrites{
				config: map[string]interface{}{"server_ip": "192.168.1.1"},
			}
			s := NewServer(cm, "18080", "test-token", []string{}, []string{}, log.New(os.Stdout, "TEST: ", log.LstdFlags))

			var body string
			if tt.body != "" {
				body = tt.body
			}

			req := httptest.NewRequest("PUT", "/api/config", strings.NewReader(body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()

			s.PutConfig(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandlers_ValidateConfig(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{
			name:       "Normal: Valid JSON returns 501 (Not Implemented)",
			body:       `{"server_ip":"10.0.0.1"}`,
			wantStatus: http.StatusNotImplemented, // 501 - full validation not available
		},
		{
			name:       "Edge: Invalid JSON returns 400",
			body:       `{invalid}`,
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &mockConfigManagerWithWrites{}
			s := NewServer(cm, "18080", "test-token", []string{}, []string{}, log.New(os.Stdout, "TEST: ", log.LstdFlags))

			var body string
			if tt.body != "" {
				body = tt.body
			}

			req := httptest.NewRequest("POST", "/api/config/validate", strings.NewReader(body))
			if tt.body != "" {
				req.Header.Set("Content-Type", "application/json")
			}
			rec := httptest.NewRecorder()

			s.ValidateConfig(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rec.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandlers_GetServers(t *testing.T) {
	tests := []struct {
		name       string
		config     any
		wantStatus int
	}{
		{
			name: "Normal: Returns servers array",
			config: map[string]interface{}{
				"servers": []map[string]interface{}{
					{"name": "Server1", "port": 8081},
					{"name": "Server2", "port": 8082},
				},
			},
			wantStatus: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := &mockConfigManagerWithWrites{config: tt.config}
			s := NewServer(cm, "18080", "test-token", []string{}, []string{}, log.New(os.Stdout, "TEST: ", log.LstdFlags))

			req := httptest.NewRequest("GET", "/api/config/servers", nil)
			rec := httptest.NewRecorder()

			s.GetServers(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", rec.Code, tt.wantStatus)
			}

			// Verify response contains servers
			if tt.wantStatus == http.StatusOK {
				var response []interface{}
				if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
					t.Errorf("Failed to parse response: %v", err)
				}
			}
		})
	}
}
