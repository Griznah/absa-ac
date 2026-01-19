package proxy

import (
	"encoding/gob"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestNewSessionStore(t *testing.T) {
	t.Run("creates new session store", func(t *testing.T) {
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

		store.StopBackgroundCleanup()
	})

	t.Run("creates sessions directory", func(t *testing.T) {
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

	t.Run("loads existing valid sessions", func(t *testing.T) {
		tmpDir := t.TempDir()

		session := &Session{
			ID:           "test-session-id",
			Token:        "test-token",
			Expires:      time.Now().Add(1 * time.Hour),
			Created:      time.Now(),
			LastAccessed: time.Now(),
		}

		sessionPath := filepath.Join(tmpDir, session.ID+".gob")
		file, err := os.OpenFile(sessionPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatalf("failed to create test session file: %v", err)
		}
		if err := gob.NewEncoder(file).Encode(session); err != nil {
			file.Close()
			t.Fatalf("failed to encode test session: %v", err)
		}
		file.Close()

		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		loaded, exists := store.Get(session.ID)
		if !exists {
			t.Fatal("existing session not loaded")
		}

		if loaded.ID != session.ID {
			t.Errorf("loaded session ID = %s, want %s", loaded.ID, session.ID)
		}
		if loaded.Token != session.Token {
			t.Errorf("loaded session Token = %s, want %s", loaded.Token, session.Token)
		}
	})

	t.Run("does not load expired sessions", func(t *testing.T) {
		tmpDir := t.TempDir()

		session := &Session{
			ID:           "expired-session",
			Token:        "test-token",
			Expires:      time.Now().Add(-1 * time.Hour),
			Created:      time.Now().Add(-2 * time.Hour),
			LastAccessed: time.Now().Add(-2 * time.Hour),
		}

		sessionPath := filepath.Join(tmpDir, session.ID+".gob")
		file, err := os.OpenFile(sessionPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Fatalf("failed to create expired session file: %v", err)
		}
		if err := gob.NewEncoder(file).Encode(session); err != nil {
			file.Close()
			t.Fatalf("failed to encode expired session: %v", err)
		}
		file.Close()

		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		if _, exists := store.Get(session.ID); exists {
			t.Error("expired session was loaded")
		}

		if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
			t.Error("expired session file was not deleted")
		}
	})
}

func TestSessionCreate(t *testing.T) {
	t.Run("creates session with file and in-memory entry", func(t *testing.T) {
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
		if session.Token != "test-token" {
			t.Errorf("session Token = %s, want test-token", session.Token)
		}
		if time.Now().Add(defaultSessionTimeout).Add(-1 * time.Second).After(session.Expires) {
			t.Error("session expiration time is too early")
		}
		if time.Now().Add(defaultSessionTimeout).Add(1 * time.Second).Before(session.Expires) {
			t.Error("session expiration time is too late")
		}

		sessionPath := filepath.Join(tmpDir, session.ID+".gob")
		if info, err := os.Stat(sessionPath); err != nil {
			t.Errorf("session file not created: %v", err)
		} else if info.Mode().Perm() != 0600 {
			t.Errorf("session file permissions = %o, want 0600", info.Mode().Perm())
		}

		retrieved, exists := store.Get(session.ID)
		if !exists {
			t.Fatal("session not in memory")
		}
		if retrieved.ID != session.ID {
			t.Errorf("retrieved session ID = %s, want %s", retrieved.ID, session.ID)
		}
		if retrieved.Token != session.Token {
			t.Errorf("retrieved session Token = %s, want %s", retrieved.Token, session.Token)
		}
	})

	t.Run("generates unique session IDs", func(t *testing.T) {
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
}

func TestSessionGet(t *testing.T) {
	t.Run("retrieves existing session", func(t *testing.T) {
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

		retrieved, exists := store.Get(created.ID)
		if !exists {
			t.Fatal("Get() returned exists = false")
		}
		if retrieved.ID != created.ID {
			t.Errorf("retrieved session ID = %s, want %s", retrieved.ID, created.ID)
		}
		if retrieved.Token != created.Token {
			t.Errorf("retrieved session Token = %s, want %s", retrieved.Token, created.Token)
		}
	})

	t.Run("returns false for non-existent session", func(t *testing.T) {
		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}
		defer store.StopBackgroundCleanup()

		_, exists := store.Get("non-existent-id")
		if exists {
			t.Error("Get() returned exists = true for non-existent session")
		}
	})

	t.Run("updates last accessed time", func(t *testing.T) {
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

		retrieved, exists := store.Get(session.ID)
		if !exists {
			t.Fatal("Get() returned exists = false")
		}

		if !retrieved.LastAccessed.After(originalLastAccessed) {
			t.Errorf("LastAccessed was not updated: original=%v, retrieved=%v", originalLastAccessed, retrieved.LastAccessed)
		}
	})

	t.Run("deletes expired session on access", func(t *testing.T) {
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

		_, exists := store.Get(session.ID)
		if exists {
			t.Error("Get() returned exists = true for expired session")
		}

		_, exists = store.Get(session.ID)
		if exists {
			t.Error("expired session still exists after Get()")
		}
	})
}

func TestSessionDelete(t *testing.T) {
	t.Run("deletes session from memory and disk", func(t *testing.T) {
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

		sessionPath := filepath.Join(tmpDir, session.ID+".gob")

		if err := store.Delete(session.ID); err != nil {
			t.Fatalf("Delete() error = %v", err)
		}

		if _, exists := store.Get(session.ID); exists {
			t.Error("session still exists in memory after Delete()")
		}

		if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
			t.Error("session file still exists after Delete()")
		}
	})

	t.Run("deleting non-existent session succeeds", func(t *testing.T) {
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

		if _, exists := store.Get(validSession.ID); !exists {
			t.Error("valid session was removed")
		}

		if _, exists := store.Get(expiredSession.ID); exists {
			t.Error("expired session still exists")
		}

		expiredPath := filepath.Join(tmpDir, expiredSession.ID+".gob")
		if _, err := os.Stat(expiredPath); !os.IsNotExist(err) {
			t.Error("expired session file still exists")
		}
	})
}

func TestConcurrentAccess(t *testing.T) {
	t.Run("concurrent reads do not deadlock", func(t *testing.T) {
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

		sessionPath := filepath.Join(tmpDir, session.ID+".gob")
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
		tmpDir := t.TempDir()

		corruptPath := filepath.Join(tmpDir, "corrupt-session.gob")
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

		if _, exists := store.Get("corrupt-session"); exists {
			t.Error("corrupt session was loaded")
		}
	})
}

func TestBackgroundCleanup(t *testing.T) {
	t.Run("background cleanup runs periodically", func(t *testing.T) {
		tmpDir := t.TempDir()
		store, err := NewSessionStore(tmpDir)
		if err != nil {
			t.Fatalf("NewSessionStore() error = %v", err)
		}

		session, err := store.Create("test-token", 10*time.Millisecond)
		if err != nil {
			t.Fatalf("Create() error = %v", err)
		}

		time.Sleep(20 * time.Millisecond)

		if _, exists := store.Get(session.ID); exists {
			t.Error("expired session not cleaned up by background goroutine")
		}

		store.StopBackgroundCleanup()
	})

	t.Run("stop background cleanup gracefully", func(t *testing.T) {
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
