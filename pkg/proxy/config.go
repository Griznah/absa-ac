package proxy

import (
	"fmt"
	"os"
)

// Config holds proxy server configuration loaded from environment variables.
// DL-004: Proxy runs on configurable separate port (default 8080)
// DL-005: Single credential pair via environment variables
// DL-006: Proxy forwards to configurable API URL
type Config struct {
	Port        string // Port to listen on (default: 8080)
	APIURL      string // URL of the upstream API (default: http://localhost:3001)
	Username    string // Basic Auth username
	Password    string // Basic Auth password
	BearerToken string // Bearer token for API authentication
}

// LoadFromEnv reads configuration from environment variables.
// DL-006: PROXY_API_URL allows proxy to run on different host from API
// PROXY_BEARER_TOKEN defaults to API_BEARER_TOKEN for convenience
func LoadFromEnv() Config {
	port := os.Getenv("PROXY_PORT")
	if port == "" {
		port = "8080"
	}

	apiURL := os.Getenv("PROXY_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:3001"
	}

	bearerToken := os.Getenv("PROXY_BEARER_TOKEN")
	if bearerToken == "" {
		bearerToken = os.Getenv("API_BEARER_TOKEN")
	}

	return Config{
		Port:        port,
		APIURL:      apiURL,
		Username:    os.Getenv("PROXY_USER"),
		Password:    os.Getenv("PROXY_PASSWORD"),
		BearerToken: bearerToken,
	}
}

// Validate ensures configuration is valid before starting the proxy.
// DL-015: Fail-fast on missing/invalid credentials with PROXY_ENABLED=true
// DL-016: 8+ character minimum for password (OWASP minimum)
func (c Config) Validate() error {
	if c.Username == "" {
		return fmt.Errorf("PROXY_USER is required when PROXY_ENABLED=true")
	}
	if len(c.Password) < 8 {
		return fmt.Errorf("PROXY_PASSWORD must be at least 8 characters (got %d)", len(c.Password))
	}

	if c.BearerToken == "" {
		return fmt.Errorf("PROXY_BEARER_TOKEN (or API_BEARER_TOKEN) is required when PROXY_ENABLED=true")
	}

	return nil
}
