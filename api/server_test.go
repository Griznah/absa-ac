package api

import (
	"context"
	"log"
	"net/http"
	"os"
	"testing"
	"time"
)

// mockConfigManager is a test double for ConfigManager
type mockConfigManager struct {
	config any
	delay  time.Duration // Optional delay for testing graceful shutdown
}

func (m *mockConfigManager) GetConfigAny() any {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return m.config
}

func (m *mockConfigManager) WriteConfigAny(cfg any) error {
	m.config = cfg
	return nil
}

func (m *mockConfigManager) UpdateConfig(partial map[string]interface{}) error {
	return nil
}

func TestServer_StartStop(t *testing.T) {
	tests := []struct {
		name    string
		port    string
		token   string
		wantErr bool
	}{
		{
			name:    "Normal: Server starts and stops gracefully",
			port:    "13001",
			token:   "test-token-123",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := log.New(os.Stdout, "TEST: ", log.LstdFlags)

			cm := &mockConfigManager{config: map[string]any{"test": "data"}}
			s := NewServer(cm, tt.port, tt.token, []string{}, []string{}, logger)

			ctx, cancel := context.WithCancel(context.Background())

			// Start server in background
			startErr := make(chan error, 1)
			go func() {
				startErr <- s.Start(ctx)
			}()

			// Give server time to start
			time.Sleep(100 * time.Millisecond)

			// Verify server is responsive
			resp, err := http.Get("http://localhost:" + tt.port + "/health")
			if err != nil {
				t.Errorf("Failed to connect to server: %v", err)
				cancel()
				// Ensure server is stopped even on error path
				_ = s.Stop()
				return
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				t.Errorf("Health check returned status %d, want %d", resp.StatusCode, http.StatusOK)
			}

			// Stop server
			cancel()
			err = s.Stop()
			if (err != nil) != tt.wantErr {
				t.Errorf("Server.Stop() error = %v, wantErr %v", err, tt.wantErr)
			}

			// Verify start error is nil (server shutdown gracefully)
			select {
			case err := <-startErr:
				if err != nil {
					t.Errorf("Server.Start() returned error: %v", err)
				}
			case <-time.After(5 * time.Second):
				t.Error("Server.Start() did not return after context cancellation")
			}
		})
	}
}

func TestServer_InFlightRequestsComplete(t *testing.T) {
	logger := log.New(os.Stdout, "TEST: ", log.LstdFlags)

	// Use a mock with delay to simulate slow endpoint
	cm := &mockConfigManager{
		config: map[string]any{"test": "data"},
		delay:  50 * time.Millisecond, // Simulate slow request
	}
	s := NewServer(cm, "18081", "test-token", []string{}, []string{}, logger)

	ctx, cancel := context.WithCancel(context.Background())

	// Start server
	go func() {
		_ = s.Start(ctx)
	}()

	time.Sleep(100 * time.Millisecond)

	// Make in-flight authenticated request to GetConfig (which has delay)
	reqDone := make(chan struct{})
	go func() {
		req, _ := http.NewRequest("GET", "http://localhost:18081/api/config", nil)
		req.Header.Set("Authorization", "Bearer test-token")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Errorf("Request failed: %v", err)
		} else {
			resp.Body.Close()
		}
		close(reqDone)
	}()

	// Give request time to start (but not complete - it needs 50ms), then stop server
	time.Sleep(10 * time.Millisecond)
	cancel()

	if err := s.Stop(); err != nil {
		t.Errorf("Server.Stop() error = %v", err)
	}

	// Verify request completed (graceful shutdown should wait for it)
	select {
	case <-reqDone:
		// Success - in-flight request completed before shutdown finished
	case <-time.After(2 * time.Second):
		t.Error("In-flight request did not complete before shutdown timeout")
	}
}
