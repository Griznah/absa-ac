package api

import (
	"context"
	"fmt"
	"log"
	"mime"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

// Server manages the HTTP API for config management
// Runs in separate goroutine from Discord bot, neither blocks the other
type Server struct {
	cm           ConfigManager
	httpServer   *http.Server
	logger       *log.Logger
	bearerToken  string
	corsOrigins  []string
	rateLimit    int
	rateBurst    int

	// wg tracks graceful shutdown completion
	wg sync.WaitGroup
}

// ConfigManager defines the interface for accessing and modifying config
// Using any allows the API package to work with main.ConfigManager without circular imports
type ConfigManager interface {
	GetConfigAny() any
	WriteConfigAny(any) error
	UpdateConfig(map[string]interface{}) error
}

// NewServer creates a new API server with the given config manager and configuration
// Port is the listen address (e.g., "8080" for :8080)
// Bearer token is required for all authenticated endpoints
// CORS origins is a list of allowed origins (empty = no CORS, "*" = all)
// Rate limits read from env vars API_RATE_LIMIT (default 10) and API_RATE_BURST (default 20)
func NewServer(cm ConfigManager, port string, bearerToken string, corsOrigins []string, logger *log.Logger) *Server {
	// Read rate limit from environment (allows runtime configuration)
	rateLimit := 10
	rateBurst := 20
	if val := os.Getenv("API_RATE_LIMIT"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			rateLimit = n
		}
	}
	if val := os.Getenv("API_RATE_BURST"); val != "" {
		if n, err := strconv.Atoi(val); err == nil && n > 0 {
			rateBurst = n
		}
	}

	return &Server{
		cm:          cm,
		bearerToken: bearerToken,
		corsOrigins: corsOrigins,
		logger:      logger,
		httpServer: &http.Server{
			Addr:         ":" + port,
			ReadTimeout:  15 * time.Second, // Prevents slow clients
			WriteTimeout: 15 * time.Second, // Prevents slow clients
			IdleTimeout:  60 * time.Second,
		},
		rateLimit: rateLimit,
		rateBurst: rateBurst,
	}
}

// Start begins the HTTP server in a background goroutine
// Blocks until the context is cancelled, then initiates graceful shutdown
// Returns error if server fails to start (listen errors)
func (s *Server) Start(ctx context.Context) error {
	// MIME type configuration for .mjs module files (ES modules)
	// Required for browsers to properly handle JavaScript modules
	mime.AddExtensionType(".mjs", "application/javascript")

	// Set up router with middleware
	mux := http.NewServeMux()

	// Apply middleware chain (order matters: outermost first)
	// Security headers: outermost (applies to all responses even on error)
	securityHeadersMiddleware := SecurityHeaders()
	// CORS: second layer (cross-origin checks before auth)
	corsMiddleware := CORS(s.corsOrigins)
	// Logger: third layer (logs all requests)
	loggerMiddleware := Logger(s.logger)
	// Rate limiting: fourth layer (throttling before expensive auth)
	rateLimitMiddleware := RateLimit(s.rateLimit, s.rateBurst)
	// Auth: innermost (validates Bearer token only after other checks pass)
	authMiddleware := BearerAuth(s.bearerToken)

	var handler http.Handler = mux
	handler = securityHeadersMiddleware(handler)
	handler = corsMiddleware(handler)
	handler = loggerMiddleware(handler)
	handler = rateLimitMiddleware(handler)
	handler = authMiddleware(handler)

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
	<-ctx.Done()
	s.logger.Println("Shutting down API server...")

	return nil
}

// Stop gracefully shuts down the HTTP server
// Allows in-flight requests up to 30 seconds to complete
// Called by main bot during shutdown sequence
func (s *Server) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := s.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("API server shutdown failed: %w", err)
	}

	// Wait for Start goroutine to finish
	s.wg.Wait()
	s.logger.Println("API server stopped")

	return nil
}
