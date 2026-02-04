package api

import (
	"encoding/json"
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
	cfg := s.cm.GetConfigAny()
	WriteJSON(w, http.StatusOK, cfg)
}

// GetServers returns only the servers list from current configuration
// Requires Bearer token authentication
func (s *Server) GetServers(w http.ResponseWriter, r *http.Request) {
	cfg := s.cm.GetConfigAny()

	// Serialize and deserialize to extract servers field
	// Note: GetConfigAny returns *Config from main.go, which we can't import
	// due to circular dependency, so we use JSON serialization to extract fields
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

	servers, ok := parsed["servers"]
	if !ok {
		WriteError(w, http.StatusInternalServerError, "Config missing servers field", "")
		return
	}

	WriteJSON(w, http.StatusOK, servers)
}

// PatchConfig applies a partial configuration update
// Requires Bearer token authentication
func (s *Server) PatchConfig(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		WriteError(w, http.StatusBadRequest, "Empty request body", "PATCH requires JSON body with partial config")
		return
	}
	defer r.Body.Close()

	var partial map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&partial); err != nil {
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
	if r.Body == nil {
		WriteError(w, http.StatusBadRequest, "Empty request body", "PUT requires JSON body with full config")
		return
	}
	defer r.Body.Close()

	var newConfig map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&newConfig); err != nil {
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
// NOTE: This endpoint only validates JSON syntax, not schema or business logic.
// Full validation requires access to the ConfigManager's validation logic which
// is defined in main.go and cannot be imported here due to circular dependency.
func (s *Server) ValidateConfig(w http.ResponseWriter, r *http.Request) {
	if r.Body == nil {
		WriteError(w, http.StatusBadRequest, "Empty request body", "POST requires JSON body with config to validate")
		return
	}
	defer r.Body.Close()

	var config map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		WriteError(w, http.StatusBadRequest, "Invalid JSON", err.Error())
		return
	}

	// Only JSON syntax validation is performed here
	// Full schema validation (field presence, types, business rules) is not available
	// through the ConfigManager interface without creating a circular dependency
	WriteJSON(w, http.StatusNotImplemented, map[string]interface{}{
		"valid":      false,
		"json_syntax": true,
		"message":   "JSON syntax is valid, but full schema validation is not available through this endpoint",
		"note":      "Apply the config via PUT /api/config to trigger full validation",
	})
}
