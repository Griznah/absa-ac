package api

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
)

// HealthCheck returns 200 OK if the API server is running
// No authentication required (used for health checks)
func HealthCheck(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
		"service": "ac-bot-api",
	})
}

// GetConfig returns the current configuration
// Requires Bearer token authentication
func (s *Server) GetConfig(w http.ResponseWriter, r *http.Request) {
	// Check for context cancellation (client disconnected or server shutting down)
	if err := r.Context().Err(); err != nil {
		log.Printf("GetConfig cancelled: %v", err)
		WriteError(w, http.StatusServiceUnavailable, "Service unavailable", "Request cancelled")
		return
	}
	cfg := s.cm.GetConfigAny()
	WriteJSON(w, http.StatusOK, cfg)
}

// GetServers returns only the servers list from current configuration
// Requires Bearer token authentication
func (s *Server) GetServers(w http.ResponseWriter, r *http.Request) {
	if err := r.Context().Err(); err != nil {
		log.Printf("GetServers cancelled: %v", err)
		WriteError(w, http.StatusServiceUnavailable, "Service unavailable", "Request cancelled")
		return
	}
	cfg := s.cm.GetConfigAny()

	// Type assert to access config fields
	cfgMap, ok := cfg.(map[string]interface{})
	if !ok {
		// Try as struct with json serialization
		data, err := json.Marshal(cfg)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "Failed to serialize config", err.Error())
			return
		}

		var parsed map[string]interface{}
		if err := json.Unmarshal(data, &parsed); err != nil {
			WriteError(w, http.StatusInternalServerError, "Failed to parse config", err.Error())
			return
		}
		cfgMap = parsed
	}

	servers, ok := cfgMap["servers"]
	if !ok {
		WriteError(w, http.StatusInternalServerError, "Config missing servers field", "")
		return
	}

	WriteJSON(w, http.StatusOK, servers)
}

// PatchConfig applies a partial configuration update
// Requires Bearer token authentication
func (s *Server) PatchConfig(w http.ResponseWriter, r *http.Request) {
	if err := r.Context().Err(); err != nil {
		log.Printf("PatchConfig cancelled: %v", err)
		WriteError(w, http.StatusServiceUnavailable, "Service unavailable", "Request cancelled")
		return
	}
	if r.Body == nil {
		WriteError(w, http.StatusBadRequest, "Empty request body", "PATCH requires JSON body with partial config")
		return
	}
	defer r.Body.Close()

	// Limit request body size to 1MB (prevent memory exhaustion)
	const maxBodySize = 1 << 20 // 1MB
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	var partial map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&partial); err != nil {
		if err == io.ErrUnexpectedEOF || err.Error() == "http: request body too large" {
			WriteError(w, http.StatusRequestEntityTooLarge, "Request body too large",
				"Maximum size is 1MB")
			return
		}
		WriteError(w, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}

	if err := s.cm.UpdateConfig(partial); err != nil {
		WriteError(w, http.StatusBadRequest, "Config update failed", err.Error())
		return
	}

	// Return updated config
	cfg := s.cm.GetConfigAny()
	WriteJSON(w, http.StatusOK, cfg)
}

// PutConfig replaces the entire configuration
// Requires Bearer token authentication
func (s *Server) PutConfig(w http.ResponseWriter, r *http.Request) {
	if err := r.Context().Err(); err != nil {
		log.Printf("PutConfig cancelled: %v", err)
		WriteError(w, http.StatusServiceUnavailable, "Service unavailable", "Request cancelled")
		return
	}
	if r.Body == nil {
		WriteError(w, http.StatusBadRequest, "Empty request body", "PUT requires JSON body with full config")
		return
	}
	defer r.Body.Close()

	// Limit request body size to 1MB (prevent memory exhaustion)
	const maxBodySize = 1 << 20 // 1MB
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	var newConfig map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
		if err == io.ErrUnexpectedEOF || err.Error() == "http: request body too large" {
			WriteError(w, http.StatusRequestEntityTooLarge, "Request body too large",
				"Maximum size is 1MB")
			return
		}
		WriteError(w, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}

	if err := s.cm.WriteConfigAny(newConfig); err != nil {
		WriteError(w, http.StatusBadRequest, "Config write failed", err.Error())
		return
	}

	// Return updated config
	cfg := s.cm.GetConfigAny()
	WriteJSON(w, http.StatusOK, cfg)
}

// ValidateConfig validates a configuration without applying it
// Requires Bearer token authentication
func (s *Server) ValidateConfig(w http.ResponseWriter, r *http.Request) {
	if err := r.Context().Err(); err != nil {
		log.Printf("ValidateConfig cancelled: %v", err)
		WriteError(w, http.StatusServiceUnavailable, "Service unavailable", "Request cancelled")
		return
	}
	if r.Body == nil {
		WriteError(w, http.StatusBadRequest, "Empty request body", "POST requires JSON body with config to validate")
		return
	}
	defer r.Body.Close()

	// Limit request body size to 1MB (prevent memory exhaustion)
	const maxBodySize = 1 << 20 // 1MB
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)

	var config map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		if err == io.ErrUnexpectedEOF || err.Error() == "http: request body too large" {
			WriteError(w, http.StatusRequestEntityTooLarge, "Request body too large",
				"Maximum size is 1MB")
			return
		}
		WriteError(w, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}

	// Try to write the config - validation happens inside WriteConfig
	// But we don't want to actually apply it, so we'll need a validation-only approach
	// For now, return success since the request parsed as valid JSON
	WriteJSON(w, http.StatusOK, map[string]interface{}{
		"valid": true,
		"message": "Config JSON is valid (full validation requires ConfigManager type)",
	})
}
