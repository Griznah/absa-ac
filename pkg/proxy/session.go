package proxy

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	defaultSessionTimeout = 4 * time.Hour
	cleanupInterval       = 5 * time.Minute
	sessionIDLength       = 16
	defaultKeyFile        = ".session_key"
	keySize               = 32
)

var (
	ErrSessionNotFound    = errors.New("session not found")
	ErrSessionExpired     = errors.New("session expired")
	ErrInvalidSessionID   = errors.New("invalid session ID")
)

type contextKey string

const SessionContextKey contextKey = "session"

// isValidSessionID validates session ID format to prevent path traversal attacks
// Rationale: os.ReadDir + filepath.Join trusts directory contents -> attacker who can create
// files in sessions_dir could escape -> validate session IDs match base64 charset before
// file operations -> reject malformed filenames early (Decision Log: "Filename sanitization
// for path traversal")
// Returns true if: length >= 1, only base64url chars (A-Za-z0-9_-), no path separators
func isValidSessionID(id string) bool {
	if len(id) < 1 {
		return false
	}

	for _, ch := range id {
		// Allow base64url characters: A-Z, a-z, 0-9, hyphen, underscore
		isAlphaNum := (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') || (ch >= '0' && ch <= '9')
		isBase64URL := ch == '-' || ch == '_'

		if !isAlphaNum && !isBase64URL {
			return false
		}
	}

	return true
}

// loadEncryptionKey reads and validates the session encryption key from file.
// Falls back to generating a new key if none exists (first run).
// Rationale: File-based storage with restricted permissions (0600) is more secure than
// environment variables (visible in /proc, logs, child processes). Base64 encoding for
// easy storage. Generates 256-bit key on first run for immediate usability.
// Key file path can be overridden via SESSION_KEY_FILE env var for testing/custom setups.
func loadEncryptionKey() ([]byte, error) {
	// Check for environment variable override (for testing)
	keyFile := os.Getenv("SESSION_KEY_FILE")
	if keyFile == "" {
		keyFile = defaultKeyFile
	}

	// Try to read existing key file
	keyBytes, err := os.ReadFile(keyFile)
	if err == nil {
		key, err := base64.StdEncoding.DecodeString(string(keyBytes))
		if err != nil {
			return nil, fmt.Errorf("failed to decode encryption key from file: %w", err)
		}
		if len(key) != keySize {
			return nil, fmt.Errorf("invalid encryption key length in file: expected %d bytes, got %d", keySize, len(key))
		}
		return key, nil
	}

	// Key file doesn't exist - generate a new key
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("failed to read key file: %w", err)
	}

	log.Printf("No encryption key found at %s, generating new key", keyFile)
	key := make([]byte, keySize)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("failed to generate encryption key: %w", err)
	}

	// Write key file with restricted permissions
	keyStr := base64.StdEncoding.EncodeToString(key)
	if err := os.WriteFile(keyFile, []byte(keyStr), 0600); err != nil {
		return nil, fmt.Errorf("failed to write encryption key file: %w", err)
	}

	log.Printf("Generated encryption key and saved to %s (permissions: 0600)", keyFile)
	return key, nil
}

// encryptToken encrypts a Bearer token using AES-256-GCM authenticated encryption
// Rationale: Tokens must not be stored in plaintext (Decision Log: "AES-256-GCM for
// token encryption") -> AES-256-GCM provides confidentiality and integrity -> unique
// nonce for each encryption prevents replay attacks -> returns base64-encoded ciphertext
// for JSON serialization -> encryption overhead is acceptable for login flow (not hot path)
func encryptToken(plaintext, key []byte) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}

	// Encrypt and authenticate token (nonce prepended to ciphertext)
	ciphertext := gcm.Seal(nonce, nonce, plaintext, nil)

	// Base64-encode for JSON serialization (RawURLEncoding for URL-safe charset)
	return base64.RawURLEncoding.EncodeToString(ciphertext), nil
}

// decryptToken decrypts a base64-encoded ciphertext using AES-256-GCM
// Rationale: Encrypted tokens must be decrypted before use (Decision Log: "Token-encrypted-
// at-rest pattern") -> GCM authentication prevents tampering -> returns error on authentication
// failure (invalid key or corrupted data) -> fast operation (~500ns) for hot path usage in
// Get() during every proxied request
func decryptToken(ciphertext string, key []byte) (string, error) {
	data, err := base64.RawURLEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode ciphertext: %w", err)
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, cipherData := data[:nonceSize], data[nonceSize:]

	// Decrypt and authenticate (returns error if tampered)
	plaintext, err := gcm.Open(nil, nonce, cipherData, nil)
	if err != nil {
		return "", fmt.Errorf("decryption failed: %w", err)
	}

	return string(plaintext), nil
}

type Session struct {
	ID             string
	Token          string `json:"-"` // Left empty, use GetToken() to decrypt
	EncryptedToken string `json:"encrypted_token"`
	CSRFToken      string `json:"csrf_token"` // Separate CSRF token for POST/PUT/DELETE requests
	Expires        time.Time
	Created        time.Time
	LastAccessed   time.Time
}

type SessionStore struct {
	sessions      map[string]*Session
	sessionsDir   string
	encryptionKey []byte
	mu            sync.RWMutex
	stopCleanup   chan struct{}
}

func NewSessionStore(sessionsDir string) (*SessionStore, error) {
	key, err := loadEncryptionKey()
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(sessionsDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create sessions directory: %w", err)
	}

	store := &SessionStore{
		sessions:      make(map[string]*Session),
		sessionsDir:   sessionsDir,
		encryptionKey: key,
		stopCleanup:   make(chan struct{}),
	}

	store.cleanupLegacyGobFiles()

	if err := store.loadExistingSessions(); err != nil {
		return nil, fmt.Errorf("failed to load existing sessions: %w", err)
	}

	go store.backgroundCleanup()

	return store, nil
}

func (s *SessionStore) loadExistingSessions() error {
	files, err := os.ReadDir(s.sessionsDir)
	if err != nil {
		return fmt.Errorf("failed to read sessions directory: %w", err)
	}

	for _, file := range files {
		if filepath.Ext(file.Name()) != ".json" {
			continue
		}

		sessionID := strings.TrimSuffix(file.Name(), ".json")

		if !isValidSessionID(sessionID) {
			log.Printf("Warning: skipping invalid session filename: %s", file.Name())
			continue
		}

		sessionPath := filepath.Join(s.sessionsDir, file.Name())
		session, err := s.loadSessionFromFile(sessionPath)
		if err != nil {
			continue
		}

		if time.Now().Before(session.Expires) {
			s.sessions[session.ID] = session
		} else {
			os.Remove(sessionPath)
		}
	}

	return nil
}

func (s *SessionStore) loadSessionFromFile(path string) (*Session, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open session file: %w", err)
	}
	defer file.Close()

	var session Session
	if err := json.NewDecoder(file).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode session: %w", err)
	}

	// Validate encrypted token can be decrypted (catches corruption early)
	if _, err := decryptToken(session.EncryptedToken, s.encryptionKey); err != nil {
		return nil, fmt.Errorf("failed to validate encrypted token: %w", err)
	}
	// Note: Token field is left empty to minimize time in memory
	// Use GetToken() method to decrypt on-demand when needed

	return &session, nil
}

func (s *SessionStore) Create(token string, timeout time.Duration) (*Session, error) {
	if timeout == 0 {
		timeout = defaultSessionTimeout
	}

	sessionID, err := generateSessionID()
	if err != nil {
		return nil, fmt.Errorf("failed to generate session ID: %w", err)
	}

	encryptedToken, err := encryptToken([]byte(token), s.encryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt token: %w", err)
	}

	// Generate CSRF token (separate from session ID for security)
	csrfTokenBytes := make([]byte, 32)
	if _, err := rand.Read(csrfTokenBytes); err != nil {
		return nil, fmt.Errorf("failed to generate CSRF token: %w", err)
	}
	csrfToken := base64.RawURLEncoding.EncodeToString(csrfTokenBytes)

	now := time.Now()
	session := &Session{
		ID:             sessionID,
		Token:          "", // Left empty - use GetToken() to decrypt on-demand
		EncryptedToken: encryptedToken,
		CSRFToken:      csrfToken,
		Expires:        now.Add(timeout),
		Created:        now,
		LastAccessed:   now,
	}

	sessionPath := filepath.Join(s.sessionsDir, session.ID+".json")
	if err := s.writeSessionToFile(session, sessionPath); err != nil {
		return nil, fmt.Errorf("failed to write session file: %w", err)
	}

	s.mu.Lock()
	s.sessions[session.ID] = session
	s.mu.Unlock()

	return session, nil
}

func (s *SessionStore) writeSessionToFile(session *Session, path string) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	if err := json.NewEncoder(file).Encode(session); err != nil {
		os.Remove(path)
		return err
	}

	return nil
}

func (s *SessionStore) Get(id string) (*Session, error) {
	// Validate session ID format before file operations (path traversal protection)
	if !isValidSessionID(id) {
		return nil, ErrInvalidSessionID
	}

	s.mu.Lock()
	session, exists := s.sessions[id]
	if !exists {
		s.mu.Unlock()
		return nil, ErrSessionNotFound
	}

	if time.Now().After(session.Expires) {
		s.mu.Unlock()
		s.Delete(id)
		return nil, ErrSessionExpired
	}

	session.LastAccessed = time.Now()
	s.mu.Unlock()

	return session, nil
}

// GetToken decrypts and returns the bearer token for a session.
// This method decrypts the token on-demand to minimize the time it spends unencrypted in memory.
// Returns the token or an error if the session doesn't exist or decryption fails.
func (s *SessionStore) GetToken(id string) (string, error) {
	s.mu.RLock()
	session, exists := s.sessions[id]
	if !exists {
		s.mu.RUnlock()
		return "", ErrSessionNotFound
	}

	encryptedToken := session.EncryptedToken
	s.mu.RUnlock()

	token, err := decryptToken(encryptedToken, s.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt token: %w", err)
	}

	return token, nil
}

func (s *SessionStore) Delete(id string) error {
	if !isValidSessionID(id) {
		return ErrInvalidSessionID
	}

	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()

	sessionPath := filepath.Join(s.sessionsDir, id+".json")
	if err := os.Remove(sessionPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete session file: %w", err)
	}

	return nil
}

func (s *SessionStore) Cleanup() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	var expiredIDs []string

	for id, session := range s.sessions {
		if now.After(session.Expires) {
			expiredIDs = append(expiredIDs, id)
		}
	}

	for _, id := range expiredIDs {
		delete(s.sessions, id)
		sessionPath := filepath.Join(s.sessionsDir, id+".json")
		os.Remove(sessionPath)
	}

	return nil
}

func (s *SessionStore) backgroundCleanup() {
	ticker := time.NewTicker(cleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.Cleanup()
		case <-s.stopCleanup:
			return
		}
	}
}

func (s *SessionStore) StopBackgroundCleanup() {
	close(s.stopCleanup)
}

func (s *SessionStore) cleanupLegacyGobFiles() {
	files, err := os.ReadDir(s.sessionsDir)
	if err != nil {
		return
	}

	for _, file := range files {
		if filepath.Ext(file.Name()) == ".gob" {
			path := filepath.Join(s.sessionsDir, file.Name())
			if err := os.Remove(path); err == nil {
				log.Printf("Warning: deleted legacy gob session file: %s", file.Name())
			}
		}
	}
}

func generateSessionID() (string, error) {
	bytes := make([]byte, sessionIDLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(bytes), nil
}
