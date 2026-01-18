package api

import (
	"encoding/json"
	"net/http"
)

// ErrorResponse represents an error response
// Error: short error message
// Details: optional detailed explanation
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// SuccessResponse represents a success response with data
type SuccessResponse struct {
	Data interface{} `json:"data"`
}

// WriteJSON writes a JSON response with the given status code and data
// Handles JSON encoding and sets proper Content-Type header
func WriteJSON(w http.ResponseWriter, status int, data interface{}) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	return json.NewEncoder(w).Encode(data)
}

// WriteError writes an error response with status code and error details
// Details is optional - pass empty string to omit
func WriteError(w http.ResponseWriter, status int, err string, details string) error {
	resp := ErrorResponse{
		Error:   err,
		Details: details,
	}
	return WriteJSON(w, status, resp)
}
