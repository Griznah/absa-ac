package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/bombom/absa-ac/api"
	"github.com/bombom/absa-ac/pkg/proxy"
	"github.com/bwmarrin/discordgo"
)

// ================= SECURITY: STRONG TOKEN ENFORCEMENT =================
// isStrongToken returns true if token meets strength requirements for REST API auth
// - At least 32 chars
// - Not a default/demo/test value
// - Not all the same character
func isStrongToken(token string) bool {
	if len(token) < 32 {
		return false
	}
	lowers := []string{"changeme-required", "changeme", "test", "token", "example", "123456", strings.Repeat("a", len(token)), strings.Repeat("1", len(token))}
	t := strings.ToLower(token)
	for _, lw := range lowers {
		if t == lw {
			return false
		}
	}
	allSame := true
	for i := range token {
		if token[i] != token[0] {
			allSame = false
			break
		}
	}
	if allSame {
		return false
	}
	return true
}

// ================= SECRET REDACTION =================
// RedactSecrets replaces secrets/patterns in logs with [REDACTED]
func RedactSecrets(s string) string {
	patterns := []string{
		`(?i)(api[_-]?key|token|secret|bearer)["'=: ]+([a-zA-Z0-9\-_.:]+)`, // API_KEY=xxx, Bearer ...
		`(?i)(password)["'=: ]+([a-zA-Z0-9\-_.:]+)`,                        // password fields
	}
	for _, pat := range patterns {
		re := regexp.MustCompile(pat)
		s = re.ReplaceAllStringFunc(s, func(m string) string {
			// Only redact value part, not the key
			parts := strings.SplitN(m, "=", 2)
			if len(parts) == 2 {
				return parts[0] + "=[REDACTED]"
			}
			colon := strings.SplitN(m, ":", 2)
			if len(colon) == 2 {
				return colon[0] + ": [REDACTED]"
			}
			space := strings.SplitN(m, " ", 2)
			if len(space) == 2 {
				return space[0] + " [REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return s
}

type redactingWriter struct{ underlying io.Writer }

func (rw *redactingWriter) Write(p []byte) (int, error) {
	redacted := RedactSecrets(string(p))
	_, err := rw.underlying.Write([]byte(redacted))
	return len(p), err
}

// Call this at program start: makes all log.Print log.Printf secrets-safe
func InstallRedactingLogger() {
	log.SetOutput(&redactingWriter{underlying: os.Stderr})
}

// ================= ENV LOADING =================

// loadEnv reads a .env file and sets environment variables
// Only sets variables that aren't already set in the environment
func loadEnv() error {
	envPath := ".env"

	file, err := os.Open(envPath)
	if err != nil {
		if os.IsNotExist(err) {
			// .env file is optional, not an error
			return nil
		}
		return fmt.Errorf("failed to open .env file: %w", err)
	}
	defer file.Close()

	log.Printf("Loading environment variables from: %s", envPath)

	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		// Parse KEY=VALUE
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			log.Printf("Warning: invalid line %d in .env, skipping: %s", lineNum, line)
			continue
		}

		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])

		// Remove quotes if present
		if strings.HasPrefix(value, "\"") && strings.HasSuffix(value, "\"") {
			value = strings.Trim(value, "\"")
		} else if strings.HasPrefix(value, "'") && strings.HasSuffix(value, "'") {
			value = strings.Trim(value, "'")
		}

		// Only set if not already in environment
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				log.Printf("Warning: failed to set %s: %v", key, err)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("error reading .env file: %w", err)
	}

	return nil
}

// ================= CONFIG =================

var (
	// API configuration flags (read from environment, stored in Bot struct)
	apiEnabled     bool
	apiPort        string
	apiBearerToken string
	apiCorsOrigins string

	// Proxy configuration flags (read from environment)
	proxyEnabled         bool
	proxyPort            string
	proxyUpstreamTimeout time.Duration
)

type Server struct {
	Name     string
	IP       string
	Port     int
	Category string
}

// ConfigManager provides thread-safe access to configuration with dynamic reload
// Uses atomic.Value for lock-free reads (critical for performance during server polling)
// Uses sync.RWMutex to serialize reload operations (rare writes vs frequent reads)
// Debounces rapid file writes to prevent excessive reload attempts during editor save operations
type ConfigManager struct {
	config        atomic.Value // stores *Config
	configPath    string
	lastModTime   time.Time
	mu            sync.RWMutex
	debounceTimer *time.Timer // Timer for debouncing rapid file writes
}

// NewConfigManager creates a new ConfigManager with an initial configuration
// Stores initial config in atomic.Value for lock-free access
// Records initial file modification time to detect future changes
func NewConfigManager(configPath string, initial *Config) *ConfigManager {
	cm := &ConfigManager{
		configPath: configPath,
	}
	cm.config.Store(initial)

	// Get initial file modification time
	if modTime, err := cm.getLastModTime(); err == nil {
		cm.lastModTime = modTime
	} else {
		log.Printf("Warning: failed to get initial config mod time: %v", err)
	}

	return cm
}

// GetConfig returns the current configuration (thread-safe, lock-free read)
// atomic.Value.Load() provides zero-copy access without mutex contention
// Multiple goroutines can call this simultaneously during server polling
func (cm *ConfigManager) GetConfig() *Config {
	return cm.config.Load().(*Config)
}

// getLastModTime retrieves the modification time of the config file (changes indicate config modifications requiring reload)
// Returns raw os.Stat error for caller to handle (file not found, permission denied, etc.)
func (cm *ConfigManager) getLastModTime() (time.Time, error) {
	info, err := os.Stat(cm.configPath)
	if err != nil {
		return time.Time{}, err
	}
	return info.ModTime(), nil
}

// checkAndReloadIfNeeded checks if the config file has changed and schedules debounced reload
// Returns nil immediately after scheduling (doesn't wait for reload to complete)
// Debouncing prevents excessive reloads during rapid file writes (e.g., editor save operations)
// Most text editors perform multiple write operations when saving files, causing multiple
// file modification events. Without debouncing, each write would trigger a separate reload.
func (cm *ConfigManager) checkAndReloadIfNeeded() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Check current file modification time
	currentModTime, err := cm.getLastModTime()
	if err != nil {
		return fmt.Errorf("failed to stat config file: %w", err)
	}

	// No change detected
	if currentModTime.Equal(cm.lastModTime) || currentModTime.Before(cm.lastModTime) {
		return nil
	}

	// File has changed, schedule debounced reload
	// Stop any existing timer to reset debounce window on each new write
	if cm.debounceTimer != nil {
		cm.debounceTimer.Stop()
	}

	// Create new timer that fires 100ms after last write
	// If another write occurs within 100ms, this timer will be stopped and reset
	cm.debounceTimer = time.AfterFunc(100*time.Millisecond, func() {
		// Timer callback runs in separate goroutine
		if err := cm.performReload(); err != nil {
			log.Printf("Config reload failed: %v", err)
		}
	})

	return nil
}

// performReload executes the actual config reload (load, validate, atomic swap)
// Called by debounce timer after writes have settled
// Logs errors but never crashes - bot continues with old config on reload failure
func (cm *ConfigManager) performReload() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Double-check modification time (file might have changed again during debounce)
	currentModTime, err := cm.getLastModTime()
	if err != nil {
		return fmt.Errorf("failed to stat config file: %w", err)
	}

	// If file hasn't changed since last reload, skip
	if currentModTime.Equal(cm.lastModTime) {
		return nil
	}

	log.Printf("Config file modified, attempting reload from: %s", cm.configPath)

	// Load new config
	newCfg, err := loadConfig(cm.configPath)
	if err != nil {
		return fmt.Errorf("failed to read config: %w", err)
	}

	// Validate new config
	if err := validateConfigStructSafeRuntime(newCfg); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Initialize server IPs from global ServerIP setting.
	// Must complete before atomic swap; readers see config via atomic.Value without locks.
	initializeServerIPs(newCfg)

	// Success: atomically swap config and update mod time
	cm.config.Store(newCfg)
	cm.lastModTime = currentModTime
	log.Println("Config reloaded successfully")

	return nil
}

// Cleanup stops the debounce timer and releases resources
// Called during bot shutdown to prevent timer callbacks after shutdown
// Safe to call multiple times (idempotent)
func (cm *ConfigManager) Cleanup() {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Stop timer if running
	if cm.debounceTimer != nil {
		cm.debounceTimer.Stop()
		cm.debounceTimer = nil
	}
}

// WriteConfig writes a complete new configuration to disk with backup and atomic write
// Creates backup file before modifying, writes to temp file, then atomic rename
// Returns error if validation fails (config unchanged on disk)
// Triggers reload via file mtime change on success
// Thread-safe: serializes concurrent writes using RWMutex write lock
func (cm *ConfigManager) WriteConfig(newConfig *Config) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Validate new config before making any changes
	if err := validateConfigStructSafeRuntime(newConfig); err != nil {
		return fmt.Errorf("config validation failed: %w", err)
	}

	// Initialize server IPs before writing (must happen before atomic swap)
	initializeServerIPs(newConfig)

	// Create backup before modifying
	if err := cm.createBackup(); err != nil {
		return fmt.Errorf("backup creation failed: %w", err)
	}

	// Serialize config to JSON
	data, err := json.MarshalIndent(newConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON encoding failed: %w", err)
	}

	// Atomic write: temp file + rename
	if err := cm.atomicWrite(data); err != nil {
		return fmt.Errorf("atomic write failed: %w", err)
	}

	// Update mod time to trigger reload (must hold lock until complete)
	// Moving touchConfigFile inside lock prevents race with reload
	if err := cm.touchConfigFile(); err != nil {
		return fmt.Errorf("failed to update config mod time: %w", err)
	}

	return nil
}

// UpdateConfig applies a partial configuration update by merging with existing config
// Reads current config, merges partial changes using deep merge, then writes
// Returns error if validation fails or merge cannot be performed
// Triggers reload via file mtime change on success
// Thread-safe: serializes concurrent writes using RWMutex write lock
func (cm *ConfigManager) UpdateConfig(partial map[string]interface{}) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	// Get current config as baseline
	current := cm.GetConfig()

	// Deep merge partial config with current
	merged, err := deepMergeConfig(current, partial)
	if err != nil {
		return fmt.Errorf("config merge failed: %w", err)
	}

	// Validate merged config
	if err := validateConfigStructSafeRuntime(merged); err != nil {
		return fmt.Errorf("merged config validation failed: %w", err)
	}

	// Initialize server IPs
	initializeServerIPs(merged)

	// Create backup
	if err := cm.createBackup(); err != nil {
		return fmt.Errorf("backup creation failed: %w", err)
	}

	// Serialize merged config
	data, err := json.MarshalIndent(merged, "", "  ")
	if err != nil {
		return fmt.Errorf("JSON encoding failed: %w", err)
	}

	// Atomic write
	if err := cm.atomicWrite(data); err != nil {
		return fmt.Errorf("atomic write failed: %w", err)
	}

	// Update mod time
	if err := cm.touchConfigFile(); err != nil {
		log.Printf("Warning: failed to update config mod time: %v", err)
	}

	return nil
}

// createBackup creates a backup of the current config file with rotation
// Implements 3-version backup rotation: .backup.1 (latest) -> .backup.2 -> .backup.3 (oldest)
// Backup path is config.json.backup in same directory as config file
// Returns nil if config file doesn't exist yet (first-time write)
func (cm *ConfigManager) createBackup() error {
	// Read existing config file
	data, err := os.ReadFile(cm.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// No existing config to backup (first write)
			return nil
		}
		return err
	}

	// Implement backup rotation: .backup.1 (latest) -> .backup.2 -> .backup.3 (oldest)
	backupPaths := []string{
		cm.configPath + ".backup.3", // Oldest - deleted first
		cm.configPath + ".backup.2",
		cm.configPath + ".backup.1",
		cm.configPath + ".backup", // Current backup
	}

	// Rotate: delete .backup.3 if exists
	if _, err := os.Stat(backupPaths[0]); err == nil {
		if err := os.Remove(backupPaths[0]); err != nil {
			return fmt.Errorf("failed to remove %s: %w", backupPaths[0], err)
		}
	}

	// Rotate: .backup.2 -> .backup.3
	if _, err := os.Stat(backupPaths[1]); err == nil {
		if err := os.Rename(backupPaths[1], backupPaths[0]); err != nil {
			return fmt.Errorf("failed to rename %s -> %s: %w", backupPaths[1], backupPaths[0], err)
		}
	}

	// Rotate: .backup.1 -> .backup.2
	if _, err := os.Stat(backupPaths[2]); err == nil {
		if err := os.Rename(backupPaths[2], backupPaths[1]); err != nil {
			return fmt.Errorf("failed to rename %s -> %s: %w", backupPaths[2], backupPaths[1], err)
		}
	}

	// Current -> .backup.1
	if _, err := os.Stat(backupPaths[3]); err == nil {
		if err := os.Rename(backupPaths[3], backupPaths[2]); err != nil {
			return fmt.Errorf("failed to rename %s -> %s: %w", backupPaths[3], backupPaths[2], err)
		}
	}

	// Write current config to .backup
	if err := os.WriteFile(backupPaths[3], data, 0644); err != nil {
		return fmt.Errorf("failed to write backup: %w", err)
	}

	log.Printf("Config backup rotated: %s (latest of 3 versions)", backupPaths[3])
	return nil
}

// atomicWrite writes data to config file using atomic temp-file-then-rename pattern
// Prevents partial writes during crash/power loss
// Write to temp file, then rename over original (atomic on POSIX systems)
func (cm *ConfigManager) atomicWrite(data []byte) error {
	// Create temp file in same directory as target
	dir := filepath.Dir(cm.configPath)
	tmpFile, err := os.CreateTemp(dir, "config.json.*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmpFile.Name()

	// Ensure temp file is cleaned up on error
	defer func() {
		if tmpFile != nil {
			tmpFile.Close()
			os.Remove(tmpPath)
			log.Printf("Cleaned up temp file: %s", tmpPath)
		}
	}()

	// Write data to temp file
	if _, err := tmpFile.Write(data); err != nil {
		return err
	}

	// Sync to disk
	if err := tmpFile.Sync(); err != nil {
		return err
	}

	// Close temp file before rename
	if err := tmpFile.Close(); err != nil {
		return err
	}
	tmpFile = nil // Prevent defer cleanup (file successfully closed)

	// Atomic rename over target
	if err := os.Rename(tmpPath, cm.configPath); err != nil {
		// On rename error, tmpFile already closed but defer won't cleanup
		// Manually clean up the orphaned temp file
		os.Remove(tmpPath)
		return err
	}

	log.Printf("Config written atomically to: %s", cm.configPath)
	return nil
}

// touchConfigFile updates the modification time of the config file
// This triggers the reload mechanism (mtime-based polling)
func (cm *ConfigManager) touchConfigFile() error {
	now := time.Now()
	return os.Chtimes(cm.configPath, now, now)
}

// WriteConfigAny is an adapter for the API interface that accepts any
// Converts the input to *Config and calls WriteConfig
func (cm *ConfigManager) WriteConfigAny(cfg any) error {
	// Convert map to Config struct
	config, err := anyToConfig(cfg)
	if err != nil {
		return err
	}
	return cm.WriteConfig(config)
}

// GetConfigAny returns the current config as any (for API compatibility)
func (cm *ConfigManager) GetConfigAny() any {
	return cm.GetConfig()
}

// deepMergeConfig merges a partial config map with an existing Config struct
// Performs deep merge for nested structures (servers, category_emojis)
// Returns a new Config struct with merged values
func deepMergeConfig(base *Config, partial map[string]interface{}) (*Config, error) {
	// Marshal base config to JSON
	baseData, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}

	// Unmarshal base into map
	baseMap := make(map[string]interface{})
	if err := json.Unmarshal(baseData, &baseMap); err != nil {
		return nil, err
	}

	// Deep merge partial into base
	merged := mergeMaps(baseMap, partial)

	// Marshal merged map back to JSON
	mergedData, err := json.Marshal(merged)
	if err != nil {
		return nil, err
	}

	// Unmarshal into Config struct
	var result Config
	if err := json.Unmarshal(mergedData, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// mergeMaps recursively merges source map into destination map
// Handles nested maps (like category_emojis) and arrays (like servers)
func mergeMaps(dest, src map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	// Copy dest first
	for k, v := range dest {
		result[k] = v
	}

	// Merge src into result
	for k, v := range src {
		if destVal, exists := result[k]; exists {
			// Both exist - check if both are maps
			destMap, destIsMap := destVal.(map[string]interface{})
			srcMap, srcIsMap := v.(map[string]interface{})

			if destIsMap && srcIsMap {
				// Recursive merge
				result[k] = mergeMaps(destMap, srcMap)
			} else if k == "servers" {
				// Special handling for servers array: merge by name instead of replacing
				result[k] = mergeServerArrays(destVal, v)
			} else {
				// Override with src value
				result[k] = v
			}
		} else {
			// New key in src
			result[k] = v
		}
	}

	return result
}

// mergeServerArrays merges server arrays by name instead of replacing
// Servers from partial update existing servers by name, new servers are appended
// Preserves all dest servers unless explicitly updated/removed in src
func mergeServerArrays(dest, src interface{}) interface{} {
	destArray, destOk := dest.([]interface{})
	srcArray, srcOk := src.([]interface{})

	// If either is not an array, replace (fallback to original behavior)
	if !destOk || !srcOk {
		return src
	}

	// Build map of existing servers by name and track updated names
	destServers := make(map[string]map[string]interface{})
	updatedNames := make(map[string]bool)
	for _, s := range destArray {
		if serverMap, ok := s.(map[string]interface{}); ok {
			if name, hasName := serverMap["name"].(string); hasName {
				destServers[name] = serverMap
			}
		}
	}

	// Start with all dest servers (preserves servers not mentioned in src)
	result := make([]interface{}, 0, len(destArray))
	for _, s := range destArray {
		serverMap, ok := s.(map[string]interface{})
		if !ok {
			result = append(result, s)
			continue
		}
		if _, hasName := serverMap["name"].(string); hasName {
			result = append(result, s)
		} else {
			result = append(result, s)
		}
	}

	// Merge src servers: update existing, append new, preserve order from src
	for _, s := range srcArray {
		serverMap, ok := s.(map[string]interface{})
		if !ok {
			// Non-map entry, append as-is (edge case)
			result = append(result, s)
			continue
		}

		name, hasName := serverMap["name"].(string)
		if !hasName {
			// No name field, append as new (can't match existing)
			result = append(result, s)
			continue
		}

		// Check if server exists in dest
		if existingServer, found := destServers[name]; found {
			if !updatedNames[name] {
				// First update: replace dest entry with merged version
				// Find and replace in result
				for i, r := range result {
					if rMap, ok := r.(map[string]interface{}); ok {
						if rName, ok := rMap["name"].(string); ok && rName == name {
							result[i] = mergeMaps(existingServer, serverMap)
							updatedNames[name] = true
							break
						}
					}
				}
			}
			// Already updated, skip duplicates in src
		} else {
			// New server, append
			result = append(result, s)
		}
	}

	return result
}

// anyToConfig converts any value to a *Config struct
// Handles both *Config and map[string]interface{} inputs
func anyToConfig(cfg any) (*Config, error) {
	switch v := cfg.(type) {
	case *Config:
		return v, nil
	case map[string]interface{}:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal config: %w", err)
		}
		var result Config
		if err := json.Unmarshal(data, &result); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
		return &result, nil
	default:
		return nil, fmt.Errorf("unsupported config type: %T", cfg)
	}
}

// validateConfigStructSafeRuntime is a non-fatal version of validateConfigStruct for runtime reload
// Returns error instead of calling log.Fatalf, allowing bot to continue with old config on validation failure
// Critical for dynamic reload: invalid config must not terminate running bot
// Same validation rules as validateConfigStruct, but safe for runtime use
func validateConfigStructSafeRuntime(cfg *Config) error {
	if cfg.ServerIP == "" {
		return fmt.Errorf("server_ip cannot be empty")
	}

	if cfg.UpdateInterval < 1 {
		return fmt.Errorf("update_interval must be at least 1 second (got: %d)", cfg.UpdateInterval)
	}

	if len(cfg.CategoryOrder) == 0 {
		return fmt.Errorf("category_order cannot be empty")
	}

	// Build category lookup map for O(1) validation
	categoryMap := make(map[string]bool)
	for _, cat := range cfg.CategoryOrder {
		categoryMap[cat] = true
	}

	// Validate all categories have emojis
	for _, cat := range cfg.CategoryOrder {
		if _, exists := cfg.CategoryEmojis[cat]; !exists {
			return fmt.Errorf("category '%s' is in category_order but missing from category_emojis", cat)
		}
	}

	// Validate servers
	for i, server := range cfg.Servers {
		if server.Name == "" {
			return fmt.Errorf("server at index %d has empty name", i)
		}

		if server.Port < 1 || server.Port > 65535 {
			return fmt.Errorf("server '%s' has invalid port: %d (valid range: 1-65535)", server.Name, server.Port)
		}

		if server.Category == "" {
			return fmt.Errorf("server '%s' has empty category", server.Name)
		}

		// Validate server category exists in CategoryOrder
		if !categoryMap[server.Category] {
			return fmt.Errorf("server '%s' has category '%s' which is not defined in category_order", server.Name, server.Category)
		}
	}

	return nil
}

// ================= TYPES =================

type ServerInfo struct {
	Name       string
	Category   string
	Map        string
	Players    string // "X/Y" format
	NumPlayers int    // For sorting/totaling (-1 = offline)
	IP         string
	Port       int
}

type Bot struct {
	session       *discordgo.Session
	channelID     string
	configManager *ConfigManager
	serverMessage *discordgo.Message
	messageMutex  sync.RWMutex

	// API server (optional - nil if disabled)
	apiServer *api.Server
	apiCancel context.CancelFunc

	// Proxy server (optional - nil if disabled)
	proxyServer *http.Server
	proxyCancel context.CancelFunc
	proxyStore  *proxy.SessionStore
}

// Config holds application configuration loaded from config.json
type Config struct {
	ServerIP       string            `json:"server_ip"`
	UpdateInterval int               `json:"update_interval"`
	CategoryOrder  []string          `json:"category_order"`
	CategoryEmojis map[string]string `json:"category_emojis"`
	Servers        []Server          `json:"servers"`
}

// loadConfig reads and parses config.json with fallback logic
func loadConfig(providedPath string) (*Config, error) {
	// If explicitly provided, only try that path
	if providedPath != "" {
		log.Printf("Loading config from provided path: %s", providedPath)
		data, err := os.ReadFile(providedPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config from %s: %w", providedPath, err)
		}

		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config from %s: %w", providedPath, err)
		}

		log.Printf("Successfully loaded config from: %s", providedPath)
		return &cfg, nil
	}

	// Otherwise, try default locations in priority order
	wd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to get working directory: %w", err)
	}

	defaultPaths := []string{
		"/data/config.json",
		filepath.Join(wd, "config.json"),
	}

	var errors []string
	for _, path := range defaultPaths {
		log.Printf("Attempting to load config from: %s", path)

		data, err := os.ReadFile(path)
		if err != nil {
			errors = append(errors, fmt.Sprintf("  %s: %v", path, err))
			continue
		}

		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return nil, fmt.Errorf("failed to parse config from %s: %w", path, err)
		}

		log.Printf("Successfully loaded config from: %s", path)
		return &cfg, nil
	}

	return nil, fmt.Errorf("failed to load config from any default location:\n%s", strings.Join(errors, "\n"))
}

// getConfigPath determines the actual config file path that loadConfig uses
// Matches loadConfig's fallback logic exactly: provided path -> /data/config.json -> ./config.json
func getConfigPath(providedPath string) string {
	// If explicitly provided, return that path (matches loadConfig's provided-path mode)
	if providedPath != "" {
		return providedPath
	}

	// Otherwise, try default locations in same priority order as loadConfig's fallback mode
	wd, err := os.Getwd()
	if err != nil {
		// If we can't get working directory, config load fails
		// Return empty string to signal error condition
		return ""
	}

	defaultPaths := []string{
		"/data/config.json",
		filepath.Join(wd, "config.json"),
	}

	// Return first existing path (matches loadConfig's fallback priority order)
	for _, path := range defaultPaths {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// No config file found - this matches loadConfig's error return when all paths fail
	// Empty string signals that no valid config path exists
	return ""
}

// validateConfigStruct performs fail-fast validation on loaded config
func validateConfigStruct(cfg *Config) {
	// Validate ServerIP
	if cfg.ServerIP == "" {
		log.Fatalf("Configuration error: server_ip cannot be empty")
	}

	// Validate UpdateInterval (minimum 1 second)
	if cfg.UpdateInterval < 1 {
		log.Fatalf("Configuration error: update_interval must be at least 1 second (got: %d)", cfg.UpdateInterval)
	}

	// Validate CategoryOrder
	if len(cfg.CategoryOrder) == 0 {
		log.Fatalf("Configuration error: category_order cannot be empty")
	}

	// Build category lookup map for O(1) validation
	categoryMap := make(map[string]bool)
	for _, cat := range cfg.CategoryOrder {
		categoryMap[cat] = true
	}

	// Validate all categories have emojis
	for _, cat := range cfg.CategoryOrder {
		if _, exists := cfg.CategoryEmojis[cat]; !exists {
			log.Fatalf("Configuration error: category '%s' is in category_order but missing from category_emojis", cat)
		}
	}

	// Validate servers
	for i, server := range cfg.Servers {
		if server.Name == "" {
			log.Fatalf("Configuration error: server at index %d has empty name", i)
		}

		if server.Port < 1 || server.Port > 65535 {
			log.Fatalf("Configuration error: server '%s' has invalid port: %d (valid range: 1-65535)", server.Name, server.Port)
		}

		if server.Category == "" {
			log.Fatalf("Configuration error: server '%s' has empty category", server.Name)
		}

		// Validate server category exists in CategoryOrder
		if !categoryMap[server.Category] {
			log.Fatalf("Configuration error: server '%s' has category '%s' which is not defined in category_order", server.Name, server.Category)
		}
	}

	log.Printf("Configuration validated: %d servers across %d categories", len(cfg.Servers), len(cfg.CategoryOrder))
}

// initializeServerIPs sets the IP field for each server to the global ServerIP value.
// This is called after config load to populate server IPs from the centralized ServerIP setting,
// avoiding redundancy in the config file while maintaining per-server IP fields for URL construction.
func initializeServerIPs(cfg *Config) {
	for i := range cfg.Servers {
		cfg.Servers[i].IP = cfg.ServerIP
	}
}

// ================= HTTP CLIENT =================

var httpClient = &http.Client{
	Timeout: 2 * time.Second,
}

func fetchAllServers(cfgManager *ConfigManager) []ServerInfo {
	cfg := cfgManager.GetConfig()
	var wg sync.WaitGroup
	infos := make([]ServerInfo, len(cfg.Servers))
	mu := sync.Mutex{}

	for i, server := range cfg.Servers {
		wg.Add(1)
		go func(idx int, s Server) {
			defer wg.Done()
			info := fetchServerInfo(s)

			mu.Lock()
			infos[idx] = info
			mu.Unlock()
		}(i, server)
	}

	wg.Wait()
	return infos
}

func fetchServerInfo(server Server) ServerInfo {
	url := fmt.Sprintf("http://%s:%d/info", server.IP, server.Port)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return offlineServerInfo(server)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return offlineServerInfo(server)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return offlineServerInfo(server)
	}

	var data struct {
		Clients    int    `json:"clients"`
		MaxClients int    `json:"maxclients"`
		Track      string `json:"track"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return offlineServerInfo(server)
	}

	trackName := filepath.Base(data.Track)
	if trackName == "." || trackName == "" {
		trackName = "Unknown"
	}

	return ServerInfo{
		Name:       server.Name,
		Category:   server.Category,
		Map:        trackName,
		Players:    fmt.Sprintf("%d/%d", data.Clients, data.MaxClients),
		NumPlayers: data.Clients,
		IP:         server.IP,
		Port:       server.Port,
	}
}

func offlineServerInfo(server Server) ServerInfo {
	return ServerInfo{
		Name:       server.Name,
		Category:   server.Category,
		Map:        "Offline",
		Players:    "0/0",
		NumPlayers: -1, // Negative indicates offline
		IP:         server.IP,
		Port:       server.Port,
	}
}

// ================= DISCORD INTEGRATION =================

func buildEmbed(infos []ServerInfo, cfgManager *ConfigManager) *discordgo.MessageEmbed {
	cfg := cfgManager.GetConfig()

	// Group servers and calculate totals
	grouped := make(map[string][]ServerInfo)
	categoryTotals := make(map[string]int)
	totalPlayers := 0

	for _, info := range infos {
		grouped[info.Category] = append(grouped[info.Category], info)
		if info.NumPlayers > 0 {
			categoryTotals[info.Category] += info.NumPlayers
			totalPlayers += info.NumPlayers
		}
	}

	// Build embed
	embed := &discordgo.MessageEmbed{
		Title:       "ABSA Official Servers",
		Description: fmt.Sprintf(":bust_in_silhouette: **Total Players:** %d", totalPlayers),
		Color:       0x00FF00, // Green
		Thumbnail: &discordgo.MessageEmbedThumbnail{
			URL: "https://upload.wikimedia.org/wikipedia/commons/thumb/d/d9/Flag_of_Norway.svg/320px-Flag_of_Norway.svg.png",
		},
		Image: &discordgo.MessageEmbedImage{
			URL: fmt.Sprintf("http://%s/images/logo.png", cfg.ServerIP),
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Updates every %d seconds", cfg.UpdateInterval),
		},
	}

	// Append fields by category
	for _, category := range cfg.CategoryOrder {
		emoji := cfg.CategoryEmojis[category]
		total := categoryTotals[category]

		// Category header field
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   fmt.Sprintf("%s **%s Servers — %d players**", emoji, category, total),
			Value:  "\u200b", // Zero-width space
			Inline: false,
		})

		// Individual server fields
		for _, info := range grouped[category] {
			statusEmoji := ":green_circle:"
			if info.NumPlayers < 0 {
				statusEmoji = ":red_circle:"
			}

			joinURL := fmt.Sprintf(
				"https://acstuff.club/s/q:race/online/join?ip=%s&httpPort=%d",
				info.IP, info.Port,
			)

			embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
				Name: fmt.Sprintf("%s %s", statusEmoji, info.Name),
				Value: fmt.Sprintf(
					"**Map:** %s\n**Players:** %s\n[Join Server](%s)",
					info.Map, info.Players, joinURL,
				),
				Inline: false,
			})
		}

		// Spacer after category
		embed.Fields = append(embed.Fields, &discordgo.MessageEmbedField{
			Name:   "\u200b",
			Value:  "\u200b",
			Inline: false,
		})
	}

	return embed
}

func (b *Bot) getStatusMessage() *discordgo.Message {
	b.messageMutex.RLock()
	defer b.messageMutex.RUnlock()
	return b.serverMessage
}

func (b *Bot) setStatusMessage(msg *discordgo.Message) {
	b.messageMutex.Lock()
	defer b.messageMutex.Unlock()
	b.serverMessage = msg
}

func (b *Bot) updateStatusMessage(embed *discordgo.MessageEmbed) error {
	existing := b.getStatusMessage()

	var msg *discordgo.Message
	var err error

	if existing == nil {
		// Create new message
		msg, err = b.session.ChannelMessageSendEmbed(b.channelID, embed)
		if err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}
		b.setStatusMessage(msg)
		log.Println("Initial status message posted")
	} else {
		// Edit existing message
		msg, err = b.session.ChannelMessageEditComplex(
			&discordgo.MessageEdit{
				ID:      existing.ID,
				Channel: b.channelID,
				Embed:   embed,
			},
		)
		if err != nil {
			// Message might have been deleted - recreate
			if restError, ok := err.(*discordgo.RESTError); ok && restError.Response != nil && restError.Response.StatusCode == 404 {
				msg, err = b.session.ChannelMessageSendEmbed(b.channelID, embed)
				if err != nil {
					return fmt.Errorf("failed to recreate message: %w", err)
				}
				b.setStatusMessage(msg)
				log.Println("Status message recreated (previous was deleted)")
				return nil
			}
			return fmt.Errorf("failed to edit message: %w", err)
		}
		b.setStatusMessage(msg)
		log.Println("Status message updated")
	}

	return nil
}

// ================= EVENT HANDLERS =================

func (b *Bot) onReady(s *discordgo.Session, event *discordgo.Ready) {
	log.Printf("✅ Logged in as %s", s.State.User.Username)

	// Clean up old messages
	if err := b.cleanupOldMessages(); err != nil {
		log.Printf("Warning: cleanup failed: %v", err)
	}

	// Start update loop in background goroutine
	go b.startUpdateLoop()
}

func (b *Bot) cleanupOldMessages() error {
	// Fetch messages (Discord API returns max 100 per request)
	messages, err := b.session.ChannelMessages(b.channelID, 100, "", "", "")
	if err != nil {
		return fmt.Errorf("failed to fetch messages: %w", err)
	}

	botUserID := b.session.State.User.ID
	deletedCount := 0

	for _, msg := range messages {
		if msg.Author.ID == botUserID {
			if err := b.session.ChannelMessageDelete(b.channelID, msg.ID); err != nil {
				log.Printf("Failed to delete message %s: %v", msg.ID, err)
			} else {
				deletedCount++
			}
		}
	}

	log.Printf("Cleaned up %d old bot messages", deletedCount)
	return nil
}

func (b *Bot) registerHandlers() {
	b.session.AddHandler(b.onReady)
}

// ================= UPDATE LOOP =================

func (b *Bot) startUpdateLoop() {
	cfg := b.configManager.GetConfig()
	ticker := time.NewTicker(time.Duration(cfg.UpdateInterval) * time.Second)
	defer ticker.Stop()

	// Immediate first update
	b.performUpdate()

	for range ticker.C {
		// Check for config updates before each update
		if err := b.checkForConfigUpdates(); err != nil {
			log.Printf("Config reload check failed: %v", err)
		}
		b.performUpdate()
	}
}

func (b *Bot) performUpdate() {
	// Fetch all server info concurrently
	infos := fetchAllServers(b.configManager)

	// Build embed
	embed := buildEmbed(infos, b.configManager)

	// Send updated embed to Discord
	if err := b.updateStatusMessage(embed); err != nil {
		log.Printf("Error updating status: %v", err)
	}
}

// ================= BOT CONSTRUCTION =================

func createDiscordSession(token string) (*discordgo.Session, error) {
	session, err := discordgo.New("Bot " + token)
	if err != nil {
		return nil, fmt.Errorf("failed to create Discord session: %w", err)
	}

	session.Identify.Intents = discordgo.IntentGuildMessages

	return session, nil
}

// NewBot creates a new Bot instance with Discord session and optional API server
// Accepts dependencies via constructor injection (enables testing with mocks)
func NewBot(cfgManager *ConfigManager, token, channelID string, apiEnabled bool, apiPort, apiBearerToken, apiCorsOrigins string) (*Bot, error) {
	if token == "" {
		return nil, fmt.Errorf("DISCORD_TOKEN environment variable not set")
	}
	if channelID == "" {
		return nil, fmt.Errorf("CHANNEL_ID environment variable not set")
	}

	session, err := createDiscordSession(token)
	if err != nil {
		return nil, err
	}

	bot := &Bot{
		session:       session,
		channelID:     channelID,
		configManager: cfgManager,
	}

	// Create API server if enabled
	if apiEnabled {
		if apiBearerToken == "" {
			return nil, fmt.Errorf("API_ENABLED=true but API_BEARER_TOKEN is not set")
		}

		// Parse CORS origins
		var corsOrigins []string
		if apiCorsOrigins != "" {
			corsOrigins = strings.Split(apiCorsOrigins, ",")
			// Trim whitespace from each origin
			for i, origin := range corsOrigins {
				corsOrigins[i] = strings.TrimSpace(origin)
			}
		}

		bot.apiServer = api.NewServer(cfgManager, apiPort, apiBearerToken, corsOrigins, log.Default())
		log.Printf("API server configured on port %s with CORS origins: %s", apiPort, apiCorsOrigins)
	}

	return &Bot{
		session:       session,
		channelID:     channelID,
		configManager: cfgManager,
		apiServer:     bot.apiServer,
	}, nil
}

// Start launches the Discord bot and optional API server
// Discord bot connects immediately, API server starts in background goroutine
func (b *Bot) Start() error {
	if err := b.session.Open(); err != nil {
		return fmt.Errorf("failed to open Discord connection: %w", err)
	}

	// Start API server in background if configured
	if b.apiServer != nil {
		ctx, cancel := context.WithCancel(context.Background())
		b.apiCancel = cancel

		go func() {
			if err := b.apiServer.Start(ctx); err != nil {
				log.Printf("API server error: %v", err)
			}
		}()
		log.Println("API server started")
	}

	return nil
}

func (b *Bot) WaitForShutdown() {
	sigchan := make(chan os.Signal, 1)
	signal.Notify(sigchan, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)

	<-sigchan
	log.Println("Shutting down...")

	// Stop proxy server if running
	if b.proxyServer != nil && b.proxyCancel != nil {
		log.Println("Stopping proxy server...")
		b.proxyCancel()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := b.proxyServer.Shutdown(ctx); err != nil {
			log.Printf("Error stopping proxy server: %v", err)
		}
		// Stop session cleanup
		if b.proxyStore != nil {
			b.proxyStore.StopBackgroundCleanup()
		}
	}

	// Stop API server if running
	if b.apiServer != nil && b.apiCancel != nil {
		log.Println("Stopping API server...")
		b.apiCancel()
		if err := b.apiServer.Stop(); err != nil {
			log.Printf("Error stopping API server: %v", err)
		}
	}

	// Cleanup config manager (stop debounce timer)
	if b.configManager != nil {
		b.configManager.Cleanup()
	}

	if err := b.session.Close(); err != nil {
		log.Printf("Error closing Discord session: %v", err)
	}

	log.Println("Shutdown complete")
}

// checkForConfigUpdates wraps checkAndReloadIfNeeded for use in update loop
func (b *Bot) checkForConfigUpdates() error {
	if b.configManager == nil {
		return nil
	}
	return b.configManager.checkAndReloadIfNeeded()
}

// ================= PROXY SERVER =================

// startProxyServer initializes and starts the proxy server in a background goroutine
// Creates session store, sets up routes, and starts HTTP server on configured port
// useHTTPS: controls whether session cookies are marked Secure (true if behind HTTPS termination)
// upstreamTimeout: timeout for upstream API requests (configurable via PROXY_UPSTREAM_TIMEOUT)
// Returns error if session store creation or server startup fails
func startProxyServer(bot *Bot, bearerToken string, useHTTPS bool, upstreamTimeout time.Duration) error {
	// Create session store with file-based persistence
	sessionsDir := "./sessions"
	store, err := proxy.NewSessionStore(sessionsDir)
	if err != nil {
		return fmt.Errorf("failed to create session store: %w", err)
	}
	bot.proxyStore = store

	// Build bot API URL for proxy upstream
	botAPIURL := fmt.Sprintf("http://localhost:%s", apiPort)

	// Setup proxy routes
	mux := http.NewServeMux()

	// Auth endpoints
	mux.HandleFunc("/proxy/login", proxy.LoginHandler(store, botAPIURL, useHTTPS, upstreamTimeout))
	mux.HandleFunc("/proxy/logout", proxy.LogoutHandler(store, useHTTPS))

	// Proxy API endpoints (authenticated with CSRF)
	proxyHandler := proxy.CSRFMiddleware(proxy.ProxyHandler(botAPIURL, store, upstreamTimeout), store)
	mux.Handle("/proxy/api/", proxyHandler)

	// Create HTTP server
	bot.proxyServer = &http.Server{
		Addr:    ":" + proxyPort,
		Handler: mux,
	}

	// Start server in background goroutine
	bot.proxyCancel = func() {}

	go func() {
		log.Printf("Proxy server listening on port %s", proxyPort)
		if err := bot.proxyServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Proxy server error: %v", err)
		}
	}()

	log.Printf("Proxy server started on port %s", proxyPort)
	return nil
}

// ================= MAIN =================

func validateConfig() (token, channelID string, err error) {
	token = os.Getenv("DISCORD_TOKEN")
	if token == "" {
		return "", "", fmt.Errorf("DISCORD_TOKEN environment variable not set")
	}

	channelID = os.Getenv("CHANNEL_ID")
	if channelID == "" {
		return "", "", fmt.Errorf("CHANNEL_ID environment variable not set")
	}

	return token, channelID, nil
}

func checkNotRootUser() {
	if os.Geteuid() == 0 {
		log.Fatalf("SECURITY: Container must not run as root! UID 0 detected. Please rebuild or run with --user/-u flag (see README). Refusing to start.")
	}
}

func checkFilePerm(path string, want os.FileMode, require bool) {
	fi, err := os.Stat(path)
	if err != nil {
		if require {
			log.Fatalf("SECURITY: Required config/file %s missing: %v", path, err)
		} else {
			log.Printf("[WARNING] File/directory %s not found: %v", path, err)
		}
		return
	}
	mode := fi.Mode().Perm()
	if mode != want {
		msg := fmt.Sprintf("SECURITY: %s permissions %o (want %o)", path, mode, want)
		if require {
			log.Fatalf("%s", msg)
		} else {
			log.Printf("[WARNING] %s", msg)
		}
	}
}

func main() {
	InstallRedactingLogger()
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

	checkNotRootUser()

	// Parse command-line flags for config path
	configPath := flag.String("c", "", "Path to config.json file")
	flag.StringVar(configPath, "config", "", "Path to config.json file")
	flag.Parse()

	// Load environment variables from .env file (optional)
	if err := loadEnv(); err != nil {
		log.Printf("Warning: %v", err)
	}

	// Read API configuration from environment
	apiEnabled = os.Getenv("API_ENABLED") == "true"
	apiPort = os.Getenv("API_PORT")
	if apiPort == "" {
		apiPort = "8080" // Default port
	}
	apiBearerToken = os.Getenv("API_BEARER_TOKEN")
	apiCorsOrigins = os.Getenv("API_CORS_ORIGINS")

	// Validate API configuration if enabled
	if apiEnabled {
		if !isStrongToken(apiBearerToken) {
			log.Fatalf(`API_BEARER_TOKEN too weak or missing: must be at least 32 random characters, not default or placeholder.\nGenerate a strong token (command: head -c 48 /dev/urandom | base64) and place in .env as API_BEARER_TOKEN=your_token_here.`)
		}

		allowCorsAny := strings.ToLower(os.Getenv("ALLOW_CORS_ANY")) == "true"
		origins := []string{}
		if apiCorsOrigins != "" {
			for _, o := range strings.Split(apiCorsOrigins, ",") {
				origins = append(origins, strings.TrimSpace(o))
			}
		}
		wildcardPresent := false
		for _, o := range origins {
			if o == "*" {
				wildcardPresent = true
				break
			}
		}
		if wildcardPresent && len(origins) > 1 {
			log.Fatalf("CORS configuration error: wildcard '*' cannot be combined with specific origins. If you want dev mode, set only '*' or only allowlist. See README.md for details.")
		}
		if wildcardPresent && !allowCorsAny {
			log.Fatalf("CORS security error: In production, you MUST provide an explicit allowlist via API_CORS_ORIGINS. Wildcard '*' is forbidden unless ALLOW_CORS_ANY=true for dev/test. See README.md for secure config instructions.")
		}
		if wildcardPresent && allowCorsAny {
			log.Printf("[WARNING] ALLOW_CORS_ANY=true: API will run with wildcard ('*') origins. This is unsafe for production! Only use for local frontend development or testing.")
		}
		log.Printf("API server enabled on port %s with CORS origins: %s", apiPort, apiCorsOrigins)
	}

	// Read proxy configuration from environment
	proxyEnabled = os.Getenv("PROXY_ENABLED") == "true"
	proxyPort = os.Getenv("PROXY_PORT")
	if proxyPort == "" {
		proxyPort = "3000" // Default port
	}
	// Default to HTTPS for production security. Set PROXY_HTTPS=false to disable (not recommended).
	proxyHTTPS := os.Getenv("PROXY_HTTPS") != "false"

	// Parse upstream timeout (default 10 seconds, max 60 seconds)
	const maxUpstreamTimeout = 60 * time.Second
	if timeoutStr := os.Getenv("PROXY_UPSTREAM_TIMEOUT"); timeoutStr != "" {
		var err error
		proxyUpstreamTimeout, err = time.ParseDuration(timeoutStr)
		if err != nil {
			log.Fatalf("Invalid PROXY_UPSTREAM_TIMEOUT value '%s': %v", timeoutStr, err)
		}
		if proxyUpstreamTimeout > maxUpstreamTimeout {
			log.Fatalf("PROXY_UPSTREAM_TIMEOUT exceeds maximum of %v", maxUpstreamTimeout)
		}
	} else {
		proxyUpstreamTimeout = 10 * time.Second
	}

	// Validate proxy configuration if enabled
	if proxyEnabled {
		if apiBearerToken == "" {
			log.Fatalf("PROXY_ENABLED=true but API_BEARER_TOKEN is not set (proxy uses same token)")
		}
		if !apiEnabled {
			log.Fatalf("PROXY_ENABLED=true but API_ENABLED=false (proxy requires bot API)")
		}
		httpsStatus := "HTTP"
		if proxyHTTPS {
			httpsStatus = "HTTPS"
		}
		log.Printf("Proxy server enabled on port %s (%s mode - cookies Secure=%v, upstream timeout=%v)", proxyPort, httpsStatus, proxyHTTPS, proxyUpstreamTimeout)
	}

	token, channelID, err := validateConfig()
	if err != nil {
		log.Fatalf("Configuration error: %v", err)
	}

	// Load and validate config.json
	cfg, err := loadConfig(*configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	validateConfigStruct(cfg)

	// Initialize server IPs before ConfigManager creation (required for lock-free readers via atomic.Value)
	initializeServerIPs(cfg)

	// Create config manager with initial config
	configManager := NewConfigManager(getConfigPath(*configPath), cfg)
	bot, err := NewBot(configManager, token, channelID, apiEnabled, apiPort, apiBearerToken, apiCorsOrigins)
	if err != nil {
		log.Fatalf("Failed to create bot: %v", err)
	}

	bot.registerHandlers()

	if err := bot.Start(); err != nil {
		log.Fatalf("Failed to start bot: %v", err)
	}

	// Start proxy server if enabled
	if proxyEnabled {
		if err := startProxyServer(bot, apiBearerToken, proxyHTTPS, proxyUpstreamTimeout); err != nil {
			log.Printf("Failed to start proxy server: %v", err)
			log.Println("Continuing without proxy server...")
		}
	}

	// Wait for shutdown signal
	bot.WaitForShutdown()
}
