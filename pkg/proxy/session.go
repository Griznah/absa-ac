package proxy

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/gob"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	defaultSessionTimeout = 4 * time.Hour
	cleanupInterval       = 5 * time.Minute
	sessionIDLength       = 16
)

var (
	ErrSessionNotFound = errors.New("session not found")
	ErrSessionExpired  = errors.New("session expired")
)

type contextKey string

const SessionContextKey contextKey = "session"

type Session struct {
	ID           string
	Token        string
	Expires      time.Time
	Created      time.Time
	LastAccessed time.Time
}

type SessionStore struct {
	sessions    map[string]*Session
	sessionsDir string
	mu          sync.RWMutex
	stopCleanup chan struct{}
}

func NewSessionStore(sessionsDir string) (*SessionStore, error) {
	if err := os.MkdirAll(sessionsDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create sessions directory: %w", err)
	}

	store := &SessionStore{
		sessions:    make(map[string]*Session),
		sessionsDir: sessionsDir,
		stopCleanup: make(chan struct{}),
	}

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
		if filepath.Ext(file.Name()) != ".gob" {
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
	if err := gob.NewDecoder(file).Decode(&session); err != nil {
		return nil, fmt.Errorf("failed to decode session: %w", err)
	}

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

	now := time.Now()
	session := &Session{
		ID:           sessionID,
		Token:        token,
		Expires:      now.Add(timeout),
		Created:      now,
		LastAccessed: now,
	}

	sessionPath := filepath.Join(s.sessionsDir, session.ID+".gob")
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

	if err := gob.NewEncoder(file).Encode(session); err != nil {
		os.Remove(path)
		return err
	}

	return nil
}

func (s *SessionStore) Get(id string) (*Session, bool) {
	s.mu.Lock()
	session, exists := s.sessions[id]
	if !exists {
		s.mu.Unlock()
		return nil, false
	}

	if time.Now().After(session.Expires) {
		s.mu.Unlock()
		s.Delete(id)
		return nil, false
	}

	session.LastAccessed = time.Now()
	s.mu.Unlock()

	return session, true
}

func (s *SessionStore) Delete(id string) error {
	s.mu.Lock()
	delete(s.sessions, id)
	s.mu.Unlock()

	sessionPath := filepath.Join(s.sessionsDir, id+".gob")
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
		sessionPath := filepath.Join(s.sessionsDir, id+".gob")
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

func generateSessionID() (string, error) {
	bytes := make([]byte, sessionIDLength)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}

	return base64.URLEncoding.EncodeToString(bytes), nil
}
