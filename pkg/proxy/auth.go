package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookieName = "proxy_session"
)

var (
	ErrUnauthorized       = errors.New("unauthorized: invalid or missing session")
	ErrInvalidBearerToken = errors.New("invalid bearer token")
)

// ================= RATE LIMITING =================

// Global rate limiter instance for login endpoint
var loginRateLimiter = newRateLimiter()

func init() {
	// Start background cleanup to prevent memory leak from one-time IP attempts
	loginRateLimiter.startBackgroundCleanup()
}

// resetRateLimiterForTests clears all rate limit entries and creates a new limiter
// This function is intended for testing purposes to ensure test isolation
func resetRateLimiterForTests() {
	loginRateLimiter = newRateLimiter()
	loginRateLimiter.startBackgroundCleanup()
}

// Rate limiter using fixed-window algorithm to prevent DoS attacks on login endpoint
// Background cleanup goroutine removes expired entries periodically to prevent memory leaks.
type rateLimiter struct {
	attempts       map[string]*attemptEntry
	mu             sync.Mutex
	windowDuration time.Duration
	maxAttempts    int
}

type attemptEntry struct {
	count      int
	windowStart time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		attempts:       make(map[string]*attemptEntry),
		windowDuration: 60 * time.Second,
		maxAttempts:    5,
	}
}

// checkRateLimit returns true if request is allowed, false if rate limit exceeded
// Implements lazy cleanup: expired entries removed during check
func (rl *rateLimiter) checkRateLimit(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.attempts[ip]

	// Lazy cleanup: remove expired entry
	if exists && now.Sub(entry.windowStart) > rl.windowDuration {
		delete(rl.attempts, ip)
		exists = false
	}

	if !exists {
		return true
	}

	return entry.count < rl.maxAttempts
}

// recordFailedAttempt increments failure counter for IP
func (rl *rateLimiter) recordFailedAttempt(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	entry, exists := rl.attempts[ip]

	if !exists || now.Sub(entry.windowStart) > rl.windowDuration {
		rl.attempts[ip] = &attemptEntry{
			count:      1,
			windowStart: now,
		}
		return
	}

	entry.count++
}

// resetRateLimit clears counter for IP after successful login
func (rl *rateLimiter) resetRateLimit(ip string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.attempts, ip)
}

// cleanup removes expired entries from the rate limiter map
// Should be called periodically to prevent unbounded memory growth
func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, entry := range rl.attempts {
		if now.Sub(entry.windowStart) > rl.windowDuration {
			delete(rl.attempts, ip)
		}
	}
}

// startBackgroundCleanup runs a goroutine that periodically cleans up expired entries
func (rl *rateLimiter) startBackgroundCleanup() {
	go func() {
		ticker := time.NewTicker(rl.windowDuration)
		defer ticker.Stop()
		for range ticker.C {
			rl.cleanup()
		}
	}()
}

// getClientIP extracts the real client IP from request, checking X-Real-IP and
// X-Forwarded-For headers for requests behind proxies. Falls back to RemoteAddr.
// Returns IP:port format for consistency with rate limiter storage.
func getClientIP(r *http.Request) string {
	// Check X-Real-IP header (set by nginx, traefik, etc.)
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return xri
	}

	// Check X-Forwarded-For header (may contain multiple IPs)
	// Format: "X-Forwarded-For: client, proxy1, proxy2"
	// We want the leftmost (original client) IP
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// Split by comma and take the first IP
		if idx := strings.Index(xff, ","); idx != -1 {
			return strings.TrimSpace(xff[:idx])
		}
		return xff
	}

	// Fall back to RemoteAddr
	return r.RemoteAddr
}

type LoginRequest struct {
	Token string `json:"token"`
}

type LoginResponse struct {
	Message   string `json:"message"`
	CSRFToken string `json:"csrf_token"` // CSRF token for subsequent POST/PUT/DELETE requests
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

		session, err := store.Get(sessionID)
		if err != nil {
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "unauthorized: invalid or expired session"})
			return
		}

		ctx := context.WithValue(r.Context(), SessionContextKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func LoginHandler(store *SessionStore, botAPIURL string, useHTTPS bool, upstreamTimeout time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			respondJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		// Extract IP for rate limiting (checks X-Real-IP, X-Forwarded-For, RemoteAddr)
		ip := getClientIP(r)

		// Check rate limit before processing
		if !loginRateLimiter.checkRateLimit(ip) {
			w.Header().Set("Retry-After", "60")
			respondJSON(w, http.StatusTooManyRequests, ErrorResponse{Error: "too many login attempts, please try again later"})
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

		if err := validateBearerTokenWithBotAPI(req.Token, botAPIURL, upstreamTimeout); err != nil {
			log.Printf("Bearer token validation failed: %v", err)
			loginRateLimiter.recordFailedAttempt(ip)
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "invalid bearer token"})
			return
		}

		session, err := store.Create(req.Token, 0)
		if err != nil {
			log.Printf("Failed to create session: %v", err)
			respondJSON(w, http.StatusInternalServerError, ErrorResponse{Error: "failed to create session"})
			return
		}

		// Successful login - reset rate limit for this IP
		loginRateLimiter.resetRateLimit(ip)

		SetSessionCookie(w, session.ID, useHTTPS)

		respondJSON(w, http.StatusOK, LoginResponse{
			Message:   "Login successful",
			CSRFToken: session.CSRFToken,
		})
	}
}

func LogoutHandler(store *SessionStore, useHTTPS bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			respondJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
			return
		}

		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			ClearSessionCookie(w, useHTTPS)
			respondJSON(w, http.StatusOK, LoginResponse{Message: "Logout successful"})
			return
		}

		sessionID := cookie.Value
		if err := store.Delete(sessionID); err != nil {
			log.Printf("Failed to delete session: %v", err)
		}

		ClearSessionCookie(w, useHTTPS)
		respondJSON(w, http.StatusOK, LoginResponse{Message: "Logout successful"})
	}
}

func GetSession(r *http.Request) (*Session, bool) {
	session, ok := r.Context().Value(SessionContextKey).(*Session)
	return session, ok
}

func SetSessionCookie(w http.ResponseWriter, sessionID string, useHTTPS bool) {
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    sessionID,
		Path:     "/proxy",
		MaxAge:   int((4 * time.Hour).Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   useHTTPS,
	}

	http.SetCookie(w, cookie)
}

func ClearSessionCookie(w http.ResponseWriter, useHTTPS bool) {
	cookie := &http.Cookie{
		Name:     sessionCookieName,
		Value:    "",
		Path:     "/proxy",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   useHTTPS,
	}

	http.SetCookie(w, cookie)
}

func validateBearerTokenWithBotAPI(token string, botAPIURL string, timeout time.Duration) error {
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	client := &http.Client{
		Timeout: timeout,
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

func CSRFMiddleware(next http.Handler, store *SessionStore) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(sessionCookieName)
		if err != nil {
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "unauthorized: session cookie required"})
			return
		}

		sessionID := cookie.Value
		if sessionID == "" {
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "unauthorized: empty session ID"})
			return
		}

		session, err := store.Get(sessionID)
		if err != nil {
			respondJSON(w, http.StatusUnauthorized, ErrorResponse{Error: "unauthorized: invalid or expired session"})
			return
		}

		if r.Method != http.MethodGet {
			csrfToken := r.Header.Get("X-CSRF-Token")
			if csrfToken == "" {
				respondJSON(w, http.StatusForbidden, ErrorResponse{Error: "forbidden: CSRF token required"})
				return
			}

			// Compare against the separate CSRF token stored in session
			if csrfToken != session.CSRFToken {
				respondJSON(w, http.StatusForbidden, ErrorResponse{Error: "forbidden: CSRF token mismatch"})
				return
			}
		}

		ctx := context.WithValue(r.Context(), SessionContextKey, session)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("Failed to encode JSON response: %v", err)
	}
}
