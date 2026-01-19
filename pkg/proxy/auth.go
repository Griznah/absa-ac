package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"
)

const (
	sessionCookieName = "proxy_session"
)

var (
	ErrUnauthorized       = errors.New("unauthorized: invalid or missing session")
	ErrInvalidBearerToken = errors.New("invalid bearer token")
)

type LoginRequest struct {
	Token string `json:"token"`
}

type LoginResponse struct {
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func AuthMiddleware(next http.Handler, store *SessionStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			if errors.Is(err, http.ErrNoCookie) {
				respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "unauthorized: session cookie required"})
				return
			}
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "unauthorized: invalid cookie"})
			return
		}

		sessionID := cookie.Value
		if sessionID == "" {
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "unauthorized: empty session ID"})
			return
		}

		session, exists := store.Get(sessionID)
		if !exists {
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "unauthorized: invalid or expired session"})
			return
		}

		ctx := context.WithValue(r.Context(), SessionContextKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func LoginHandler(store *SessionStore, botAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			respondJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		var req LoginRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid request body"})
			return
		}

		if req.Token == "" {
			respondJSON(w, http.StatusBadRequest, ErrorResponse{Error: "bearer token is required"})
			return
		}

		if !strings.HasPrefix(req.Token, "Bearer ") {
			respondJSON(w, http.StatusBadRequest, ErrorResponse{Error: "token must use Bearer prefix"})
			return
		}

		if err := validateBearerTokenWithBotAPI(req.Token, botAPIURL); err != nil {
			log.Printf("Bearer token validation failed: %v", err)
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "invalid bearer token"})
			return
		}

		session, err := store.Create(req.Token, 0)
		if err != nil {
			log.Printf("Failed to create session: %v", err)
			respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to create session"})
			return
		}

		SetSessionCookie(w, session.ID)

		respondJSON(w, http.StatusOK, LoginResponse{Message: "Login successful"})
	}
}

func LogoutHandler(store *SessionStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			respondJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			ClearSessionCookie(w)
			respondJSON(w, http.StatusOK, LoginResponse{Message: "Logout successful"})
			return
		}

		sessionID := cookie.Value
		if err := store.Delete(sessionID); err != nil {
			log.Printf("Failed to delete session: %v", err)
		}

		ClearSessionCookie(w)
		respondJSON(w, http.StatusOK, LoginResponse{Message: "Logout successful"})
	}
}

func GetSession(r *http.Request) (*Session, bool) {
	session, ok := r.Context().Value(SessionContextKey).(*Session)
	return session, ok
}

func SetSessionCookie(w http.ResponseWriter, sessionID string) {
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/proxy",
		MaxAge:   int((4 * time.Hour).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isHTTPS(),
	}

	http.SetCookie(w, cookie)
}

func ClearSessionCookie(w http.ResponseWriter) {
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/proxy",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   isHTTPS(),
	}

	http.SetCookie(w, cookie)
}

func validateBearerTokenWithBotAPI(token string, botAPIURL string) error {
	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	req, err := http.NewRequest("GET", botAPIURL+"/api/config", nil)
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", token)

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return ErrInvalidBearerToken
	}

	return nil
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}

func isHTTPS() bool {
	return false
}
