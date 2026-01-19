package api

import (
	"crypto/rand"
	"encoding/hex"
	"log"
	"net/http"
	"os"
	"sync"
	"time"
)

// CSRF protection using custom request header pattern
// Single shared token for all users (matches current Bearer token model)
// In production with per-user sessions, this should be per-user tokens

var (
	csrfToken     string
	csrfTokenOnce sync.Once
	csrfMutex     sync.RWMutex
)

// initCSRFToken generates or loads a CSRF token from environment
// Falls back to generating a random token if not set
func initCSRFToken() {
	csrfTokenOnce.Do(func() {
		// Try to load from environment first
		csrfToken = os.Getenv("API_CSRF_TOKEN")

		if csrfToken == "" {
			// Generate random token (32 bytes = 64 hex chars)
			bytes := make([]byte, 32)
			if _, err := rand.Read(bytes); err != nil {
				log.Fatalf("Failed to generate CSRF token: %v", err)
			}
			csrfToken = hex.EncodeToString(bytes)
			log.Printf("Generated CSRF token (set API_CSRF_TOKEN env var to use fixed token)")
		} else {
			log.Printf("Using CSRF token from environment")
		}

		// Validate token length (should be at least 16 bytes/32 chars for security)
		if len(csrfToken) < 32 {
			log.Printf("WARNING: CSRF token is short (%d chars), recommend at least 32 chars", len(csrfToken))
		}
	})
}

// GetCSRFToken returns the current CSRF token (thread-safe)
func GetCSRFToken() string {
	csrfMutex.RLock()
	defer csrfMutex.RUnlock()
	return csrfToken
}

// RotateCSRFToken generates a new CSRF token (for admin operations or key rotation)
func RotateCSRFToken() string {
	csrfMutex.Lock()
	defer csrfMutex.Unlock()

	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		log.Printf("Failed to rotate CSRF token: %v", err)
		return csrfToken
	}

	newToken := hex.EncodeToString(bytes)
	csrfToken = newToken
	log.Printf("CSRF token rotated at %s", time.Now().Format(time.RFC3339))
	return newToken
}

// GetCSRFTokenHandler returns the current CSRF token to authenticated clients
// Requires Bearer token authentication (prevents token leakage to unauthenticated users)
func (s *Server) GetCSRFTokenHandler(w http.ResponseWriter, r *http.Request) {
	if err := r.Context().Err(); err != nil {
		log.Printf("GetCSRFTokenHandler cancelled: %v", err)
		WriteError(w, http.StatusServiceUnavailable, "Service unavailable", "Request cancelled")
		return
	}

	// Only allow GET requests
	if r.Method != http.MethodGet {
		WriteError(w, http.StatusMethodNotAllowed, "Method not allowed", "Only GET method is supported")
		return
	}

	// Return the CSRF token
	WriteJSON(w, http.StatusOK, map[string]string{
		"csrf_token": GetCSRFToken(),
		"expires_in": "3600", // 1 hour (clients should refresh periodically)
	})
}
