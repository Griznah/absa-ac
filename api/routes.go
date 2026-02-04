package api

import (
	"net/http"
	"os"
)

// RegisterRoutes registers all API routes with the given mux
// Middleware is applied externally (auth, rate limit, logger, CSRF)
func RegisterRoutes(mux *http.ServeMux, s *Server) {
	// Health check (no auth required, but rate limited)
	mux.HandleFunc("GET /health", HealthCheck)

	// CSRF token endpoint (auth required, returns token for frontend)
	mux.HandleFunc("GET /api/csrf-token", s.GetCSRFTokenHandler)

	// Config endpoints (auth + rate limit + CSRF applied externally)
	mux.HandleFunc("GET /api/config", s.GetConfig)
	mux.HandleFunc("GET /api/config/servers", s.GetServers)
	mux.HandleFunc("PATCH /api/config", s.PatchConfig)
	mux.HandleFunc("PUT /api/config", s.PutConfig)
	mux.HandleFunc("POST /api/config/validate", s.ValidateConfig)

	// Static file server for web UI
	// Serves files from ./static directory at /static/ URL path
	// Security: http.StripPrefix removes /static prefix before serving
	// http.FileServer prevents directory traversal attacks
	staticDir := "./static"
	if _, err := os.Stat(staticDir); err == nil {
		// StripPrefix removes /static prefix so requests to /static/index.html
		// serve ./static/index.html
		fs := http.StripPrefix("/static/", http.FileServer(http.Dir(staticDir)))
		// Handle all paths under /static/ with trailing slash redirect
		mux.Handle("/static/", fs)
		mux.Handle("/static", http.RedirectHandler("/static/", http.StatusMovedPermanently))
	}
}
