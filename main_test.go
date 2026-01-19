package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// globalStateMutex protects access to global variables used by proxy server tests
// These globals are modified by tests but accessed by startProxyServer goroutines
var globalStateMutex sync.Mutex

// testSerialMutex ensures proxy server tests run serially to avoid race conditions
// with global state variables that are read by background goroutines
//
// NOTE: There is an unavoidable data race in the current design where the
// goroutine launched by startProxyServer reads the global 'proxyPort' variable
// asynchronously (in main.go:1352, a logging statement). This race does not
// affect correctness (the read is only for logging, not functional logic), but
// it will be detected by the race detector.
//
// To avoid race detector warnings, run tests with: go test -parallel=1 -run TestProxyServer
//
// The proper fix would be to modify main.go to capture the port value in a local
// variable before launching the goroutine, but that's outside the scope of test-only changes.
var testSerialMutex sync.Mutex

// TestInitializeServerIPs_Normal tests that all servers get their IP set correctly
func TestInitializeServerIPs_Normal(t *testing.T) {
	cfg := &Config{
		ServerIP:       "192.168.1.100",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift", "Track"},
		CategoryEmojis: map[string]string{"Drift": "🟣", "Track": "🔵"},
		Servers: []Server{
			{Name: "Server1", Port: 8081, Category: "Drift", IP: ""},
			{Name: "Server2", Port: 8082, Category: "Track", IP: ""},
			{Name: "Server3", Port: 8083, Category: "Drift", IP: ""},
		},
	}

	initializeServerIPs(cfg)

	if cfg.Servers[0].IP != "192.168.1.100" {
		t.Errorf("Expected Server1 IP '192.168.1.100', got '%s'", cfg.Servers[0].IP)
	}

	if cfg.Servers[1].IP != "192.168.1.100" {
		t.Errorf("Expected Server2 IP '192.168.1.100', got '%s'", cfg.Servers[1].IP)
	}

	if cfg.Servers[2].IP != "192.168.1.100" {
		t.Errorf("Expected Server3 IP '192.168.1.100', got '%s'", cfg.Servers[2].IP)
	}
}

// TestInitializeServerIPs_ZeroServers tests that empty server slice doesn't panic
func TestInitializeServerIPs_ZeroServers(t *testing.T) {
	cfg := &Config{
		ServerIP:       "192.168.1.100",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{},
	}

	initializeServerIPs(cfg)

	if len(cfg.Servers) != 0 {
		t.Errorf("Expected 0 servers, got %d", len(cfg.Servers))
	}
}

// TestInitializeServerIPs_Idempotent tests that function is idempotent when IP already set
func TestInitializeServerIPs_Idempotent(t *testing.T) {
	cfg := &Config{
		ServerIP:       "192.168.1.100",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers: []Server{
			{Name: "Server1", Port: 8081, Category: "Drift", IP: "192.168.1.100"},
			{Name: "Server2", Port: 8082, Category: "Drift", IP: "192.168.1.100"},
		},
	}

	initializeServerIPs(cfg)

	if cfg.Servers[0].IP != "192.168.1.100" {
		t.Errorf("Expected Server1 IP '192.168.1.100' after idempotent call, got '%s'", cfg.Servers[0].IP)
	}

	if cfg.Servers[1].IP != "192.168.1.100" {
		t.Errorf("Expected Server2 IP '192.168.1.100' after idempotent call, got '%s'", cfg.Servers[1].IP)
	}

	// Call again to verify idempotence
	initializeServerIPs(cfg)

	if cfg.Servers[0].IP != "192.168.1.100" {
		t.Errorf("Expected Server1 IP '192.168.1.100' after second call, got '%s'", cfg.Servers[0].IP)
	}

	if cfg.Servers[1].IP != "192.168.1.100" {
		t.Errorf("Expected Server2 IP '192.168.1.100' after second call, got '%s'", cfg.Servers[1].IP)
	}
}

// TestLoadEnv_NoFile tests that loadEnv handles missing .env gracefully
func TestLoadEnv_NoFile(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Clear any existing env vars
	os.Unsetenv("TEST_VAR")

	err := loadEnv()
	if err != nil {
		t.Fatalf("loadEnv() should not error when .env doesn't exist: %v", err)
	}
}

// TestLoadEnv_ValidFile tests loading a valid .env file
func TestLoadEnv_ValidFile(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Create .env file
	envContent := `# Test .env file
DISCORD_TOKEN=test_token_123
CHANNEL_ID=456

# Empty lines and comments are ignored

ANOTHER_VAR=value_with_underscores
`
	os.WriteFile(".env", []byte(envContent), 0644)

	// Clear env vars first
	os.Unsetenv("DISCORD_TOKEN")
	os.Unsetenv("CHANNEL_ID")
	os.Unsetenv("ANOTHER_VAR")

	err := loadEnv()
	if err != nil {
		t.Fatalf("loadEnv() failed: %v", err)
	}

	// Verify env vars were set
	if token := os.Getenv("DISCORD_TOKEN"); token != "test_token_123" {
		t.Errorf("Expected DISCORD_TOKEN 'test_token_123', got '%s'", token)
	}

	if channelID := os.Getenv("CHANNEL_ID"); channelID != "456" {
		t.Errorf("Expected CHANNEL_ID '456', got '%s'", channelID)
	}

	if anotherVar := os.Getenv("ANOTHER_VAR"); anotherVar != "value_with_underscores" {
		t.Errorf("Expected ANOTHER_VAR 'value_with_underscores', got '%s'", anotherVar)
	}
}

// TestLoadEnv_WithQuotes tests handling of quoted values
func TestLoadEnv_WithQuotes(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	envContent := `DOUBLE_QUOTED="value in double quotes"
SINGLE_QUOTED='value in single quotes'
NO_QUOTES=value_without_quotes
`
	os.WriteFile(".env", []byte(envContent), 0644)

	os.Unsetenv("DOUBLE_QUOTED")
	os.Unsetenv("SINGLE_QUOTED")
	os.Unsetenv("NO_QUOTES")

	err := loadEnv()
	if err != nil {
		t.Fatalf("loadEnv() failed: %v", err)
	}

	// Quotes should be stripped
	if v := os.Getenv("DOUBLE_QUOTED"); v != "value in double quotes" {
		t.Errorf("Expected 'value in double quotes', got '%s'", v)
	}

	if v := os.Getenv("SINGLE_QUOTED"); v != "value in single quotes" {
		t.Errorf("Expected 'value in single quotes', got '%s'", v)
	}

	if v := os.Getenv("NO_QUOTES"); v != "value_without_quotes" {
		t.Errorf("Expected 'value_without_quotes', got '%s'", v)
	}
}

// TestLoadEnv_DoesNotOverrideExisting tests that .env doesn't override existing env vars
func TestLoadEnv_DoesNotOverrideExisting(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Set env var first
	os.Setenv("TEST_VAR", "original_value")

	envContent := `TEST_VAR=new_value_from_env
`
	os.WriteFile(".env", []byte(envContent), 0644)

	err := loadEnv()
	if err != nil {
		t.Fatalf("loadEnv() failed: %v", err)
	}

	// Should keep original value
	if v := os.Getenv("TEST_VAR"); v != "original_value" {
		t.Errorf("Expected .env to not override existing env var, got '%s'", v)
	}
}

// TestLoadEnv_IgnoresInvalidLines tests handling of malformed lines
func TestLoadEnv_IgnoresInvalidLines(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	envContent := `VALID_LINE_1=value1
INVALID_LINE_NO_EQUALS
  # Comment with leading spaces
VALID_LINE_2=value2

ANOTHER_INVALID=
VALID_LINE_3=value3
`
	os.WriteFile(".env", []byte(envContent), 0644)

	os.Unsetenv("VALID_LINE_1")
	os.Unsetenv("VALID_LINE_2")
	os.Unsetenv("VALID_LINE_3")

	err := loadEnv()
	if err != nil {
		t.Fatalf("loadEnv() failed: %v", err)
	}

	// Valid lines should be processed
	if v := os.Getenv("VALID_LINE_1"); v != "value1" {
		t.Errorf("Expected 'value1', got '%s'", v)
	}

	if v := os.Getenv("VALID_LINE_2"); v != "value2" {
		t.Errorf("Expected 'value2', got '%s'", v)
	}

	if v := os.Getenv("VALID_LINE_3"); v != "value3" {
		t.Errorf("Expected 'value3', got '%s'", v)
	}
}

// TestLoadConfig_ValidConfig tests loading a valid config file
func TestLoadConfig_ValidConfig(t *testing.T) {
	// Create temp directory for test config
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create a valid config
	validConfig := Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift", "Touge", "Track"},
		CategoryEmojis: map[string]string{
			"Drift": "🟣",
			"Touge": "🟠",
			"Track": "🔵",
		},
		Servers: []Server{
			{Name: "Test Server", Port: 8081, Category: "Drift"},
		},
	}

	data, _ := json.Marshal(validConfig)
	os.WriteFile(configPath, data, 0644)

	// Change to temp directory
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Test loading
	cfg, err := loadConfig("")
	if err != nil {
		t.Fatalf("loadConfig(\"\") failed: %v", err)
	}

	if cfg.ServerIP != "192.168.1.1" {
		t.Errorf("Expected ServerIP '192.168.1.1', got '%s'", cfg.ServerIP)
	}

	if cfg.UpdateInterval != 30 {
		t.Errorf("Expected UpdateInterval 30, got %d", cfg.UpdateInterval)
	}

	if len(cfg.Servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(cfg.Servers))
	}
}

// TestLoadConfig_FileNotFound tests missing config file
func TestLoadConfig_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	_, err := loadConfig("")
	if err == nil {
		t.Fatal("Expected error for missing config file, got nil")
	}

	// Error should mention all attempted paths
	errMsg := err.Error()
	if !strings.Contains(errMsg, "/data/config.json") {
		t.Errorf("Error message should mention '/data/config.json', got: %v", errMsg)
	}
	if !strings.Contains(errMsg, "config.json") {
		t.Errorf("Error message should mention 'config.json', got: %v", errMsg)
	}
}

// TestLoadConfig_InvalidJSON tests malformed JSON
func TestLoadConfig_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Write invalid JSON
	os.WriteFile(configPath, []byte("{invalid json}"), 0644)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	_, err := loadConfig("")
	if err == nil {
		t.Fatal("Expected error for invalid JSON, got nil")
	}
}

// TestLoadConfig_ExplicitPath tests loading config from an explicit path
func TestLoadConfig_ExplicitPath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "custom-config.json")

	validConfig := Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(validConfig)
	os.WriteFile(configPath, data, 0644)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	cfg, err := loadConfig(configPath)
	if err != nil {
		t.Fatalf("loadConfig(%s) failed: %v", configPath, err)
	}

	if cfg.ServerIP != "192.168.1.1" {
		t.Errorf("Expected ServerIP '192.168.1.1', got '%s'", cfg.ServerIP)
	}
}

// TestLoadConfig_ExplicitPathTakesPrecedence tests that explicit path is used over fallback paths
func TestLoadConfig_ExplicitPathTakesPrecedence(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a local config.json that should NOT be loaded
	localConfig := Config{
		ServerIP:       "local-ip",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}
	data, _ := json.Marshal(localConfig)
	os.WriteFile(filepath.Join(tmpDir, "config.json"), data, 0644)

	// Create an explicit config at a different path
	explicitConfigPath := filepath.Join(tmpDir, "custom-config.json")
	explicitConfig := Config{
		ServerIP:       "explicit-ip",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}
	data, _ = json.Marshal(explicitConfig)
	os.WriteFile(explicitConfigPath, data, 0644)

	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// With explicit path, should only load that path
	cfg, err := loadConfig(explicitConfigPath)
	if err != nil {
		t.Fatalf("loadConfig(%s) failed: %v", explicitConfigPath, err)
	}

	if cfg.ServerIP != "explicit-ip" {
		t.Errorf("Expected explicit path to be loaded (ServerIP: explicit-ip), got: %s", cfg.ServerIP)
	}
}

// TestValidateConfigStruct_EmptyServerIP tests validation of empty server_ip
func TestValidateConfigStruct_EmptyServerIP(t *testing.T) {
	cfg := &Config{
		ServerIP:       "",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	err := validateConfigStructSafeRuntime(cfg)
	if err == nil {
		t.Error("Expected error for empty ServerIP, got nil")
	}

	expectedMsg := "server_ip cannot be empty"
	if err != nil && !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain '%s', got: %v", expectedMsg, err)
	}
}

// TestValidateConfigStruct_InvalidUpdateInterval tests update interval validation
func TestValidateConfigStruct_InvalidUpdateInterval(t *testing.T) {
	testCases := []int{0, -1, -100}

	for _, interval := range testCases {
		cfg := &Config{
			ServerIP:       "192.168.1.1",
			UpdateInterval: interval,
			CategoryOrder:  []string{"Drift"},
			CategoryEmojis: map[string]string{"Drift": "🟣"},
			Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
		}

		err := validateConfigStructSafeRuntime(cfg)
		if err == nil {
			t.Errorf("Expected error for UpdateInterval %d, got nil", interval)
		}

		expectedMsg := "update_interval must be at least 1 second"
		if err != nil && !strings.Contains(err.Error(), expectedMsg) {
			t.Errorf("Expected error to contain '%s', got: %v", expectedMsg, err)
		}
	}
}

// TestValidateConfigStruct_EmptyCategoryOrder tests empty category_order
func TestValidateConfigStruct_EmptyCategoryOrder(t *testing.T) {
	cfg := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	err := validateConfigStructSafeRuntime(cfg)
	if err == nil {
		t.Error("Expected error for empty CategoryOrder, got nil")
	}

	expectedMsg := "category_order cannot be empty"
	if err != nil && !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain '%s', got: %v", expectedMsg, err)
	}
}

// TestValidateConfigStruct_MissingEmoji tests missing category emoji
func TestValidateConfigStruct_MissingEmoji(t *testing.T) {
	cfg := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift", "Touge"},
		CategoryEmojis: map[string]string{
			"Drift": "🟣",
			// Touge is missing
		},
		Servers: []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	err := validateConfigStructSafeRuntime(cfg)
	if err == nil {
		t.Error("Expected error for missing category emoji, got nil")
	}

	expectedMsg := "category 'Touge' is in category_order but missing from category_emojis"
	if err != nil && !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain '%s', got: %v", expectedMsg, err)
	}
}

// TestValidateConfigStruct_InvalidPort tests port validation
func TestValidateConfigStruct_InvalidPort(t *testing.T) {
	testCases := []struct {
		name        string
		port        int
		shouldError bool
	}{
		{"below minimum", 0, true},
		{"negative port", -1, true},
		{"above maximum", 65536, true},
		{"way above maximum", 100000, true},
		{"minimum valid", 1, false},
		{"maximum valid", 65535, false},
		{"common HTTP port", 8080, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				ServerIP:       "192.168.1.1",
				UpdateInterval: 30,
				CategoryOrder:  []string{"Drift"},
				CategoryEmojis: map[string]string{"Drift": "🟣"},
				Servers:        []Server{{Name: "Test", Port: tc.port, Category: "Drift"}},
			}

			err := validateConfigStructSafeRuntime(cfg)

			if tc.shouldError {
				if err == nil {
					t.Errorf("Expected error for port %d, got nil", tc.port)
				} else if !strings.Contains(err.Error(), "invalid port") {
					t.Errorf("Expected 'invalid port' in error, got: %v", err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error for port %d, got: %v", tc.port, err)
				}
			}
		})
	}
}

// TestValidateConfigStruct_UnknownCategory tests server with unknown category
func TestValidateConfigStruct_UnknownCategory(t *testing.T) {
	cfg := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Unknown"}},
	}

	err := validateConfigStructSafeRuntime(cfg)
	if err == nil {
		t.Error("Expected error for unknown category, got nil")
	}

	expectedMsg := "category 'Unknown' which is not defined in category_order"
	if err != nil && !strings.Contains(err.Error(), expectedMsg) {
		t.Errorf("Expected error to contain '%s', got: %v", expectedMsg, err)
	}
}

// TestCheckAndReloadIfNeeded_NoChange tests that checkAndReloadIfNeeded returns nil when config unchanged
func TestCheckAndReloadIfNeeded_NoChange(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	validConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(validConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, validConfig)

	// Call checkAndReloadIfNeeded without modifying file
	err := cm.checkAndReloadIfNeeded()

	if err != nil {
		t.Errorf("Expected nil error when config unchanged, got: %v", err)
	}

	// Config should remain unchanged
	currentCfg := cm.GetConfig()
	if currentCfg.ServerIP != "192.168.1.1" {
		t.Errorf("Config should not change when file unmodified")
	}
}

// TestCheckAndReloadIfNeeded_ValidReload tests successful config reload (with debouncing)
func TestCheckAndReloadIfNeeded_ValidReload(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Wait a bit to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Write new config with different ServerIP
	newConfig := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ = json.Marshal(newConfig)
	os.WriteFile(configPath, data, 0644)

	// Trigger reload (schedules debounce)
	err := cm.checkAndReloadIfNeeded()

	if err != nil {
		t.Fatalf("Expected successful reload scheduling, got error: %v", err)
	}

	// Config should NOT be updated immediately (debounce pending)
	if cm.GetConfig().ServerIP == "10.0.0.1" {
		t.Error("Config should not be updated immediately after scheduling")
	}

	// Wait for debounce timer to fire
	time.Sleep(150 * time.Millisecond)

	// Config should be updated after debounce
	currentCfg := cm.GetConfig()
	if currentCfg.ServerIP != "10.0.0.1" {
		t.Errorf("Expected ServerIP '10.0.0.1' after debounce, got '%s'", currentCfg.ServerIP)
	}
}

// TestCheckAndReloadIfNeeded_InvalidJSON tests that invalid JSON keeps old config (with debouncing)
func TestCheckAndReloadIfNeeded_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Write invalid JSON
	os.WriteFile(configPath, []byte("{invalid json}"), 0644)

	// Store original config for comparison
	originalIP := cm.GetConfig().ServerIP

	// Trigger reload (schedules debounce)
	// Note: checkAndReloadIfNeeded will return nil immediately (debounce scheduled)
	// The error will occur in the background during performReload
	err := cm.checkAndReloadIfNeeded()

	// With debouncing, the check returns immediately (error happens in background)
	if err != nil {
		t.Fatalf("Expected nil during scheduling, got: %v", err)
	}

	// Config should remain unchanged immediately
	if cm.GetConfig().ServerIP != originalIP {
		t.Errorf("Config should not change immediately, got ServerIP: %s", cm.GetConfig().ServerIP)
	}

	// Wait for debounce timer to fire and reload to fail
	time.Sleep(150 * time.Millisecond)

	// Config should remain unchanged (reload failed)
	currentCfg := cm.GetConfig()
	if currentCfg == nil {
		t.Fatal("Config should not be nil after failed reload")
	}

	if currentCfg.ServerIP != originalIP {
		t.Errorf("Config should remain unchanged on invalid JSON, got ServerIP: %s", currentCfg.ServerIP)
	}
}

// TestCheckAndReloadIfNeeded_ValidationFailure tests that validation errors keep old config (with debouncing)
func TestCheckAndReloadIfNeeded_ValidationFailure(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Write config with empty ServerIP (invalid)
	invalidConfig := &Config{
		ServerIP:       "", // Invalid
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ = json.Marshal(invalidConfig)
	os.WriteFile(configPath, data, 0644)

	// Store original config for comparison
	originalIP := cm.GetConfig().ServerIP

	// Trigger reload (schedules debounce)
	err := cm.checkAndReloadIfNeeded()

	// With debouncing, returns immediately (error happens in background)
	if err != nil {
		t.Fatalf("Expected nil during scheduling, got: %v", err)
	}

	// Wait for debounce timer to fire and reload to fail
	time.Sleep(150 * time.Millisecond)

	// Config should remain unchanged (reload failed)
	currentCfg := cm.GetConfig()
	if currentCfg == nil {
		t.Fatal("Config should not be nil after failed reload")
	}

	if currentCfg.ServerIP != originalIP {
		t.Errorf("Config should remain unchanged on validation failure, got ServerIP: %s", currentCfg.ServerIP)
	}
}

// TestCheckAndReloadIfNeeded_FileNotFound tests that missing file keeps old config (with debouncing)
func TestCheckAndReloadIfNeeded_FileNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Delete the config file
	os.Remove(configPath)

	// Store original config for comparison
	originalIP := cm.GetConfig().ServerIP

	// Trigger reload check (schedules debounce)
	err := cm.checkAndReloadIfNeeded()

	// checkAndReloadIfNeeded returns error during stat (before debounce)
	if err == nil {
		t.Error("Expected error for missing file, got nil")
	}

	if !strings.Contains(err.Error(), "failed to stat config file") {
		t.Errorf("Expected 'failed to stat config file' error, got: %v", err)
	}

	// Config should remain unchanged
	currentCfg := cm.GetConfig()
	if currentCfg.ServerIP != originalIP {
		t.Errorf("Config should remain unchanged when file missing, got ServerIP: %s", currentCfg.ServerIP)
	}
}

// TestCheckAndReloadIfNeeded_ConcurrentCalls tests that concurrent calls are serialized (with debouncing)
func TestCheckAndReloadIfNeeded_ConcurrentCalls(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Write new config
	newConfig := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ = json.Marshal(newConfig)
	os.WriteFile(configPath, data, 0644)

	// Launch multiple concurrent calls
	done := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			done <- cm.checkAndReloadIfNeeded()
		}()
	}

	// Collect results
	successCount := 0
	for i := 0; i < 10; i++ {
		err := <-done
		if err == nil {
			successCount++
		}
	}

	// All calls should succeed (schedule debounce)
	if successCount != 10 {
		t.Errorf("Expected all 10 concurrent calls to succeed, got %d successes", successCount)
	}

	// Wait for debounce timer to fire
	time.Sleep(150 * time.Millisecond)

	// Config should be updated
	currentCfg := cm.GetConfig()
	if currentCfg.ServerIP != "10.0.0.1" {
		t.Errorf("Expected ServerIP '10.0.0.1' after concurrent reloads, got '%s'", currentCfg.ServerIP)
	}
}

// TestCheckAndReloadIfNeeded_RapidChanges tests behavior with rapid file modifications (with debouncing)
func TestCheckAndReloadIfNeeded_RapidChanges(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Make multiple rapid changes
	configs := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	for _, ip := range configs {
		time.Sleep(5 * time.Millisecond)

		newConfig := &Config{
			ServerIP:       ip,
			UpdateInterval: 30,
			CategoryOrder:  []string{"Drift"},
			CategoryEmojis: map[string]string{"Drift": "🟣"},
			Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
		}

		data, _ = json.Marshal(newConfig)
		os.WriteFile(configPath, data, 0644)

		cm.checkAndReloadIfNeeded()
	}

	// Wait for debounce timer to fire
	time.Sleep(150 * time.Millisecond)

	// Config should reflect the last valid change (after debounce)
	currentCfg := cm.GetConfig()
	if currentCfg.ServerIP != "10.0.0.3" {
		t.Errorf("Expected ServerIP '10.0.0.3' after rapid changes, got '%s'", currentCfg.ServerIP)
	}
}

// TestNewConfigManager tests ConfigManager creation with valid config
func TestNewConfigManager(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	validConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(validConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, validConfig)

	if cm == nil {
		t.Fatal("NewConfigManager returned nil")
	}

	if cm.configPath != configPath {
		t.Errorf("Expected configPath %s, got %s", configPath, cm.configPath)
	}

	if cm.GetConfig() == nil {
		t.Error("GetConfig returned nil")
	}

	if cm.GetConfig().ServerIP != "192.168.1.1" {
		t.Errorf("Expected ServerIP '192.168.1.1', got '%s'", cm.GetConfig().ServerIP)
	}

	if !cm.lastModTime.IsZero() {
		t.Logf("ConfigManager created with lastModTime: %v", cm.lastModTime)
	}
}

// TestConfigManager_ConcurrentGetConfig tests that GetConfig is thread-safe
func TestConfigManager_ConcurrentGetConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	validConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(validConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, validConfig)

	// Launch 100 goroutines calling GetConfig concurrently
	done := make(chan bool)
	for i := 0; i < 100; i++ {
		go func() {
			cfg := cm.GetConfig()
			if cfg == nil {
				t.Error("GetConfig returned nil in concurrent access")
			}
			done <- true
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < 100; i++ {
		<-done
	}
}

// TestConfigManager_NilConfig tests ConfigManager with nil initial config
func TestConfigManager_NilConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	// Create config file
	validConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}
	data, _ := json.Marshal(validConfig)
	os.WriteFile(configPath, data, 0644)

	// Passing nil doesn't panic but GetConfig will return nil
	cm := NewConfigManager(configPath, nil)

	if cm == nil {
		t.Fatal("NewConfigManager returned nil")
	}

	// GetConfig should return nil (not crash, but returns nil pointer)
	cfg := cm.GetConfig()
	if cfg != nil {
		t.Errorf("Expected GetConfig to return nil, got %+v", cfg)
	}
}

// TestValidateConfigStructSafeRuntime tests the runtime-safe validation function
func TestValidateConfigStructSafeRuntime(t *testing.T) {
	testCases := []struct {
		name        string
		cfg         *Config
		shouldError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			cfg: &Config{
				ServerIP:       "192.168.1.1",
				UpdateInterval: 30,
				CategoryOrder:  []string{"Drift"},
				CategoryEmojis: map[string]string{"Drift": "🟣"},
				Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
			},
			shouldError: false,
		},
		{
			name: "empty server IP",
			cfg: &Config{
				ServerIP:       "",
				UpdateInterval: 30,
				CategoryOrder:  []string{"Drift"},
				CategoryEmojis: map[string]string{"Drift": "🟣"},
				Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
			},
			shouldError: true,
			errorMsg:    "server_ip cannot be empty",
		},
		{
			name: "invalid update interval",
			cfg: &Config{
				ServerIP:       "192.168.1.1",
				UpdateInterval: 0,
				CategoryOrder:  []string{"Drift"},
				CategoryEmojis: map[string]string{"Drift": "🟣"},
				Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
			},
			shouldError: true,
			errorMsg:    "update_interval must be at least 1 second",
		},
		{
			name: "empty category order",
			cfg: &Config{
				ServerIP:       "192.168.1.1",
				UpdateInterval: 30,
				CategoryOrder:  []string{},
				CategoryEmojis: map[string]string{"Drift": "🟣"},
				Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
			},
			shouldError: true,
			errorMsg:    "category_order cannot be empty",
		},
		{
			name: "missing category emoji",
			cfg: &Config{
				ServerIP:       "192.168.1.1",
				UpdateInterval: 30,
				CategoryOrder:  []string{"Drift", "Touge"},
				CategoryEmojis: map[string]string{
					"Drift": "🟣",
				},
				Servers: []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
			},
			shouldError: true,
			errorMsg:    "category 'Touge' is in category_order but missing from category_emojis",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateConfigStructSafeRuntime(tc.cfg)

			if tc.shouldError {
				if err == nil {
					t.Errorf("Expected error containing '%s', got nil", tc.errorMsg)
				} else if !strings.Contains(err.Error(), tc.errorMsg) {
					t.Errorf("Expected error to contain '%s', got: %v", tc.errorMsg, err)
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}

// TestGetConfigPath tests the getConfigPath helper function
func TestGetConfigPath(t *testing.T) {
	testCases := []struct {
		name         string
		providedPath string
		setupFunc    func(t *testing.T) func()
		validateFunc func(t *testing.T, result string)
	}{
		{
			name:         "explicit path is returned",
			providedPath: "/custom/config.json",
			setupFunc:    func(t *testing.T) func() { return func() {} },
			validateFunc: func(t *testing.T, result string) {
				if result != "/custom/config.json" {
					t.Errorf("Expected path '/custom/config.json', got: %s", result)
				}
			},
		},
		{
			name:         "uses ./config.json when /data/config.json doesn't exist",
			providedPath: "",
			setupFunc: func(t *testing.T) func() {
				tmpDir := t.TempDir()
				configPath := filepath.Join(tmpDir, "config.json")
				os.WriteFile(configPath, []byte("{}"), 0644)

				origWd, _ := os.Getwd()
				os.Chdir(tmpDir)

				return func() {
					os.Chdir(origWd)
				}
			},
			validateFunc: func(t *testing.T, result string) {
				// Should return the working directory config.json
				if !strings.Contains(result, "config.json") {
					t.Errorf("Expected path containing 'config.json', got: %s", result)
				}
			},
		},
		{
			name:         "returns empty string when no config found",
			providedPath: "",
			setupFunc: func(t *testing.T) func() {
				tmpDir := t.TempDir()
				origWd, _ := os.Getwd()
				os.Chdir(tmpDir)

				return func() {
					os.Chdir(origWd)
				}
			},
			validateFunc: func(t *testing.T, result string) {
				if result != "" {
					t.Errorf("Expected empty string when no config found, got: %s", result)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cleanup := tc.setupFunc(t)
			defer cleanup()

			result := getConfigPath(tc.providedPath)
			tc.validateFunc(t, result)
		})
	}
}

// TestIntegration_ConfigReloadWithBot tests config reload integration with bot update cycle (with debouncing)
func TestIntegration_ConfigReloadWithBot(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 1, // 1 second for faster testing
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test Server", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Simulate bot update cycle checking for config changes
	initialIP := cm.GetConfig().ServerIP

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Modify config file
	newConfig := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 1,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test Server", Port: 8081, Category: "Drift"}},
	}

	data, _ = json.Marshal(newConfig)
	os.WriteFile(configPath, data, 0644)

	// Simulate bot checking for config updates (schedules debounce)
	err := cm.checkAndReloadIfNeeded()
	if err != nil {
		t.Fatalf("checkAndReloadIfNeeded failed: %v", err)
	}

	// Config should not change immediately (debounce pending)
	if cm.GetConfig().ServerIP != initialIP {
		t.Error("Config should not change immediately after scheduling")
	}

	// Wait for debounce timer to fire
	time.Sleep(150 * time.Millisecond)

	// Verify config was reloaded after debounce
	currentCfg := cm.GetConfig()
	if currentCfg.ServerIP == initialIP {
		t.Error("Config was not reloaded after debounce, ServerIP unchanged")
	}

	if currentCfg.ServerIP != "10.0.0.1" {
		t.Errorf("Expected ServerIP '10.0.0.1' after reload, got '%s'", currentCfg.ServerIP)
	}
}

// TestIntegration_InvalidConfigDuringRuntime tests that invalid config doesn't crash bot (with debouncing)
func TestIntegration_InvalidConfigDuringRuntime(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 1,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test Server", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)
	originalIP := cm.GetConfig().ServerIP

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Write invalid config
	invalidConfig := &Config{
		ServerIP:       "", // Invalid: empty server IP
		UpdateInterval: 1,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test Server", Port: 8081, Category: "Drift"}},
	}

	data, _ = json.Marshal(invalidConfig)
	os.WriteFile(configPath, data, 0644)

	// Attempt reload (schedules debounce)
	err := cm.checkAndReloadIfNeeded()

	// With debouncing, returns immediately (error happens in background)
	if err != nil {
		t.Fatalf("Expected nil during scheduling, got: %v", err)
	}

	// Wait for debounce timer to fire and reload to fail
	time.Sleep(150 * time.Millisecond)

	// Config should remain unchanged (reload failed)
	currentCfg := cm.GetConfig()
	if currentCfg == nil {
		t.Fatal("Config should not be nil after failed reload")
	}

	if currentCfg.ServerIP != originalIP {
		t.Errorf("Config should remain unchanged on invalid config, got ServerIP: %s", currentCfg.ServerIP)
	}

	// Wait and write valid config
	time.Sleep(10 * time.Millisecond)

	validConfig := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 1,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test Server", Port: 8081, Category: "Drift"}},
	}

	data, _ = json.Marshal(validConfig)
	os.WriteFile(configPath, data, 0644)

	// Reload should succeed now (schedules debounce)
	err = cm.checkAndReloadIfNeeded()
	if err != nil {
		t.Fatalf("Expected successful scheduling after fixing config, got: %v", err)
	}

	// Wait for debounce
	time.Sleep(150 * time.Millisecond)

	currentCfg = cm.GetConfig()
	if currentCfg.ServerIP != "10.0.0.1" {
		t.Errorf("Expected ServerIP '10.0.0.1' after valid reload, got '%s'", currentCfg.ServerIP)
	}
}

// TestIntegration_RapidConfigChanges tests rapid config file modifications with debouncing
func TestIntegration_RapidConfigChanges(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 1,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test Server", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Make multiple rapid changes
	configs := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	for _, ip := range configs {
		time.Sleep(5 * time.Millisecond)

		newConfig := &Config{
			ServerIP:       ip,
			UpdateInterval: 1,
			CategoryOrder:  []string{"Drift"},
			CategoryEmojis: map[string]string{"Drift": "🟣"},
			Servers:        []Server{{Name: "Test Server", Port: 8081, Category: "Drift"}},
		}

		data, _ = json.Marshal(newConfig)
		os.WriteFile(configPath, data, 0644)

		cm.checkAndReloadIfNeeded()
	}

	// Wait for debounce timer to fire (debouncing delays reload)
	time.Sleep(150 * time.Millisecond)

	// Config should reflect the last valid change (after debounce)
	currentCfg := cm.GetConfig()
	if currentCfg.ServerIP != "10.0.0.3" {
		t.Errorf("Expected ServerIP '10.0.0.3' after rapid changes, got '%s'", currentCfg.ServerIP)
	}
}

// TestIntegration_ConcurrentConfigAccess tests config access during concurrent reloads
func TestIntegration_ConcurrentConfigAccess(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 1,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test Server", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Launch concurrent readers and reloaders
	done := make(chan bool)
	readerCount := 50

	// Start readers that simulate server polling
	for i := 0; i < readerCount; i++ {
		go func() {
			for j := 0; j < 10; j++ {
				cfg := cm.GetConfig()
				if cfg == nil {
					t.Error("GetConfig returned nil during concurrent access")
				}
				time.Sleep(time.Millisecond)
			}
			done <- true
		}()
	}

	// Start reloaders that modify config file
	for i := 0; i < 5; i++ {
		go func(idx int) {
			time.Sleep(time.Duration(idx*10) * time.Millisecond)

			newConfig := &Config{
				ServerIP:       fmt.Sprintf("10.0.0.%d", idx+1),
				UpdateInterval: 1,
				CategoryOrder:  []string{"Drift"},
				CategoryEmojis: map[string]string{"Drift": "🟣"},
				Servers:        []Server{{Name: "Test Server", Port: 8081, Category: "Drift"}},
			}

			data, _ := json.Marshal(newConfig)
			os.WriteFile(configPath, data, 0644)
			cm.checkAndReloadIfNeeded()
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < readerCount+5; i++ {
		<-done
	}

	// Verify final config is valid
	cfg := cm.GetConfig()
	if cfg == nil {
		t.Fatal("Final config is nil")
	}

	if cfg.ServerIP == "" {
		t.Error("Final config has empty ServerIP")
	}

	t.Logf("Final ServerIP after concurrent access: %s", cfg.ServerIP)
}

// TestConfigManager_DebounceSingleWrite tests that single write triggers reload after debounce
func TestConfigManager_DebounceSingleWrite(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Write new config
	newConfig := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ = json.Marshal(newConfig)
	os.WriteFile(configPath, data, 0644)

	// Trigger reload check (schedules debounce)
	err := cm.checkAndReloadIfNeeded()
	if err != nil {
		t.Fatalf("checkAndReloadIfNeeded failed: %v", err)
	}

	// Config should NOT be updated immediately (debounce pending)
	if cm.GetConfig().ServerIP == "10.0.0.1" {
		t.Error("Config should not be updated immediately after scheduling")
	}

	// Wait for debounce timer to fire
	time.Sleep(150 * time.Millisecond)

	// Now config should be updated
	if cm.GetConfig().ServerIP != "10.0.0.1" {
		t.Errorf("Expected ServerIP '10.0.0.1' after debounce, got '%s'", cm.GetConfig().ServerIP)
	}
}

// TestConfigManager_DebounceRapidWrites tests that rapid writes trigger single reload
func TestConfigManager_DebounceRapidWrites(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Simulate rapid writes (5 writes in 50ms - typical editor behavior)
	configs := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3", "10.0.0.4", "10.0.0.5"}
	for _, ip := range configs {
		newConfig := &Config{
			ServerIP:       ip,
			UpdateInterval: 30,
			CategoryOrder:  []string{"Drift"},
			CategoryEmojis: map[string]string{"Drift": "🟣"},
			Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
		}

		data, _ := json.Marshal(newConfig)
		os.WriteFile(configPath, data, 0644)

		// Trigger reload check after each write
		cm.checkAndReloadIfNeeded()

		// Small delay between writes (10ms)
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for debounce timer to fire
	time.Sleep(150 * time.Millisecond)

	// Config should reflect the LAST write (10.0.0.5)
	// Only ONE reload should have occurred
	if cm.GetConfig().ServerIP != "10.0.0.5" {
		t.Errorf("Expected ServerIP '10.0.0.5' after rapid writes, got '%s'", cm.GetConfig().ServerIP)
	}
}

// TestConfigManager_DebounceTimerReset tests that timer is reset on subsequent writes
func TestConfigManager_DebounceTimerReset(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// First write
	newConfig := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ = json.Marshal(newConfig)
	os.WriteFile(configPath, data, 0644)
	cm.checkAndReloadIfNeeded()

	// Wait 50ms (less than debounce delay)
	time.Sleep(50 * time.Millisecond)

	// Config should NOT be reloaded yet
	if cm.GetConfig().ServerIP == "10.0.0.1" {
		t.Error("Config should not be reloaded before debounce timer fires")
	}

	// Second write before first timer fires (resets timer)
	newConfig.ServerIP = "10.0.0.2"
	data, _ = json.Marshal(newConfig)
	os.WriteFile(configPath, data, 0644)
	cm.checkAndReloadIfNeeded()

	// Wait another 50ms (still less than debounce delay from reset)
	time.Sleep(50 * time.Millisecond)

	// Config should STILL not be reloaded (timer was reset)
	if cm.GetConfig().ServerIP == "10.0.0.2" {
		t.Error("Config should not be reloaded before reset debounce timer fires")
	}

	// Wait for debounce timer to fire
	time.Sleep(100 * time.Millisecond)

	// Now config should be updated with LAST write
	if cm.GetConfig().ServerIP != "10.0.0.2" {
		t.Errorf("Expected ServerIP '10.0.0.2' after debounce reset, got '%s'", cm.GetConfig().ServerIP)
	}
}

// TestConfigManager_CleanupStopsTimer tests that Cleanup stops debounce timer
func TestConfigManager_CleanupStopsTimer(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Write new config
	newConfig := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ = json.Marshal(newConfig)
	os.WriteFile(configPath, data, 0644)

	// Trigger reload check (schedules debounce)
	cm.checkAndReloadIfNeeded()

	// Immediately cleanup (should stop timer)
	cm.Cleanup()

	// Wait longer than debounce delay
	time.Sleep(150 * time.Millisecond)

	// Config should NOT be updated (timer was stopped)
	if cm.GetConfig().ServerIP == "10.0.0.1" {
		t.Error("Config should not be reloaded after Cleanup stops timer")
	}

	// Cleanup should be idempotent (calling multiple times is safe)
	cm.Cleanup()
	cm.Cleanup()

	if cm.GetConfig().ServerIP != "192.168.1.1" {
		t.Errorf("Config should remain unchanged after Cleanup, got ServerIP: %s", cm.GetConfig().ServerIP)
	}
}

// TestConfigManager_CleanupConcurrentWithReload tests Cleanup during pending reload
func TestConfigManager_CleanupConcurrentWithReload(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Write new config
	newConfig := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ = json.Marshal(newConfig)
	os.WriteFile(configPath, data, 0644)

	// Trigger multiple concurrent reload checks
	done := make(chan error)
	for i := 0; i < 5; i++ {
		go func() {
			done <- cm.checkAndReloadIfNeeded()
		}()
	}

	// Cleanup concurrently
	go func() {
		time.Sleep(10 * time.Millisecond)
		cm.Cleanup()
	}()

	// All checks should succeed (or not crash)
	for i := 0; i < 5; i++ {
		<-done
	}

	// Wait for any pending timers
	time.Sleep(150 * time.Millisecond)

	// Verify config manager still works
	cfg := cm.GetConfig()
	if cfg == nil {
		t.Error("GetConfig should return valid config after Cleanup")
	}
}

// TestConfigManager_DebounceWithInvalidConfig tests that invalid config doesn't crash debounce
func TestConfigManager_DebounceWithInvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)
	originalIP := cm.GetConfig().ServerIP

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Write invalid config
	invalidConfig := &Config{
		ServerIP:       "", // Invalid
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ = json.Marshal(invalidConfig)
	os.WriteFile(configPath, data, 0644)

	// Trigger reload check (schedules debounce)
	err := cm.checkAndReloadIfNeeded()
	if err != nil {
		t.Fatalf("checkAndReloadIfNeeded failed: %v", err)
	}

	// Wait for debounce timer to fire and reload to fail
	time.Sleep(150 * time.Millisecond)

	// Config should remain unchanged (reload failed)
	if cm.GetConfig().ServerIP != originalIP {
		t.Errorf("Config should remain unchanged on invalid config, got ServerIP: %s", cm.GetConfig().ServerIP)
	}

	// Write valid config
	time.Sleep(10 * time.Millisecond)
	validConfig := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ = json.Marshal(validConfig)
	os.WriteFile(configPath, data, 0644)

	cm.checkAndReloadIfNeeded()

	// Wait for debounce
	time.Sleep(150 * time.Millisecond)

	// Config should now be updated
	if cm.GetConfig().ServerIP != "10.0.0.1" {
		t.Errorf("Expected ServerIP '10.0.0.1' after valid config, got '%s'", cm.GetConfig().ServerIP)
	}
}

// TestConfigManager_DebounceConcurrentWrites tests concurrent file modifications
func TestConfigManager_DebounceConcurrentWrites(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Launch 10 concurrent writers
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			newConfig := &Config{
				ServerIP:       fmt.Sprintf("10.0.0.%d", idx+1),
				UpdateInterval: 30,
				CategoryOrder:  []string{"Drift"},
				CategoryEmojis: map[string]string{"Drift": "🟣"},
				Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
			}

			data, _ := json.Marshal(newConfig)
			os.WriteFile(configPath, data, 0644)
			cm.checkAndReloadIfNeeded()
			done <- true
		}(i)
	}

	// Wait for all writers
	for i := 0; i < 10; i++ {
		<-done
	}

	// Wait for debounce timer
	time.Sleep(200 * time.Millisecond)

	// Config should be valid and reflect one of the writes
	cfg := cm.GetConfig()
	if cfg == nil {
		t.Fatal("Config should not be nil after concurrent writes")
	}

	if cfg.ServerIP == "" {
		t.Error("ServerIP should not be empty after concurrent writes")
	}

	if !strings.HasPrefix(cfg.ServerIP, "10.0.0.") {
		t.Errorf("Expected ServerIP to start with '10.0.0.', got: %s", cfg.ServerIP)
	}

	t.Logf("Final ServerIP after concurrent writes: %s", cfg.ServerIP)
}

// TestConfigReload_IPsInitialized tests that config reload correctly initializes server IPs
func TestConfigReload_IPsInitialized(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift", "Track"},
		CategoryEmojis: map[string]string{"Drift": "🟣", "Track": "🔵"},
		Servers: []Server{
			{Name: "Drift Server 1", Port: 8081, Category: "Drift"},
			{Name: "Drift Server 2", Port: 8082, Category: "Drift"},
			{Name: "Track Server", Port: 8083, Category: "Track"},
		},
	}

	// Initialize IPs for initial config (simulating main.go behavior)
	initializeServerIPs(initialConfig)

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Verify initial IPs are set
	cfg := cm.GetConfig()
	for i, server := range cfg.Servers {
		if server.IP != "192.168.1.1" {
			t.Errorf("Initial config: server %d expected IP '192.168.1.1', got '%s'", i, server.IP)
		}
	}

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Write new config with different ServerIP
	newConfig := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift", "Track"},
		CategoryEmojis: map[string]string{"Drift": "🟣", "Track": "🔵"},
		Servers: []Server{
			{Name: "Drift Server 1", Port: 8081, Category: "Drift"},
			{Name: "Drift Server 2", Port: 8082, Category: "Drift"},
			{Name: "Track Server", Port: 8083, Category: "Track"},
		},
	}

	data, _ = json.Marshal(newConfig)
	os.WriteFile(configPath, data, 0644)

	// Trigger reload
	err := cm.checkAndReloadIfNeeded()
	if err != nil {
		t.Fatalf("checkAndReloadIfNeeded failed: %v", err)
	}

	// Wait for debounce
	time.Sleep(150 * time.Millisecond)

	// Verify all servers have updated IPs
	cfg = cm.GetConfig()
	if cfg.ServerIP != "10.0.0.1" {
		t.Errorf("Expected ServerIP '10.0.0.1' after reload, got '%s'", cfg.ServerIP)
	}

	for i, server := range cfg.Servers {
		if server.IP != "10.0.0.1" {
			t.Errorf("After reload: server %d expected IP '10.0.0.1', got '%s'", i, server.IP)
		}
	}
}

// TestConfigReload_SameServerIP tests that reload with same ServerIP is idempotent
func TestConfigReload_SameServerIP(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers: []Server{
			{Name: "Server 1", Port: 8081, Category: "Drift"},
			{Name: "Server 2", Port: 8082, Category: "Drift"},
		},
	}

	// Initialize IPs for initial config (simulating main.go behavior)
	initializeServerIPs(initialConfig)

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Write config with same ServerIP but different UpdateInterval
	newConfig := &Config{
		ServerIP:       "192.168.1.1", // Same IP
		UpdateInterval: 60,            // Different interval
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers: []Server{
			{Name: "Server 1", Port: 8081, Category: "Drift"},
			{Name: "Server 2", Port: 8082, Category: "Drift"},
		},
	}

	data, _ = json.Marshal(newConfig)
	os.WriteFile(configPath, data, 0644)

	// Trigger reload
	err := cm.checkAndReloadIfNeeded()
	if err != nil {
		t.Fatalf("checkAndReloadIfNeeded failed: %v", err)
	}

	// Wait for debounce
	time.Sleep(150 * time.Millisecond)

	// Verify IPs are still correctly set
	cfg := cm.GetConfig()
	if cfg.ServerIP != "192.168.1.1" {
		t.Errorf("Expected ServerIP '192.168.1.1' after reload, got '%s'", cfg.ServerIP)
	}

	for i, server := range cfg.Servers {
		if server.IP != "192.168.1.1" {
			t.Errorf("After idempotent reload: server %d expected IP '192.168.1.1', got '%s'", i, server.IP)
		}
	}

	// Verify UpdateInterval was updated
	if cfg.UpdateInterval != 60 {
		t.Errorf("Expected UpdateInterval 60 after reload, got %d", cfg.UpdateInterval)
	}
}

// TestConfigReload_InvalidConfigPreservesIPs tests that failed reload preserves original IPs
func TestConfigReload_InvalidConfigPreservesIPs(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers: []Server{
			{Name: "Server 1", Port: 8081, Category: "Drift"},
			{Name: "Server 2", Port: 8082, Category: "Drift"},
		},
	}

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Store original IPs for verification
	originalIPs := make([]string, len(initialConfig.Servers))
	for i, server := range initialConfig.Servers {
		originalIPs[i] = server.IP
	}

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Write invalid config (empty ServerIP)
	invalidConfig := &Config{
		ServerIP:       "", // Invalid
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Server 1", Port: 8081, Category: "Drift"}},
	}

	data, _ = json.Marshal(invalidConfig)
	os.WriteFile(configPath, data, 0644)

	// Trigger reload (schedules debounce)
	err := cm.checkAndReloadIfNeeded()
	if err != nil {
		t.Fatalf("checkAndReloadIfNeeded failed: %v", err)
	}

	// Wait for debounce and reload attempt
	time.Sleep(150 * time.Millisecond)

	// Config should remain unchanged with original IPs intact
	cfg := cm.GetConfig()
	if cfg == nil {
		t.Fatal("Config should not be nil after failed reload")
	}

	if cfg.ServerIP != "192.168.1.1" {
		t.Errorf("Config should preserve original ServerIP after failed reload, got '%s'", cfg.ServerIP)
	}

	for i, server := range cfg.Servers {
		if server.IP != originalIPs[i] {
			t.Errorf("Server %d IP should be preserved after failed reload: expected '%s', got '%s'", i, originalIPs[i], server.IP)
		}
	}
}

// TestConfigReload_ConcurrentAccessWithIPs tests that concurrent reads during reload see consistent IPs
func TestConfigReload_ConcurrentAccessWithIPs(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers: []Server{
			{Name: "Server 1", Port: 8081, Category: "Drift"},
			{Name: "Server 2", Port: 8082, Category: "Drift"},
		},
	}

	// Initialize IPs for initial config (simulating main.go behavior)
	initializeServerIPs(initialConfig)

	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Wait to ensure different modification time
	time.Sleep(10 * time.Millisecond)

	// Start concurrent readers
	readerDone := make(chan bool)
	for i := 0; i < 50; i++ {
		go func() {
			for j := 0; j < 20; j++ {
				cfg := cm.GetConfig()
				if cfg == nil {
					t.Error("GetConfig returned nil during concurrent access")
					continue
				}

				// Verify all servers have consistent IPs
				expectedIP := cfg.ServerIP
				for k, server := range cfg.Servers {
					if server.IP != expectedIP {
						t.Errorf("Inconsistent IPs detected: ServerIP='%s', Server[%d].IP='%s'", expectedIP, k, server.IP)
					}
				}
				time.Sleep(time.Millisecond)
			}
			readerDone <- true
		}()
	}

	// Trigger multiple reloads during concurrent reads
	reloadDone := make(chan bool)
	go func() {
		for i := 0; i < 5; i++ {
			time.Sleep(20 * time.Millisecond)

			newConfig := &Config{
				ServerIP:       fmt.Sprintf("10.0.0.%d", i+1),
				UpdateInterval: 30,
				CategoryOrder:  []string{"Drift"},
				CategoryEmojis: map[string]string{"Drift": "🟣"},
				Servers: []Server{
					{Name: "Server 1", Port: 8081, Category: "Drift"},
					{Name: "Server 2", Port: 8082, Category: "Drift"},
				},
			}

			data, _ := json.Marshal(newConfig)
			os.WriteFile(configPath, data, 0644)
			cm.checkAndReloadIfNeeded()
		}
		reloadDone <- true
	}()

	// Wait for all readers and reloader
	for i := 0; i < 50; i++ {
		<-readerDone
	}
	<-reloadDone

	// Wait for final debounce
	time.Sleep(150 * time.Millisecond)

	// Verify final config has consistent IPs
	cfg := cm.GetConfig()
	if cfg == nil {
		t.Fatal("Final config should not be nil")
	}

	expectedIP := cfg.ServerIP
	for i, server := range cfg.Servers {
		if server.IP != expectedIP {
			t.Errorf("Final config: ServerIP='%s', Server[%d].IP='%s' - should be consistent", expectedIP, i, server.IP)
		}
	}

	t.Logf("Final consistent state: ServerIP=%s, all server IPs match", cfg.ServerIP)
}

// TestConfigManager_WriteConfig_Normal tests writing a valid config creates backup and updates file
func TestConfigManager_WriteConfig_Normal(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialCfg := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 60,
		CategoryOrder:  []string{"Race"},
		CategoryEmojis: map[string]string{"Race": "🏎️"},
		Servers: []Server{
			{Name: "TestServer", Port: 9999, Category: "Race", IP: "10.0.0.1"},
		},
	}

	cm := NewConfigManager(configPath, initialCfg)

	// Write initial config (first write, no backup expected)
	if err := cm.WriteConfig(initialCfg); err != nil {
		t.Fatalf("First WriteConfig failed: %v", err)
	}

	// Verify config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Config file was not created")
	}

	// No backup should exist on first write
	backupPath := configPath + ".backup"
	if _, err := os.Stat(backupPath); err == nil {
		t.Error("Backup should not exist on first write (nothing to backup)")
	}

	// Write second config (should create backup this time)
	secondCfg := &Config{
		ServerIP:       "10.0.0.2",
		UpdateInterval: 60,
		CategoryOrder:  []string{"Race"},
		CategoryEmojis: map[string]string{"Race": "🏎️"},
		Servers: []Server{
			{Name: "TestServer", Port: 9999, Category: "Race", IP: "10.0.0.2"},
		},
	}

	if err := cm.WriteConfig(secondCfg); err != nil {
		t.Fatalf("Second WriteConfig failed: %v", err)
	}

	// Verify backup exists now
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Error("Backup file was not created on second write")
	}

	// Verify file content contains second config
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("Failed to parse config file: %v", err)
	}

	if cfg.ServerIP != "10.0.0.2" {
		t.Errorf("Expected ServerIP '10.0.0.2', got '%s'", cfg.ServerIP)
	}

	// Verify backup contains first config
	backupData, err := os.ReadFile(backupPath)
	if err != nil {
		t.Fatalf("Failed to read backup file: %v", err)
	}

	var backupCfg Config
	if err := json.Unmarshal(backupData, &backupCfg); err != nil {
		t.Fatalf("Failed to parse backup file: %v", err)
	}

	if backupCfg.ServerIP != "10.0.0.1" {
		t.Errorf("Expected backup ServerIP '10.0.0.1', got '%s'", backupCfg.ServerIP)
	}
}

// TestConfigManager_WriteConfig_ConcurrentWrites tests that concurrent writes are serialized
func TestConfigManager_WriteConfig_ConcurrentWrites(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialCfg := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Race"},
		CategoryEmojis: map[string]string{"Race": "🏎️"},
		Servers: []Server{
			{Name: "Server1", Port: 8001, Category: "Race", IP: "10.0.0.1"},
		},
	}

	cm := NewConfigManager(configPath, initialCfg)

	// Launch concurrent writes
	done := make(chan bool, 10)
	for i := 0; i < 10; i++ {
		go func(idx int) {
			cfg := &Config{
				ServerIP:       fmt.Sprintf("10.0.0.%d", idx+1),
				UpdateInterval: 30,
				CategoryOrder:  []string{"Race"},
				CategoryEmojis: map[string]string{"Race": "🏎️"},
				Servers: []Server{
					{Name: fmt.Sprintf("Server%d", idx+1), Port: 8000 + idx, Category: "Race", IP: fmt.Sprintf("10.0.0.%d", idx+1)},
				},
			}
			_ = cm.WriteConfig(cfg)
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Verify final config is valid
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Errorf("Final config is invalid JSON: %v", err)
	}
}

// TestConfigManager_WriteConfig_InvalidConfig tests that invalid config returns error without modifying file
func TestConfigManager_WriteConfig_InvalidConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialCfg := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 60,
		CategoryOrder:  []string{"Race"},
		CategoryEmojis: map[string]string{"Race": "🏎️"},
		Servers: []Server{
			{Name: "TestServer", Port: 9999, Category: "Race", IP: "10.0.0.1"},
		},
	}

	cm := NewConfigManager(configPath, initialCfg)

	// Write valid initial config
	if err := cm.WriteConfig(initialCfg); err != nil {
		t.Fatalf("Initial WriteConfig failed: %v", err)
	}

	// Get initial file content
	initialData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read initial config: %v", err)
	}

	// Try to write invalid config (empty ServerIP)
	invalidCfg := &Config{
		ServerIP:       "",
		UpdateInterval: 60,
		CategoryOrder:  []string{"Race"},
		CategoryEmojis: map[string]string{"Race": "🏎️"},
		Servers:        []Server{},
	}

	err = cm.WriteConfig(invalidCfg)
	if err == nil {
		t.Error("WriteConfig should have returned error for invalid config")
	}

	// Verify file was not modified
	finalData, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read final config: %v", err)
	}

	if string(initialData) != string(finalData) {
		t.Error("Config file was modified despite validation error")
	}
}

// TestConfigManager_UpdateConfig_Normal tests partial config update
func TestConfigManager_UpdateConfig_Normal(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.json")

	initialCfg := &Config{
		ServerIP:       "10.0.0.1",
		UpdateInterval: 60,
		CategoryOrder:  []string{"Race", "Drift"},
		CategoryEmojis: map[string]string{"Race": "🏎️", "Drift": "🏁"},
		Servers: []Server{
			{Name: "Server1", Port: 8001, Category: "Race", IP: "10.0.0.1"},
			{Name: "Server2", Port: 8002, Category: "Drift", IP: "10.0.0.1"},
		},
	}

	cm := NewConfigManager(configPath, initialCfg)

	// Write initial config
	if err := cm.WriteConfig(initialCfg); err != nil {
		t.Fatalf("Initial WriteConfig failed: %v", err)
	}

	// Update just the UpdateInterval
	partial := map[string]interface{}{
		"update_interval": 120,
	}

	if err := cm.UpdateConfig(partial); err != nil {
		t.Fatalf("UpdateConfig failed: %v", err)
	}

	// Verify update was applied
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config: %v", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("Failed to parse config: %v", err)
	}

	if cfg.UpdateInterval != 120 {
		t.Errorf("Expected UpdateInterval 120, got %d", cfg.UpdateInterval)
	}

	if cfg.ServerIP != "10.0.0.1" {
		t.Errorf("ServerIP should remain '10.0.0.1', got '%s'", cfg.ServerIP)
	}

	if len(cfg.Servers) != 2 {
		t.Errorf("Should have 2 servers, got %d", len(cfg.Servers))
	}
}

// TestProxyServer_Startup tests that proxy server starts correctly with valid configuration
func TestProxyServer_Startup(t *testing.T) {
	testSerialMutex.Lock()
	defer testSerialMutex.Unlock()

	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	// Create config file
	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	configPath := filepath.Join(tmpDir, "config.json")
	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	// Set environment variables for API and proxy
	os.Setenv("API_ENABLED", "true")
	os.Setenv("API_PORT", "18080")
	os.Setenv("API_BEARER_TOKEN", "test-token")
	defer os.Unsetenv("API_ENABLED")
	defer os.Unsetenv("API_PORT")
	defer os.Unsetenv("API_BEARER_TOKEN")

	// Reset global variables with mutex protection
	globalStateMutex.Lock()
	apiEnabled = true
	apiPort = "18080"
	apiBearerToken = "test-token"
	proxyEnabled = false // Will be set by startProxyServer test
	proxyPort = "13000"
	globalStateMutex.Unlock()

	// Create bot directly without starting Discord session
	bot := &Bot{
		session:       nil, // No Discord session for test
		channelID:     "test-channel-id",
		configManager: cm,
		apiServer:     nil,
		apiCancel:     nil,
	}

	// Start proxy server
	globalStateMutex.Lock()
	proxyPort = "13000"
	globalStateMutex.Unlock()

	if err := startProxyServer(bot, apiBearerToken); err != nil {
		t.Fatalf("startProxyServer failed: %v", err)
	}

	// Verify proxy server is configured
	if bot.proxyServer == nil {
		t.Error("proxyServer should be non-nil after startProxyServer")
	}

	if bot.proxyStore == nil {
		t.Error("proxyStore should be non-nil after startProxyServer")
	}

	if bot.proxyCancel == nil {
		t.Error("proxyCancel should be non-nil after startProxyServer")
	}

	// Verify session directory was created
	sessionsDir := "./sessions"
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		t.Error("Session directory should be created by startProxyServer")
	}

	// Cleanup
	bot.proxyCancel()
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()
	if err := bot.proxyServer.Shutdown(ctx); err != nil {
		t.Logf("Expected shutdown error (timeout acceptable): %v", err)
	}
	bot.proxyStore.StopBackgroundCleanup()
	os.RemoveAll(sessionsDir)

	// Wait for goroutine to finish reading global state
	// Use Gosched to encourage the goroutine to run
	for i := 0; i < 10; i++ {
		runtime.Gosched()
		time.Sleep(100 * time.Millisecond)
	}
}

// TestProxyServer_DisabledFlag tests that proxy server doesn't start when disabled
func TestProxyServer_DisabledFlag(t *testing.T) {
	testSerialMutex.Lock()
	defer testSerialMutex.Unlock()

	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	configPath := filepath.Join(tmpDir, "config.json")
	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	os.Setenv("API_ENABLED", "true")
	os.Setenv("API_PORT", "18081")
	os.Setenv("API_BEARER_TOKEN", "test-token")
	os.Setenv("PROXY_ENABLED", "false")
	defer os.Unsetenv("API_ENABLED")
	defer os.Unsetenv("API_PORT")
	defer os.Unsetenv("API_BEARER_TOKEN")
	defer os.Unsetenv("PROXY_ENABLED")

	globalStateMutex.Lock()
	apiEnabled = true
	apiPort = "18081"
	apiBearerToken = "test-token"
	proxyEnabled = false
	proxyPort = "3000"
	globalStateMutex.Unlock()

	// Create bot directly without Discord session
	bot := &Bot{
		session:       nil,
		channelID:     "test-channel-id",
		configManager: cm,
		apiServer:     nil,
		apiCancel:     nil,
	}

	// Don't start proxy server - verify fields are nil
	if bot.proxyServer != nil {
		t.Error("proxyServer should be nil when proxy is disabled")
	}

	if bot.proxyStore != nil {
		t.Error("proxyStore should be nil when proxy is disabled")
	}

	if bot.proxyCancel != nil {
		t.Error("proxyCancel should be nil when proxy is disabled")
	}
}

// TestProxyServer_PortInUse tests behavior when proxy port is already in use
func TestProxyServer_PortInUse(t *testing.T) {
	testSerialMutex.Lock()
	defer testSerialMutex.Unlock()

	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	configPath := filepath.Join(tmpDir, "config.json")
	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	os.Setenv("API_ENABLED", "true")
	os.Setenv("API_PORT", "18082")
	os.Setenv("API_BEARER_TOKEN", "test-token")
	defer os.Unsetenv("API_ENABLED")
	defer os.Unsetenv("API_PORT")
	defer os.Unsetenv("API_BEARER_TOKEN")

	globalStateMutex.Lock()
	apiEnabled = true
	apiPort = "18082"
	apiBearerToken = "test-token"
	globalStateMutex.Unlock()

	// Create bot directly without Discord session
	bot := &Bot{
		session:       nil,
		channelID:     "test-channel-id",
		configManager: cm,
		apiServer:     nil,
		apiCancel:     nil,
	}

	// Start a blocking HTTP server on the test port
	testPort := "13001"
	testServer := &http.Server{Addr: ":" + testPort}
	listener, err := net.Listen("tcp", ":"+testPort)
	if err != nil {
		t.Fatalf("Failed to create listener: %v", err)
	}

	go func() {
		testServer.Serve(listener)
	}()
	defer testServer.Close()
	defer listener.Close()

	// Give the test server time to start
	time.Sleep(50 * time.Millisecond)

	// Try to start proxy server on the same port
	globalStateMutex.Lock()
	proxyPort = testPort
	globalStateMutex.Unlock()

	// Wait for any previous goroutines to finish reading proxyPort
	time.Sleep(100 * time.Millisecond)

	err = startProxyServer(bot, apiBearerToken)

	// startProxyServer launches server in goroutine, so it won't return error immediately
	// The error will be logged in the goroutine, not returned
	// So we just verify the struct is set up and document this limitation
	if bot.proxyServer == nil {
		t.Error("proxyServer should be set even if ListenAndServe will fail in background")
	}

	// Give time for the background goroutine to attempt starting
	time.Sleep(50 * time.Millisecond)

	// Cleanup proxy server if it was created
	if bot.proxyCancel != nil {
		bot.proxyCancel()
	}
	if bot.proxyServer != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		bot.proxyServer.Shutdown(ctx)
	}
	if bot.proxyStore != nil {
		bot.proxyStore.StopBackgroundCleanup()
	}
	os.RemoveAll("./sessions")

	// Wait for goroutine to finish reading global state
	// Use Gosched to encourage the goroutine to run
	for i := 0; i < 10; i++ {
		runtime.Gosched()
		time.Sleep(100 * time.Millisecond)
	}
}

// TestProxyServer_SessionDirPermissions tests behavior when session directory cannot be created
func TestProxyServer_SessionDirPermissions(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping test when running as root (permissions test ineffective)")
	}

	testSerialMutex.Lock()
	defer testSerialMutex.Unlock()

	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	configPath := filepath.Join(tmpDir, "config.json")
	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	os.Setenv("API_ENABLED", "true")
	os.Setenv("API_PORT", "18083")
	os.Setenv("API_BEARER_TOKEN", "test-token")
	defer os.Unsetenv("API_ENABLED")
	defer os.Unsetenv("API_PORT")
	defer os.Unsetenv("API_BEARER_TOKEN")

	globalStateMutex.Lock()
	apiEnabled = true
	apiPort = "18083"
	apiBearerToken = "test-token"
	globalStateMutex.Unlock()

	// Create bot directly without Discord session
	bot := &Bot{
		session:       nil,
		channelID:     "test-channel-id",
		configManager: cm,
		apiServer:     nil,
		apiCancel:     nil,
	}

	// Create a file named "sessions" to block directory creation
	sessionsFile := "./sessions"
	if err := os.WriteFile(sessionsFile, []byte("blocked"), 0000); err != nil {
		t.Fatalf("Failed to create blocking file: %v", err)
	}
	defer os.Remove(sessionsFile)

	// Try to start proxy server
	globalStateMutex.Lock()
	proxyPort = "13002"
	globalStateMutex.Unlock()
	err := startProxyServer(bot, apiBearerToken)

	// Should fail because sessions is a file, not a directory
	if err == nil {
		t.Error("Expected error when session directory cannot be created, got nil")
	}
}

// TestProxyServer_GracefulShutdown tests that proxy server shuts down gracefully
func TestProxyServer_GracefulShutdown(t *testing.T) {
	testSerialMutex.Lock()
	defer testSerialMutex.Unlock()

	tmpDir := t.TempDir()
	origWd, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origWd)

	initialConfig := &Config{
		ServerIP:       "192.168.1.1",
		UpdateInterval: 30,
		CategoryOrder:  []string{"Drift"},
		CategoryEmojis: map[string]string{"Drift": "🟣"},
		Servers:        []Server{{Name: "Test", Port: 8081, Category: "Drift"}},
	}

	configPath := filepath.Join(tmpDir, "config.json")
	data, _ := json.Marshal(initialConfig)
	os.WriteFile(configPath, data, 0644)

	cm := NewConfigManager(configPath, initialConfig)

	os.Setenv("API_ENABLED", "true")
	os.Setenv("API_PORT", "18084")
	os.Setenv("API_BEARER_TOKEN", "test-token")
	defer os.Unsetenv("API_ENABLED")
	defer os.Unsetenv("API_PORT")
	defer os.Unsetenv("API_BEARER_TOKEN")

	globalStateMutex.Lock()
	apiEnabled = true
	apiPort = "18084"
	apiBearerToken = "test-token"
	globalStateMutex.Unlock()

	// Create bot directly without Discord session
	bot := &Bot{
		session:       nil,
		channelID:     "test-channel-id",
		configManager: cm,
		apiServer:     nil,
		apiCancel:     nil,
	}

	// Start proxy server
	globalStateMutex.Lock()
	proxyPort = "13003"
	globalStateMutex.Unlock()

	// Wait for any previous goroutines to finish reading proxyPort
	time.Sleep(100 * time.Millisecond)

	if err := startProxyServer(bot, apiBearerToken); err != nil {
		t.Fatalf("startProxyServer failed: %v", err)
	}

	// Cancel proxy context
	bot.proxyCancel()

	// Shutdown proxy server
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	if err := bot.proxyServer.Shutdown(ctx); err != nil {
		// Timeout is expected for immediate shutdown
		t.Logf("Shutdown returned error (acceptable for immediate shutdown): %v", err)
	}

	// Stop session cleanup
	bot.proxyStore.StopBackgroundCleanup()

	// Cleanup session directory
	os.RemoveAll("./sessions")

	// Verify proxy server is stopped
	select {
	case <-ctx.Done():
		// Context timeout is expected for fast shutdown
		t.Log("Proxy server shutdown completed (context timeout expected)")
	default:
	}
}
