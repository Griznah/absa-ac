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
	cm             ConfigManager
	httpServer     *http.Server
	logger         *log.Logger
	bearerToken    string
	corsOrigins    []string
	trustedProxies []string

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
			ReadTimeout:  15 * time.Second, // Prevents slow clients
			WriteTimeout: 15 * time.Second, // Prevents slow clients
			IdleTimeout:  60 * time.Second,
		},
		rateLimit: rateLimit,
		rateBurst: rateBurst,
	}
}

// Start begins the HTTP server in a background goroutine
// Blocks until the context is cancelled, then returns
// Note: You must call Stop() separately to initiate graceful shutdown
// Returns nil always (server errors are logged, not returned)
func (s *Server) Start(ctx context.Context) error {
	// MIME type configuration for .mjs module files (ES modules)
	// Required for browsers to properly handle JavaScript modules
	mime.AddExtensionType(".mjs", "application/javascript")

	// Set up router with middleware
	mux := http.NewServeMux()

	// Apply middleware chain (order matters: innermost first)
	// Execution order (outer to inner): SecurityHeaders → CORS → Logger → RateLimit → BearerAuth
	securityHeadersMiddleware := SecurityHeaders()
	// CORS: second layer (cross-origin checks before auth)
	corsMiddleware := CORS(s.corsOrigins)
	loggerMiddleware := Logger(s.logger)
	rateLimitMiddleware := RateLimit(10, 20, s.trustedProxies, ctx) // 10 req/sec, burst 20
	authMiddleware := BearerAuth(s.bearerToken, s.trustedProxies)

	var handler http.Handler = mux
	handler = authMiddleware(handler)           // Innermost: check auth first
	handler = rateLimitMiddleware(handler)      // Apply rate limiting before auth
	handler = loggerMiddleware(handler)         // Log after rate limiting
	handler = corsMiddleware(handler)           // Handle CORS before security headers
	handler = securityHeadersMiddleware(handler) // Outermost: security headers applied to all responses

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
