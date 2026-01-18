package api

import (
	"net/http"
)

// RegisterRoutes registers all API routes with the given mux
// Middleware is applied externally (auth, rate limit, logger)
func RegisterRoutes(mux *http.ServeMux, s *Server) {
	// Health check (no auth required)
	mux.HandleFunc("GET /health", HealthCheck)

	// Config endpoints (auth + rate limit applied externally)
	mux.HandleFunc("GET /api/config", s.GetConfig)
	mux.HandleFunc("GET /api/config/servers", s.GetServers)
	mux.HandleFunc("PATCH /api/config", s.PatchConfig)
	mux.HandleFunc("PUT /api/config", s.PutConfig)
	mux.HandleFunc("POST /api/config/validate", s.ValidateConfig)
}
