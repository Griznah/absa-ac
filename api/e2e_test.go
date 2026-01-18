package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// generateConfig creates deterministic config data for testing
// numServers specifies how many servers to generate
func generateConfig(numServers int) map[string]interface{} {
	servers := make([]map[string]interface{}, numServers)
	categories := []string{"Race", "Drift", "Time Attack"}
	emojis := map[string]string{"Race": "🏎️", "Drift": "🏁", "Time Attack": "⏱️"}

	for i := 0; i < numServers; i++ {
		category := categories[i%len(categories)]
		servers[i] = map[string]interface{}{
			"name":     fmt.Sprintf("Server%d", i+1),
			"ip":       "192.168.1.100",
			"port":     8080 + i,
			"category": category,
		}
	}

	return map[string]interface{}{
		"server_ip":       "192.168.1.100",
		"update_interval": 30,
		"category_order":  categories,
			"category_emojis": emojis,
		"servers":         servers,
	}
}

// generateUnicodeConfig creates config with unicode strings for testing
func generateUnicodeConfig() map[string]interface{} {
	return map[string]interface{}{
		"server_ip":       "192.168.1.100",
		"update_interval": 60,
		"category_order":  []string{"漂移", "レース", "경주"},
		"category_emojis": map[string]string{
			"漂移":  "🏁",
			"レース": "🏎️",
			"경주":  "🏁",
		},
		"servers": []map[string]interface{}{
			{
				"name":     "東京サーバー",
				"ip":       "10.0.0.1",
				"port":     8081,
				"category": "レース",
			},
			{
				"name":     "서울서버",
				"ip":       "10.0.0.2",
				"port":     8082,
				"category": "경주",
			},
		},
	}
}

// setupTestEnvironment creates a test environment with temp config, HTTP server, and client
func setupTestEnvironment(t *testing.T, initialConfig map[string]interface{}) (*http.Client, string, func()) {
	t.Helper()

	// Create temp directory for config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Write initial config
	data, err := json.MarshalIndent(initialConfig, "", "  ")
	if err != nil {
		t.Fatalf("Failed to marshal initial config: %v", err)
	}
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		t.Fatalf("Failed to write initial config: %v", err)
	}

	// Create a mock config manager that reads/writes the file
	cm := &e2eConfigManager{
		configPath: configPath,
	}

	// Start HTTP server
	port := "19080" // Use different port for E2E tests
	bearerToken := "e2e-test-token"
	cm.server = NewServer(cm, port, bearerToken, []string{}, log.New(os.Stdout, "E2E: ", log.LstdFlags))

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		_ = cm.server.Start(ctx)
	}()

	// Wait for server to start
	time.Sleep(200 * time.Millisecond)

	// Create HTTP client
	client := &http.Client{Timeout: 5 * time.Second}
	baseURL := fmt.Sprintf("http://localhost:%s", port)

	// Return cleanup function
	cleanup := func() {
		cancel()
		if cm.server != nil {
			_ = cm.server.Stop()
		}
	}

	return client, baseURL, cleanup
}

// e2eConfigManager is a minimal config manager for E2E tests
type e2eConfigManager struct {
	configPath string
	server     *Server
}

func (m *e2eConfigManager) GetConfigAny() any {
	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return map[string]interface{}{}
	}
	var result map[string]interface{}
	_ = json.Unmarshal(data, &result)
	return result
}

func (m *e2eConfigManager) WriteConfigAny(cfg any) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	// Create backup
	if _, err := os.Stat(m.configPath); err == nil {
		backupPath := m.configPath + ".backup"
		_ = os.WriteFile(backupPath, data, 0644)
	}

	return os.WriteFile(m.configPath, data, 0644)
}

func (m *e2eConfigManager) UpdateConfig(partial map[string]interface{}) error {
	current := m.GetConfigAny().(map[string]interface{})

	// Deep merge
	for k, v := range partial {
		current[k] = v
	}

	return m.WriteConfigAny(current)
}

// TestE2E_ConfigUpdateFlow tests the full config update flow
func TestE2E_ConfigUpdateFlow(t *testing.T) {
	initialConfig := generateConfig(3)
	client, baseURL, cleanup := setupTestEnvironment(t, initialConfig)
	defer cleanup()

	// Send PATCH request to update update_interval
	patchData := map[string]interface{}{
		"update_interval": 120,
	}
	jsonData, _ := json.Marshal(patchData)

	req, err := http.NewRequest("PATCH", baseURL+"/api/config", bytes.NewReader(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer e2e-test-token")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Verify response contains updated config
	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	updateInterval := response["update_interval"].(float64)
	if updateInterval != 120 {
		t.Errorf("Expected update_interval 120, got %v", updateInterval)
	}
}

// TestE2E_LargeConfig tests config with 1000 servers
func TestE2E_LargeConfig(t *testing.T) {
	largeConfig := generateConfig(1000)
	client, baseURL, cleanup := setupTestEnvironment(t, largeConfig)
	defer cleanup()

	// Verify we can retrieve the large config
	req, err := http.NewRequest("GET", baseURL+"/api/config", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer e2e-test-token")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	servers := result["servers"].([]interface{})
	if len(servers) != 1000 {
		t.Errorf("Expected 1000 servers, got %d", len(servers))
	}
}

// TestE2E_UnicodeConfig tests config with unicode strings
func TestE2E_UnicodeConfig(t *testing.T) {
	unicodeConfig := generateUnicodeConfig()
	client, baseURL, cleanup := setupTestEnvironment(t, unicodeConfig)
	defer cleanup()

	// Send PUT request to update config
	jsonData, _ := json.Marshal(unicodeConfig)

	req, err := http.NewRequest("PUT", baseURL+"/api/config", bytes.NewReader(jsonData))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer e2e-test-token")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("Expected status 200, got %d: %s", resp.StatusCode, string(body))
	}

	// Verify response contains unicode data correctly
	var response map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	categoryOrder := response["category_order"].([]interface{})
	if len(categoryOrder) != 3 {
		t.Errorf("Expected 3 categories, got %d", len(categoryOrder))
	}

	// Verify specific unicode values
	expectedCategories := []string{"漂移", "レース", "경주"}
	for i, cat := range categoryOrder {
		if cat.(string) != expectedCategories[i] {
			t.Errorf("Category %d: expected %s, got %v", i, expectedCategories[i], cat)
		}
	}
}

// TestE2E_Authentication tests that authentication is enforced
func TestE2E_Authentication(t *testing.T) {
	initialConfig := generateConfig(1)
	client, baseURL, cleanup := setupTestEnvironment(t, initialConfig)
	defer cleanup()

	// Request without auth token should fail
	req, err := http.NewRequest("GET", baseURL+"/api/config", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("Expected status 401, got %d", resp.StatusCode)
	}
}

// TestE2E_HealthCheck tests that health check works without auth
func TestE2E_HealthCheck(t *testing.T) {
	initialConfig := generateConfig(1)
	client, baseURL, cleanup := setupTestEnvironment(t, initialConfig)
	defer cleanup()

	// Health check should work without auth
	req, err := http.NewRequest("GET", baseURL+"/health", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("Expected status 'ok', got '%s'", result["status"])
	}
}

// TestE2E_GetServers tests the /api/config/servers endpoint
func TestE2E_GetServers(t *testing.T) {
	initialConfig := generateConfig(5)
	client, baseURL, cleanup := setupTestEnvironment(t, initialConfig)
	defer cleanup()

	req, err := http.NewRequest("GET", baseURL+"/api/config/servers", nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer e2e-test-token")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to send request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var servers []interface{}
	if err := json.NewDecoder(resp.Body).Decode(&servers); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(servers) != 5 {
		t.Errorf("Expected 5 servers, got %d", len(servers))
	}
}
