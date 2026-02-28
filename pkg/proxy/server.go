package proxy

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Server manages the reverse proxy HTTP server.
// DL-001: Standalone proxy package separate from api package
// DL-008: Health endpoint at /health returns 200 OK
type Server struct {
	httpServer *http.Server
	config     Config
	logger     *log.Logger
	httpClient *http.Client // DL-011: Reused for upstream requests

	// wg tracks graceful shutdown completion
	wg sync.WaitGroup

	// cancel is stored to allow Stop() to cancel the Start() context
	cancel   context.CancelFunc
	cancelMu sync.Mutex
}

// NewServer creates a new proxy server with the given configuration.
// DL-011: HTTP client with 30s timeout, connection pooling (MaxIdleConns=10, IdleConnTimeout=90s)
func NewServer(cfg Config, logger *log.Logger) *Server {
	// Configure HTTP client with timeouts and connection pooling
	// DL-011: Default Go http.Client has no timeout -> risk of hanging requests
	transport := &http.Transport{
		MaxIdleConns:       10,
		IdleConnTimeout:    90 * time.Second,
		DisableCompression: false,
	}

	httpClient := &http.Client{
		Timeout:   30 * time.Second, // DL-011: 30s reasonable for internal API calls
		Transport: transport,
	}

	return &Server{
		config:     cfg,
		logger:     logger,
		httpClient: httpClient,
		httpServer: &http.Server{
			Addr:         ":" + cfg.Port,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}
}

// Start begins the HTTP server in a background goroutine.
// Blocks until Stop() is called, then performs graceful shutdown.
func (s *Server) Start(ctx context.Context) error {
	serverCtx, serverCancel := context.WithCancel(ctx)

	s.cancelMu.Lock()
	s.cancel = serverCancel
	s.cancelMu.Unlock()

	mux := http.NewServeMux()

	// DL-008: Health endpoint bypasses auth (matches existing API pattern)
	mux.HandleFunc("GET /health", s.healthHandler)

	// Apply middleware chain (inside-out): mux -> ProxyHandler -> BasicAuth -> AccessLog
	// Request flow: AccessLog -> BasicAuth -> ProxyHandler -> mux
	handler := ProxyHandler(s.config.APIURL, s.config.BearerToken, s.httpClient, s.logger)(mux)
	handler = BasicAuth(s.config.Username, s.config.Password, s.logger)(handler)
	handler = AccessLog(handler, s.logger)

	s.httpServer.Handler = handler

	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.logger.Printf("Proxy server listening on %s", s.httpServer.Addr)

		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Printf("Proxy server error: %v", err)
		}
	}()

	<-serverCtx.Done()
	s.logger.Println("Shutting down proxy server...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("proxy server shutdown failed: %w", err)
	}

	s.wg.Wait()
	s.logger.Println("Proxy server stopped")

	return nil
}

// Stop gracefully shuts down the HTTP server.
func (s *Server) Stop() error {
	s.cancelMu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.cancelMu.Unlock()

	s.wg.Wait()

	return nil
}

// healthHandler returns 200 OK for health checks.
// DL-008: Matches existing API health endpoint pattern
func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Fprintln(w, "OK")
}
