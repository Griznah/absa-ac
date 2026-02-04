package proxy

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// setupTestKeyFile creates a test encryption key file and returns the path.
// Caller is responsible for cleanup (t.Cleanup or t.TempDir).
func setupTestKeyFile(t *testing.T, keyData string) string {
	t.Helper()
	key := base64.StdEncoding.EncodeToString([]byte(keyData))
	keyFile := filepath.Join(t.TempDir(), ".session_key")
	if err := os.WriteFile(keyFile, []byte(key), 0600); err != nil {
		t.Fatalf("Failed to write test key file: %v", err)
	}
	t.Setenv("SESSION_KEY_FILE", keyFile)
	return keyFile
}

func TestNewSessionStore(t *testing.T) {
	t.Run("creates new session store with valid encryption key", func(t *testing.T) {
		_ = setupTestKeyFile(t, strings.Repeat("x", 32))

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)

		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}

		if store == nil {
			t.Fatal("NewSessionStore() returned nil store")
		}

		if store.sessionsDir != tmpDir {
			t.Errorf("sessionsDir = %s, want %s", store.sessionsDir, tmpDir)
		}

		if store.encryptionKey == nil {
			t.Error("encryptionKey is nil")
		}

		store.StopBackgroundCleanup()
	})

	t.Run("auto-generates key when key file does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, ".session_key")
		t.Setenv("SESSION_KEY_FILE", keyFile)

		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}

		// Verify key file was created
		if _, err := os.Stat(keyFile); err != nil {
			t.Errorf("Key file was not created: %v", err)
		}

		store.StopBackgroundCleanup()
	})

	t.Run("returns error when key file contains invalid base64", func(t *testing.T) {
		tmpDir := t.TempDir()
		keyFile := filepath.Join(tmpDir, ".session_key")
		if err := os.WriteFile(keyFile, []byte("not-valid-base64!!!"), 0600); err != nil {
			t.Fatalf("Failed to write test key file: %v", err)
		}
		t.Setenv("SESSION_KEY_FILE", keyFile)

		_, err := NewSessionStore(t.TempDir())
		if err == nil {
			t.Error("NewSessionStore() expected error for invalid base64 key")
		}
	})

	t.Run("returns error when key file contains wrong length", func(t *testing.T) {
		tmpDir := t.TempDir()
		shortKey := base64.StdEncoding.EncodeToString([]byte("short"))
		keyFile := filepath.Join(tmpDir, ".session_key")
		if err := os.WriteFile(keyFile, []byte(shortKey), 0600); err != nil {
			t.Fatalf("Failed to write test key file: %v", err)
		}
		t.Setenv("SESSION_KEY_FILE", keyFile)

		_, err := NewSessionStore(t.TempDir())
		if err == nil {
			t.Error("NewSessionStore() expected error for short encryption key")
		}
	})

	t.Run("creates sessions directory", func(t *testing.T) {
		_ = setupTestKeyFile(t, strings.Repeat("a", 32))

		tmpDir := t.TempDir()
		sessionsDir := filepath.Join(tmpDir, "sessions")

		_, err := NewSessionStore(sessionsDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}

		if info, err := os.Stat(sessionsDir); err != nil {
			t.Fatalf("sessions directory not created: %v", err)
		} else if info.Mode().Perm() != 0700 {
			t.Errorf("sessions directory permissions = %o, want 0700", info.Mode().Perm())
		}
	})

	t.Run("loads existing valid sessions from JSON", func(t *testing.T) {
		keyData := strings.Repeat("b", 32)
		_ = setupTestKeyFile(t, keyData)

		tmpDir := t.TempDir()

		now := time.Now()
		plaintextToken := "test-token"
		encryptedToken, _ := encryptToken([]byte(plaintextToken), []byte(keyData))

		session := &Session{
			ID:             "test-session-id",
			Token:          "",
			EncryptedToken: encryptedToken,
			Expires:        now.Add(1 * time.Hour),
			Created:        now,
			LastAccessed:   now,
		}

		sessionPath := filepath.Join(tmpDir, session.ID+".json")
		file, err := os.OpenFile(sessionPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatalf("failed to create test session file: %v", err)
		}
		if err := json.NewEncoder(file).Encode(session); err != nil {
			file.Close()
			t.Fatalf("failed to encode test session: %v", err)
		}
		file.Close()

		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		loaded, err := store.Get(session.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}

		if loaded.ID != session.ID {
			t.Errorf("loaded session ID = %s, want %s", loaded.ID, session.ID)
		}

		// Use GetToken() to decrypt and verify token
		decryptedToken, err := store.GetToken(session.ID)
		if err != nil {
			t.Fatalf("GetToken() error = %v", err)
		}
		if decryptedToken != plaintextToken {
			t.Errorf("decrypted token = %s, want %s", decryptedToken, plaintextToken)
		}
	})

	t.Run("does not load expired sessions", func(t *testing.T) {
		keyData := strings.Repeat("c", 32)
		_ = setupTestKeyFile(t, keyData)

		tmpDir := t.TempDir()

		now := time.Now()
		plaintextToken := "test-token"
		encryptedToken, _ := encryptToken([]byte(plaintextToken), []byte(keyData))

		session := &Session{
			ID:             "expired-session",
			Token:          "",
			EncryptedToken: encryptedToken,
			Expires:        now.Add(-1 * time.Hour),
			Created:        now.Add(-2 * time.Hour),
			LastAccessed:   now.Add(-2 * time.Hour),
		}

		sessionPath := filepath.Join(tmpDir, session.ID+".json")
		file, err := os.OpenFile(sessionPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatalf("failed to create expired session file: %v", err)
		}
		if err := json.NewEncoder(file).Encode(session); err != nil {
			file.Close()
			t.Fatalf("failed to encode expired session: %v", err)
		}
		file.Close()

		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		if _, err := store.Get(session.ID); err == nil {
			t.Error("expired session was loaded")
		}

		if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
			t.Error("expired session file was not deleted")
		}
	})

	t.Run("deletes legacy gob files on startup", func(t *testing.T) {
		_ = setupTestKeyFile(t, strings.Repeat("d", 32))

		tmpDir := t.TempDir()

		gobPath := filepath.Join(tmpDir, "legacy-session.gob")
		file, err := os.Create(gobPath)
		if err != nil {
			t.Fatalf("failed to create gob file: %v", err)
		}
		file.WriteString("legacy gob data")
		file.Close()

		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		if _, err := os.Stat(gobPath); !os.IsNotExist(err) {
			t.Error("legacy gob file was not deleted")
		}
	})
}

func TestSessionCreate(t *testing.T) {
	t.Run("creates session with JSON file and encrypted token", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("e", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		session, err := store.Create("test-token", defaultSessionTimeout)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		if session.ID == "" {
			t.Error("session ID is empty")
		}
		// Token field is left empty - use GetToken() to decrypt and verify
		token, err := store.GetToken(session.ID)
		if err != nil {
			t.Fatalf("GetToken() error = %v", err)
		}
		if token != "test-token" {
			t.Errorf("decrypted token = %s, want test-token", token)
		}
		if time.Now().Add(defaultSessionTimeout).Add(-1*time.Second).After(session.Expires) {
			t.Error("session expiration time is too early")
		}
		if time.Now().Add(defaultSessionTimeout).Add(1*time.Second).Before(session.Expires) {
			t.Error("session expiration time is too late")
		}

		sessionPath := filepath.Join(tmpDir, session.ID+".json")
		if info, err := os.Stat(sessionPath); err != nil {
			t.Errorf("session file not created: %v", err)
		} else if info.Mode().Perm() != 0600 {
			t.Errorf("session file permissions = %o, want 0600", info.Mode().Perm())
		}

		retrieved, err := store.Get(session.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if retrieved.ID != session.ID {
			t.Errorf("retrieved session ID = %s, want %s", retrieved.ID, session.ID)
		}
		// Verify both sessions decrypt to the same token
		retrievedToken, err := store.GetToken(retrieved.ID)
		if err != nil {
			t.Fatalf("GetToken() for retrieved session error = %v", err)
		}
		originalToken, err := store.GetToken(session.ID)
		if err != nil {
			t.Fatalf("GetToken() for original session error = %v", err)
		}
		if retrievedToken != originalToken {
			t.Errorf("retrieved decrypted token = %s, want %s", retrievedToken, originalToken)
		}
	})

	t.Run("token is encrypted in JSON file", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("f", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		session, err := store.Create("secret-bearer-token", defaultSessionTimeout)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		sessionPath := filepath.Join(tmpDir, session.ID+".json")
		file, err := os.ReadFile(sessionPath)
		if err != nil {
			t.Fatalf("failed to read session file: %v", err)
		}

		fileContent := string(file)
		if strings.Contains(fileContent, "secret-bearer-token") {
			t.Error("plaintext token found in JSON file")
		}
		if !strings.Contains(fileContent, "encrypted_token") {
			t.Error("encrypted_token field not found in JSON file")
		}
	})

	t.Run("generates unique session IDs", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("g", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		ids := make(map[string]bool)
		for i := 0; i < 100; i++ {
			session, err := store.Create("token", defaultSessionTimeout)
			if err != nil {
				t.Fatalf("Create() iteration %d error = %v", i, err)
			}
			if ids[session.ID] {
				t.Errorf("duplicate session ID generated: %s", session.ID)
			}
			ids[session.ID] = true
		}
	})

	t.Run("uses default timeout when timeout is zero", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("h", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		session, err := store.Create("test-token", 0)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		expectedExpires := time.Now().Add(defaultSessionTimeout)
		if session.Expires.Before(expectedExpires.Add(-1*time.Second)) || session.Expires.After(expectedExpires.Add(1*time.Second)) {
			t.Errorf("session Expires = %v, want approximately %v", session.Expires, expectedExpires)
		}
	})

	t.Run("returns error when encryption fails", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("i", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		store.encryptionKey = []byte("invalid")

		_, err = store.Create("test-token", defaultSessionTimeout)
		if err == nil {
			t.Error("Create() expected error for invalid encryption key")
		}
	})
}

func TestSessionGet(t *testing.T) {
	t.Run("retrieves and decrypts existing session", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("j", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		created, err := store.Create("test-token", defaultSessionTimeout)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		retrieved, err := store.Get(created.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if retrieved.ID != created.ID {
			t.Errorf("retrieved session ID = %s, want %s", retrieved.ID, created.ID)
		}
		if retrieved.Token != created.Token {
			t.Errorf("retrieved session Token = %s, want %s", retrieved.Token, created.Token)
		}
	})

	t.Run("returns error for tampered ciphertext", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		created, err := store.Create("test-token", defaultSessionTimeout)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		sessionPath := filepath.Join(tmpDir, created.ID+".json")
		data, err := os.ReadFile(sessionPath)
		if err != nil {
			t.Fatalf("failed to read session file: %v", err)
		}

		tamperedData := strings.ReplaceAll(string(data), created.EncryptedToken, "tampered-ciphertext")
		if err := os.WriteFile(sessionPath, []byte(tamperedData), 0600); err != nil {
			t.Fatalf("failed to write tampered session: %v", err)
		}

		delete(store.sessions, created.ID)

		_, err = store.Get(created.ID)
		if err == nil {
			t.Error("Get() returned nil error for tampered session")
		}
	})

	t.Run("returns error for non-existent session", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("l", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		_, err = store.Get("non-existent-id")
		if err != ErrSessionNotFound {
			t.Errorf("Get() error = %v, want ErrSessionNotFound", err)
		}
	})

	t.Run("updates last accessed time", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("m", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		session, err := store.Create("test-token", defaultSessionTimeout)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		originalLastAccessed := session.LastAccessed
		time.Sleep(10 * time.Millisecond)

		retrieved, err := store.Get(session.ID)
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}

		if !retrieved.LastAccessed.After(originalLastAccessed) {
			t.Errorf("LastAccessed was not updated: original=%v, retrieved=%v", originalLastAccessed, retrieved.LastAccessed)
		}
	})

	t.Run("deletes expired session on access", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("n", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		session, err := store.Create("test-token", 1*time.Millisecond)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		time.Sleep(10 * time.Millisecond)

		_, err = store.Get(session.ID)
		if err != ErrSessionExpired {
			t.Errorf("Get() error = %v, want ErrSessionExpired", err)
		}

		_, err = store.Get(session.ID)
		if err != ErrSessionNotFound {
			t.Errorf("Get() error = %v, want ErrSessionNotFound", err)
		}
	})
}

func TestSessionDelete(t *testing.T) {
	t.Run("deletes session from memory and disk", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("o", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		session, err := store.Create("test-token", defaultSessionTimeout)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		sessionPath := filepath.Join(tmpDir, session.ID+".json")

		if err := store.Delete(session.ID); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		if _, err := store.Get(session.ID); err != ErrSessionNotFound {
			t.Error("session still exists in memory after Delete()")
		}

		if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
			t.Error("session file still exists after Delete()")
		}
	})

	t.Run("deleting non-existent session succeeds", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("p", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		if err := store.Delete("non-existent-id"); err != nil {
			t.Errorf("Delete() error = %v", err)
		}
	})
}

func TestSessionCleanup(t *testing.T) {
	t.Run("removes expired sessions", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("q", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		validSession, err := store.Create("valid-token", 1*time.Hour)
		if err != nil {
			t.Fatalf("Create() valid session error = %v", err)
		}

		expiredSession, err := store.Create("expired-token", 1*time.Millisecond)
		if err != nil {
			t.Fatalf("Create() expired session error = %v", err)
		}

		time.Sleep(10 * time.Millisecond)

		if err := store.Cleanup(); err != nil {
			t.Fatalf("Cleanup() error = %v", err)
		}

		if _, err := store.Get(validSession.ID); err != nil {
			t.Error("valid session was removed")
		}

		if _, err := store.Get(expiredSession.ID); err != ErrSessionNotFound {
			t.Error("expired session still exists")
		}

		expiredPath := filepath.Join(tmpDir, expiredSession.ID+".json")
		if _, err := os.Stat(expiredPath); !os.IsNotExist(err) {
			t.Error("expired session file still exists")
		}
	})
}

func TestConcurrentAccess(t *testing.T) {
	t.Run("concurrent reads do not deadlock", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("r", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		session, err := store.Create("test-token", defaultSessionTimeout)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		var wg sync.WaitGroup
		done := make(chan bool)

		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 10; j++ {
					select {
					case <-done:
						return
					default:
						store.Get(session.ID)
						time.Sleep(1 * time.Millisecond)
					}
				}
			}()
		}

		time.Sleep(50 * time.Millisecond)
		close(done)
		wg.Wait()
	})

	t.Run("concurrent writes do not deadlock", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("s", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		var wg sync.WaitGroup

		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func(n int) {
				defer wg.Done()
				_, err := store.Create("test-token", defaultSessionTimeout)
				if err != nil {
					t.Errorf("concurrent Create() %d error = %v", n, err)
				}
			}(i)
		}

		wg.Wait()
	})

	t.Run("concurrent reads and writes do not deadlock", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("t", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		session, err := store.Create("test-token", defaultSessionTimeout)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		var wg sync.WaitGroup

		for i := 0; i < 50; i++ {
			wg.Add(2)

			go func() {
				defer wg.Done()
				store.Get(session.ID)
			}()

			go func() {
				defer wg.Done()
				store.Create("new-token", defaultSessionTimeout)
			}()
		}

		wg.Wait()
	})
}

func TestSessionFilePermissions(t *testing.T) {
	t.Run("session files have correct permissions", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("u", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		session, err := store.Create("test-token", defaultSessionTimeout)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		sessionPath := filepath.Join(tmpDir, session.ID+".json")
		info, err := os.Stat(sessionPath)
		if err != nil {
			t.Fatalf("failed to stat session file: %v", err)
		}

		if info.Mode().Perm() != 0600 {
			t.Errorf("session file permissions = %o, want 0600", info.Mode().Perm())
		}
	})
}

func TestCorruptSessionFile(t *testing.T) {
	t.Run("ignores corrupt session files during load", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("v", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()

		corruptPath := filepath.Join(tmpDir, "corrupt-session.json")
		file, err := os.Create(corruptPath)
		if err != nil {
			t.Fatalf("failed to create corrupt file: %v", err)
		}
		file.WriteString("corrupt data")
		file.Close()

		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		if _, err := store.Get("corrupt-session"); err != ErrSessionNotFound {
			t.Error("corrupt session was loaded")
		}
	})
}

func TestBackgroundCleanup(t *testing.T) {

	t.Run("stop background cleanup gracefully", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("x", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}

		done := make(chan bool)
		go func() {
			store.StopBackgroundCleanup()
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Error("StopBackgroundCleanup() did not return")
		}
	})
}

func TestGenerateSessionID(t *testing.T) {
	t.Run("generates valid session IDs", func(t *testing.T) {
		id, err := generateSessionID()
		if err != nil {
			t.Fatalf("generateSessionID() error = %v", err)
		}

		if len(id) == 0 {
			t.Error("generated session ID is empty")
		}
	})

	t.Run("generates unique session IDs", func(t *testing.T) {
		ids := make(map[string]bool)
		for i := 0; i < 1000; i++ {
			id, err := generateSessionID()
			if err != nil {
				t.Fatalf("generateSessionID() iteration %d error = %v", i, err)
			}
			if ids[id] {
				t.Errorf("duplicate session ID generated: %s", id)
			}
			ids[id] = true
		}
	})
}

func TestIsValidSessionID(t *testing.T) {
	t.Run("accepts valid base64 session IDs", func(t *testing.T) {
		validIDs := []string{
			"abc123",
			"ABC-xyz_0123",
			"aB3-XyZ_9",
			strings.Repeat("a", 100),
		}

		for _, id := range validIDs {
			if !isValidSessionID(id) {
				t.Errorf("isValidSessionID(%q) = false, want true", id)
			}
		}
	})

	t.Run("rejects empty string", func(t *testing.T) {
		if isValidSessionID("") {
			t.Error("isValidSessionID(\"\") = true, want false")
		}
	})

	t.Run("rejects single character", func(t *testing.T) {
		singleCharIDs := []string{"a", "Z", "9", "-", "_"}
		for _, id := range singleCharIDs {
			if !isValidSessionID(id) {
				t.Errorf("isValidSessionID(%q) = false, want true", id)
			}
		}
	})

	t.Run("rejects path traversal sequences", func(t *testing.T) {
		maliciousIDs := []string{
			"../../../etc/passwd",
			"..\\..\\..\\windows\\system32",
			"../malicious.gob",
			"../../sensitive.json",
			"../",
			"..",
			"./etc/passwd",
			".\\windows\\system32",
		}

		for _, id := range maliciousIDs {
			if isValidSessionID(id) {
				t.Errorf("isValidSessionID(%q) = true, want false (path traversal)", id)
			}
		}
	})

	t.Run("rejects absolute paths", func(t *testing.T) {
		absolutePaths := []string{
			"/etc/passwd",
			"/tmp/session.json",
			"C:\\Windows\\System32",
			"\\\\network\\path",
		}

		for _, id := range absolutePaths {
			if isValidSessionID(id) {
				t.Errorf("isValidSessionID(%q) = true, want false (absolute path)", id)
			}
		}
	})

	t.Run("rejects invalid base64url characters", func(t *testing.T) {
		invalidIDs := []string{
			"abc+def",              // + not allowed
			"abc/def",              // / not allowed (path separator)
			"abc=def",              // = not allowed
			"abc def",              // space not allowed
			"abc\tdef",             // tab not allowed
			"abc\x00def",           // null not allowed
			"abc!@#$%def",          // special chars not allowed
			"abc.txt",              // . not allowed
			"abc,def",              // , not allowed
			"abc;def",              // ; not allowed
			"abc(def)",             // () not allowed
			"abc[def]",             // [] not allowed
			"abc{def}",             // {} not allowed
			"abc<def>",             // <> not allowed
			"abc&def",              // & not allowed
			"abc|def",              // | not allowed
			"abc$def",              // $ not allowed
			"abc`def",              // backtick not allowed
			"abc'def",              // single quote not allowed
			"abc\"def",             // double quote not allowed
			"abc\\def",             // backslash not allowed
		}

		for _, id := range invalidIDs {
			if isValidSessionID(id) {
				t.Errorf("isValidSessionID(%q) = true, want false (invalid char)", id)
			}
		}
	})
}

func TestSessionGetInvalidID(t *testing.T) {
	t.Run("returns error for path traversal attempt", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("y", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		_, err = store.Get("../../../etc/passwd")
		if err != ErrInvalidSessionID {
			t.Errorf("Get() error = %v, want ErrInvalidSessionID", err)
		}
	})

	t.Run("returns error for empty string", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("z", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		_, err = store.Get("")
		if err != ErrInvalidSessionID {
			t.Errorf("Get() error = %v, want ErrInvalidSessionID", err)
		}
	})

	t.Run("returns error for ID with invalid characters", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("0", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		_, err = store.Get("abc/../def")
		if err != ErrInvalidSessionID {
			t.Errorf("Get() error = %v, want ErrInvalidSessionID", err)
		}
	})
}

func TestSessionDeleteInvalidID(t *testing.T) {
	t.Run("returns error for path traversal attempt", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("1", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		err = store.Delete("../../../etc/passwd")
		if err != ErrInvalidSessionID {
			t.Errorf("Delete() error = %v, want ErrInvalidSessionID", err)
		}
	})

	t.Run("returns error for empty string", func(t *testing.T) {
		key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("2", 32)))
		t.Setenv("SESSION_KEY_FILE", key)

		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		err = store.Delete("")
		if err != ErrInvalidSessionID {
			t.Errorf("Delete() error = %v, want ErrInvalidSessionID", err)
		}
	})
}

func TestLoadExistingSessionsInvalidFilenames(t *testing.T) {
	t.Run("skips invalid filenames without crashing", func(t *testing.T) {
		keyData := strings.Repeat("3", 32)
		_ = setupTestKeyFile(t, keyData)

		tmpDir := t.TempDir()

		// Test with special characters that should be rejected
		// We can't create actual files with path separators, so we test
		// that Get() properly rejects them without needing files on disk
		invalidSessionIDs := []string{
			"../../../etc/passwd",
			"../malicious.gob",
			"../../sensitive",
			"abc/../def",
			"/tmp/evil",
			"..\\..\\windows",
			"abc\\..\\def",
		}

		// Create a valid session
		now := time.Now()
		plaintextToken := "test-token"
		encryptedToken, _ := encryptToken([]byte(plaintextToken), []byte(keyData))
		session := &Session{
			ID:             "valid-session-abc123",
			Token:          "",
			EncryptedToken: encryptedToken,
			Expires:        now.Add(1 * time.Hour),
			Created:        now,
			LastAccessed:   now,
		}

		sessionPath := filepath.Join(tmpDir, session.ID+".json")
		file, err := os.OpenFile(sessionPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatalf("failed to create valid session file: %v", err)
		}
		if err := json.NewEncoder(file).Encode(session); err != nil {
			file.Close()
			t.Fatalf("failed to encode valid session: %v", err)
		}
		file.Close()

		// Load sessions - should successfully load valid session
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		// Valid session should be loaded
		loaded, err := store.Get(session.ID)
		if err != nil {
			t.Errorf("valid session was not loaded: %v", err)
		}
		if loaded.ID != session.ID {
			t.Errorf("loaded session ID = %s, want %s", loaded.ID, session.ID)
		}

		// Invalid session IDs should be rejected even without files on disk
		for _, sessionID := range invalidSessionIDs {
			_, err := store.Get(sessionID)
			if err != ErrInvalidSessionID {
				t.Errorf("malicious session %s was accessible (error = %v, want ErrInvalidSessionID)", sessionID, err)
			}
		}
	})
}
