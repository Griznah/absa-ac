package proxy

import (
	"os"
	"testing"
)

func TestConfigLoadFromEnv(t *testing.T) {
	tests := []struct {
		name           string
		envVars        map[string]string
		expectedConfig Config
	}{
		{
			name:    "defaults when no env vars set",
			envVars: map[string]string{},
			expectedConfig: Config{
				Port:        "8080",
				APIURL:      "http://localhost:3001",
				Username:    "",
				Password:    "",
				BearerToken: "",
			},
		},
		{
			name: "custom port and API URL",
			envVars: map[string]string{
				"PROXY_PORT":    "8080",
				"PROXY_API_URL": "http://api.example.com:3001",
			},
			expectedConfig: Config{
				Port:        "8080",
				APIURL:      "http://api.example.com:3001",
				Username:    "",
				Password:    "",
				BearerToken: "",
			},
		},
		{
			name: "all env vars set",
			envVars: map[string]string{
				"PROXY_PORT":        "9000",
				"PROXY_API_URL":     "http://upstream:3001",
				"PROXY_USER":        "admin",
				"PROXY_PASSWORD":    "secretpass123",
				"PROXY_BEARER_TOKEN": "my-token",
			},
			expectedConfig: Config{
				Port:        "9000",
				APIURL:      "http://upstream:3001",
				Username:    "admin",
				Password:    "secretpass123",
				BearerToken: "my-token",
			},
		},
		{
			name: "PROXY_BEARER_TOKEN falls back to API_BEARER_TOKEN",
			envVars: map[string]string{
				"API_BEARER_TOKEN": "fallback-token",
			},
			expectedConfig: Config{
				Port:        "8080",
				APIURL:      "http://localhost:3001",
				Username:    "",
				Password:    "",
				BearerToken: "fallback-token",
			},
		},
		{
			name: "PROXY_BEARER_TOKEN takes precedence over API_BEARER_TOKEN",
			envVars: map[string]string{
				"PROXY_BEARER_TOKEN": "proxy-token",
				"API_BEARER_TOKEN":   "api-token",
			},
			expectedConfig: Config{
				Port:        "8080",
				APIURL:      "http://localhost:3001",
				Username:    "",
				Password:    "",
				BearerToken: "proxy-token",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all relevant env vars first
			os.Unsetenv("PROXY_PORT")
			os.Unsetenv("PROXY_API_URL")
			os.Unsetenv("PROXY_USER")
			os.Unsetenv("PROXY_PASSWORD")
			os.Unsetenv("PROXY_BEARER_TOKEN")
			os.Unsetenv("API_BEARER_TOKEN")

			// Set test env vars
			for key, value := range tt.envVars {
				os.Setenv(key, value)
				defer os.Unsetenv(key)
			}

			cfg := LoadFromEnv()

			if cfg.Port != tt.expectedConfig.Port {
				t.Errorf("Port = %q, want %q", cfg.Port, tt.expectedConfig.Port)
			}
			if cfg.APIURL != tt.expectedConfig.APIURL {
				t.Errorf("APIURL = %q, want %q", cfg.APIURL, tt.expectedConfig.APIURL)
			}
			if cfg.Username != tt.expectedConfig.Username {
				t.Errorf("Username = %q, want %q", cfg.Username, tt.expectedConfig.Username)
			}
			if cfg.Password != tt.expectedConfig.Password {
				t.Errorf("Password = %q, want %q", cfg.Password, tt.expectedConfig.Password)
			}
			if cfg.BearerToken != tt.expectedConfig.BearerToken {
				t.Errorf("BearerToken = %q, want %q", cfg.BearerToken, tt.expectedConfig.BearerToken)
			}
		})
	}
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name        string
		config      Config
		expectError bool
		errorMsg    string
	}{
		{
			name: "valid config",
			config: Config{
				Username:    "admin",
				Password:    "password123",
				BearerToken: "token",
			},
			expectError: false,
		},
		{
			name: "missing username",
			config: Config{
				Username:    "",
				Password:    "password123",
				BearerToken: "token",
			},
			expectError: true,
			errorMsg:    "PROXY_USER is required",
		},
		{
			name: "password too short - 7 chars",
			config: Config{
				Username:    "admin",
				Password:    "1234567",
				BearerToken: "token",
			},
			expectError: true,
			errorMsg:    "PROXY_PASSWORD must be at least 8 characters",
		},
		{
			name: "password too short - empty",
			config: Config{
				Username:    "admin",
				Password:    "",
				BearerToken: "token",
			},
			expectError: true,
			errorMsg:    "PROXY_PASSWORD must be at least 8 characters (got 0)",
		},
		{
			name: "password exactly 8 chars is valid",
			config: Config{
				Username:    "admin",
				Password:    "12345678",
				BearerToken: "token",
			},
			expectError: false,
		},
		{
			name: "missing bearer token",
			config: Config{
				Username:    "admin",
				Password:    "password123",
				BearerToken: "",
			},
			expectError: true,
			errorMsg:    "PROXY_BEARER_TOKEN (or API_BEARER_TOKEN) is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()

			if tt.expectError {
				if err == nil {
					t.Errorf("expected error containing %q, got nil", tt.errorMsg)
				} else if !contains(err.Error(), tt.errorMsg) {
					t.Errorf("error = %q, want to contain %q", err.Error(), tt.errorMsg)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

func TestConfigFailFast(t *testing.T) {
	tests := []struct {
		name     string
		envVars  map[string]string
		hasError bool
	}{
		{
			name: "complete config passes validation",
			envVars: map[string]string{
				"PROXY_USER":        "admin",
				"PROXY_PASSWORD":    "secretpassword",
				"PROXY_BEARER_TOKEN": "token",
			},
			hasError: false,
		},
		{
			name: "missing PROXY_USER fails",
			envVars: map[string]string{
				"PROXY_PASSWORD":     "secretpassword",
				"PROXY_BEARER_TOKEN": "token",
			},
			hasError: true,
		},
		{
			name: "short password fails",
			envVars: map[string]string{
				"PROXY_USER":         "admin",
				"PROXY_PASSWORD":     "short",
				"PROXY_BEARER_TOKEN": "token",
			},
			hasError: true,
		},
		{
			name: "missing bearer token fails",
			envVars: map[string]string{
				"PROXY_USER":     "admin",
				"PROXY_PASSWORD": "secretpassword",
			},
			hasError: true,
		},
		{
			name: "bearer token fallback to API_BEARER_TOKEN",
			envVars: map[string]string{
				"PROXY_USER":       "admin",
				"PROXY_PASSWORD":   "secretpassword",
				"API_BEARER_TOKEN": "api-token",
			},
			hasError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear all relevant env vars first
			os.Unsetenv("PROXY_PORT")
			os.Unsetenv("PROXY_API_URL")
			os.Unsetenv("PROXY_USER")
			os.Unsetenv("PROXY_PASSWORD")
			os.Unsetenv("PROXY_BEARER_TOKEN")
			os.Unsetenv("API_BEARER_TOKEN")

			// Set test env vars
			for key, value := range tt.envVars {
				os.Setenv(key, value)
				defer os.Unsetenv(key)
			}

			cfg := LoadFromEnv()
			err := cfg.Validate()

			if tt.hasError && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.hasError && err != nil {
				t.Errorf("unexpected validation error: %v", err)
			}
		})
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && findSubstring(s, substr)))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
