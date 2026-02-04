package api

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

// Server manages the HTTP API for config management
// Runs in separate goroutine from Discord bot, neither blocks the other
type Server struct {
	cm             ConfigManager
	httpServer     *http.Server
	logger         *log.Logger
	bearerToken    string
	corsOrigins    []string
	trustedProxies []string

	// wg tracks graceful shutdown completion
	wg sync.WaitGroup

	// cancel is stored to allow Stop() to cancel the Start() context
	cancel context.CancelFunc
	cancelMu sync.Mutex
}

// ConfigManager defines the interface for accessing and modifying config
// Using any allows the API package to work with main.ConfigManager without circular imports
type ConfigManager interface {
	GetConfigAny() any
	WriteConfigAny(any) error
	UpdateConfig(map[string]interface{}) error
}

// NewServer creates a new API server with the given config manager and configuration
// Port is the listen address (e.g., "3001" for :3001)
// Bearer token is required for all authenticated endpoints
// CORS origins is a list of allowed origins (empty = no CORS, "*" = all)
// Trusted proxies is a list of proxy IPs to trust for X-Forwarded-For validation
func NewServer(cm ConfigManager, port string, bearerToken string, corsOrigins []string, trustedProxies []string, logger *log.Logger) *Server {
	return &Server{
		cm:             cm,
		bearerToken:    bearerToken,
		corsOrigins:    corsOrigins,
		trustedProxies: trustedProxies,
		logger:         logger,
		httpServer: &http.Server{
			Addr:         ":" + port,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}
}

// Start begins the HTTP server in a background goroutine
// Blocks until Stop() is called, then performs graceful shutdown
// Returns error if graceful shutdown fails
func (s *Server) Start(ctx context.Context) error {
	// Create a cancellable context for this server
	// This allows Stop() to cancel it without needing access to the caller's context
	serverCtx, serverCancel := context.WithCancel(ctx)

	s.cancelMu.Lock()
	s.cancel = serverCancel
	s.cancelMu.Unlock()

	// Set up router with middleware
	mux := http.NewServeMux()

	// Apply middleware chain (order matters: each middleware wraps the previous one)
	// Execution order (outer to inner): SecurityHeaders → CORS → RateLimit → Logger → BearerAuth
	securityHeadersMiddleware := SecurityHeaders()
	corsMiddleware := CORS(s.corsOrigins)
	rateLimitMiddleware := RateLimit(10, 20, s.trustedProxies, serverCtx) // 10 req/sec, burst 20
	loggerMiddleware := Logger(s.logger)
	authMiddleware := BearerAuth(s.bearerToken, s.trustedProxies)

	var handler http.Handler = mux
	handler = authMiddleware(handler)                    // Innermost: check auth last
	handler = rateLimitMiddleware(handler)               // Apply rate limiting before expensive auth
	handler = loggerMiddleware(handler)                  // Log all requests including rate limited ones
	handler = corsMiddleware(handler)                    // Handle CORS preflight before rate limiting
	handler = securityHeadersMiddleware(handler)         // Outermost: security headers applied to all responses

	s.httpServer.Handler = handler

	// Register routes
	RegisterRoutes(mux, s)

	// Start server in background
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.logger.Printf("API server listening on %s", s.httpServer.Addr)

		// ListenAndServe blocks until server shutdown
		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.Printf("API server error: %v", err)
		}
	}()

	// Wait for context cancellation
	<-serverCtx.Done()
	s.logger.Println("Shutting down API server...")

	// Initiate graceful shutdown
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("API server shutdown failed: %w", err)
	}

	// Wait for server goroutine to finish
	s.wg.Wait()
	s.logger.Println("API server stopped")

	return nil
}

// Stop gracefully shuts down the HTTP server
// Allows in-flight requests up to 30 seconds to complete
// Called by main bot during shutdown sequence
func (s *Server) Stop() error {
	s.cancelMu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.cancelMu.Unlock()

	// Wait for Start goroutine to finish (which handles the shutdown)
	s.wg.Wait()

	return nil
}
