# Plan

## Overview

Admin UI requires manual Bearer token entry which is not browser-native. Users want simpler browser-based access using HTTP Basic Auth for automatic browser login dialogs.

**Approach**: Create standalone reverse proxy package (pkg/proxy) that accepts HTTP Basic Auth, validates credentials, forwards requests to existing API with Bearer token injection. Proxy runs independently from main bot on separate port (default 8080).

### Proxy Architecture

```
Browser --[Basic Auth]--> Proxy Server --[Bearer Token]--> API Server --> ConfigManager
```

## Planning Context

### Decision Log

| ID | Decision | Reasoning Chain |
|---|---|---|
| DL-001 | Create standalone proxy package (pkg/proxy) separate from api package | Proxy is optional/separate from main bot -> independent package allows separate binary deployment -> no modification to existing API auth (out of scope constraint) -> cleaner separation of concerns |
| DL-002 | Use HTTP Basic Auth (RFC 7617) for browser-native authentication | Browser handles Basic Auth dialog natively -> no custom login form needed -> credentials in Authorization header per RFC -> simpler UX for web access |
| DL-003 | Proxy injects Bearer token when forwarding to API | Existing API requires Bearer token (invariant) -> proxy validates Basic Auth credentials -> replaces Authorization header with Bearer token -> API unchanged |
| DL-004 | Proxy runs on configurable separate port (default 8080) | API already on port 3001 -> separate port allows independent operation -> configurable via PROXY_PORT env var |
| DL-005 | Single credential pair via environment variables (PROXY_USER, PROXY_PASSWORD) | Single user or shared credentials acceptable (M priority assumption) -> environment variables match existing API_BEARER_TOKEN pattern -> simple configuration |
| DL-006 | Proxy forwards to configurable API URL (default localhost:API_PORT) | Allows proxy to run on different host from API -> configurable via PROXY_API_URL env var -> supports containerized deployment |
| DL-007 | Constant-time password comparison using crypto/subtle.ConstantTimeCompare | Prevents timing attacks on password -> matches existing BearerAuth pattern in api/middleware.go -> security consistency |
| DL-008 | Proxy includes health endpoint at /health (no auth required) | Matches existing API health endpoint pattern -> allows load balancer health checks -> consistent observability |
| DL-009 | Proxy supports optional startup via PROXY_ENABLED environment variable | Optional component matches API_ENABLED pattern -> can run independently or as part of main binary -> flexible deployment |
| DL-010 | TLS termination handled by reverse proxy/load balancer, not proxy itself | Proxy is internal component -> TLS termination at edge (ingress/load balancer) -> follows standard deployment patterns -> simpler proxy implementation -> consistent with existing API deployment |
| DL-011 | HTTP client with 30s timeout, connection pooling (MaxIdleConns=10, IdleConnTimeout=90s), reuse across requests | Default Go http.Client has no timeout -> risk of hanging requests -> 30s reasonable for internal API calls -> connection pooling reduces latency -> reuse client for efficiency |
| DL-012 | Proxy does NOT implement rate limiting - relies on API rate limiting | API already has rate limiting middleware -> duplicate rate limiting adds complexity -> proxy is passthrough for auth translation -> consistent with out-of-scope for API modification |
| DL-013 | Graceful degradation: return 502 Bad Gateway on upstream failure, 504 Gateway Timeout on timeout, include error message in response body | Standard HTTP proxy error codes -> clients understand gateway errors -> error message aids debugging -> no retry logic (keep proxy simple) |
| DL-014 | Credential rotation is manual: update env vars and restart proxy | Single credential pair (assumption) -> rotation is rare operational task -> no runtime rotation needed -> restart is acceptable -> documented in operational procedures |
| DL-015 | Fail-fast: PROXY_ENABLED=true with missing/invalid credentials causes fatal error at startup | Security-sensitive component -> silent fallback could expose API -> explicit failure alerts operator -> no partial operation -> consistent with API_BEARER_TOKEN validation pattern |
| DL-016 | 8+ character minimum for password matches OWASP minimum and provides reasonable security margin | OWASP recommends minimum 8 characters -> shorter passwords vulnerable to brute force -> consistent with industry standards -> not excessive for operational use |

### Constraints

- MUST: Use HTTP Basic Auth (browser-native login dialog)
- MUST: Forward authenticated requests to existing API (localhost:API_PORT)
- MUST: Be optional/separate from main bot (can run independently)
- SHOULD: Minimal dependencies, simple Go implementation
- SHOULD: Support configurable credentials via environment variables
- OUT OF SCOPE: Modifying existing API auth (Bearer token)
- OUT OF SCOPE: Changing admin UI
- OUT OF SCOPE: Modifying Discord bot

### Known Risks

- **Basic Auth sends credentials with every request (credentials visible in browser dev tools)**: Document tradeoff clearly; recommend HTTPS in production; credentials only for proxy access, not API
- **Single credential pair means no per-user audit trail**: Acceptable for single admin or small team; log proxy access with source IP for basic audit
- **Proxy adds hop latency to all API requests**: Proxy runs on same host as API by default (localhost); minimal latency impact

## Invisible Knowledge

### System

AC Discord Bot with REST API - Go-based Discord bot for monitoring Assetto Corsa racing servers. Admin UI at /admin/ uses vanilla JS with Bearer token stored in sessionStorage.

### Invariants

- Existing API always requires Bearer token authentication (RFC 6750)
- Proxy must inject Bearer token - API auth remains unchanged
- Basic Auth credentials sent with every request (less secure than Bearer)
- Proxy is optional - can run independently or disabled entirely

### Tradeoffs

- Basic Auth vs Bearer token: Basic Auth is browser-native but sends credentials with every request; Bearer is more secure but requires manual entry in admin UI
- Single credential pair vs multiple users: Simpler configuration but no per-user access control
- Separate port vs same port: Cleaner separation but requires additional port management

## Milestones

### Milestone 1: Foundation: Proxy package structure and configuration

**Files**: pkg/proxy/server.go, pkg/proxy/config.go

**Acceptance Criteria**:

- Config loads all env vars correctly
- Validate rejects credentials <8 chars
- fail-fast on missing credentials with PROXY_ENABLED=true
- HTTP client configured with timeouts and connection pooling

**Tests**:

- TestConfigLoadFromEnv
- TestConfigValidation
- TestConfigFailFast

#### Code Intent

- **CI-M-001-001** `pkg/proxy/config.go`: Config struct with fields: Port (default 8080), APIURL (default localhost:3001), Username, Password, BearerToken. LoadFromEnv() reads PROXY_PORT, PROXY_API_URL, PROXY_USER, PROXY_PASSWORD, PROXY_BEARER_TOKEN (defaults to API_BEARER_TOKEN). Validate() ensures username and password are 8+ chars when PROXY_ENABLED=true; fatal error on validation failure. (refs: DL-004, DL-005, DL-006, DL-015, DL-016)
- **CI-M-001-002** `pkg/proxy/server.go`: Server struct with http.Server, config, logger, http.Client (reused for upstream requests). NewServer() constructor creates HTTP client with 30s timeout, MaxIdleConns=10, IdleConnTimeout=90s. Start(ctx) begins listening, supports graceful shutdown via context. Stop() method for clean termination. Health endpoint at /health returns 200 OK. (refs: DL-001, DL-008, DL-011)

#### Code Changes

**CC-M-001-001** (pkg/proxy/config.go) - implements CI-M-001-001

```diff
--- a/pkg/proxy/config.go
+++ b/pkg/proxy/config.go
@@ -0,0 +1,74 @@
+package proxy
+
+import (
+	"fmt"
+	"os"
+)
+
+// Config holds proxy server configuration loaded from environment variables.
+// DL-004: Proxy runs on configurable separate port (default 8080)
+// DL-005: Single credential pair via environment variables
+// DL-006: Proxy forwards to configurable API URL
+type Config struct {
+	Port         string // Port to listen on (default: 8080)
+	APIURL       string // URL of the upstream API (default: http://localhost:3001)
+	Username     string // Basic Auth username
+	Password     string // Basic Auth password
+	BearerToken  string // Bearer token for API authentication
+}
+
+// LoadFromEnv reads configuration from environment variables.
+// DL-006: PROXY_API_URL allows proxy to run on different host from API
+// PROXY_BEARER_TOKEN defaults to API_BEARER_TOKEN for convenience
+func LoadFromEnv() Config {
+	port := os.Getenv("PROXY_PORT")
+	if port == "" {
+		port = "8080"
+	}
+
+	apiURL := os.Getenv("PROXY_API_URL")
+	if apiURL == "" {
+		apiURL = "http://localhost:3001"
+	}
+
+	bearerToken := os.Getenv("PROXY_BEARER_TOKEN")
+	if bearerToken == "" {
+		bearerToken = os.Getenv("API_BEARER_TOKEN")
+	}
+
+	return Config{
+		Port:        port,
+		APIURL:      apiURL,
+		Username:    os.Getenv("PROXY_USER"),
+		Password:    os.Getenv("PROXY_PASSWORD"),
+		BearerToken: bearerToken,
+	}
+}
+
+// Validate ensures configuration is valid before starting the proxy.
+// DL-015: Fail-fast on missing/invalid credentials with PROXY_ENABLED=true
+// DL-016: 8+ character minimum for password (OWASP minimum)
+func (c Config) Validate() error {
+	if c.Username == "" {
+		return fmt.Errorf("PROXY_USER is required when PROXY_ENABLED=true")
+	}
+	if len(c.Password) < 8 {
+		return fmt.Errorf("PROXY_PASSWORD must be at least 8 characters (got %d)", len(c.Password))
+	}
+
+	if c.BearerToken == "" {
+		return fmt.Errorf("PROXY_BEARER_TOKEN (or API_BEARER_TOKEN) is required when PROXY_ENABLED=true")
+	}
+
+	return nil
+}
```

**CC-M-001-002** (pkg/proxy/server.go) - implements CI-M-001-002

```diff
--- a/pkg/proxy/server.go
+++ b/pkg/proxy/server.go
@@ -0,0 +1,104 @@
+package proxy
+
+import (
+	"context"
+	"fmt"
+	"log"
+	"net/http"
+	"sync"
+	"time"
+)
+
+// Server manages the reverse proxy HTTP server.
+// DL-001: Standalone proxy package separate from api package
+// DL-008: Health endpoint at /health returns 200 OK
+type Server struct {
+	httpServer *http.Server
+	config     Config
+	logger     *log.Logger
+	httpClient *http.Client // DL-011: Reused for upstream requests
+
+	// wg tracks graceful shutdown completion
+	wg sync.WaitGroup
+
+	// cancel is stored to allow Stop() to cancel the Start() context
+	cancel   context.CancelFunc
+	cancelMu sync.Mutex
+}
+
+// NewServer creates a new proxy server with the given configuration.
+// DL-011: HTTP client with 30s timeout, connection pooling (MaxIdleConns=10, IdleConnTimeout=90s)
+func NewServer(cfg Config, logger *log.Logger) *Server {
+	// Configure HTTP client with timeouts and connection pooling
+	// DL-011: Default Go http.Client has no timeout -> risk of hanging requests
+	transport := &http.Transport{
+		MaxIdleConns:        10,
+		IdleConnTimeout:     90 * time.Second,
+		DisableCompression:  false,
+	}
+
+	httpClient := &http.Client{
+		Timeout:   30 * time.Second, // DL-011: 30s reasonable for internal API calls
+		Transport: transport,
+	}
+
+	return &Server{
+		config:     cfg,
+		logger:     logger,
+		httpClient: httpClient,
+		httpServer: &http.Server{
+			Addr:         ":" + cfg.Port,
+			ReadTimeout:  15 * time.Second,
+			WriteTimeout: 15 * time.Second,
+			IdleTimeout:  60 * time.Second,
+		},
+	}
+}
+
+// Start begins the HTTP server in a background goroutine.
+// Blocks until Stop() is called, then performs graceful shutdown.
+func (s *Server) Start(ctx context.Context) error {
+	serverCtx, serverCancel := context.WithCancel(ctx)
+
+	s.cancelMu.Lock()
+	s.cancel = serverCancel
+	s.cancelMu.Unlock()
+
+	mux := http.NewServeMux()
+
+	// DL-008: Health endpoint bypasses auth (matches existing API pattern)
+	mux.HandleFunc("GET /health", s.healthHandler)
+
+	// Apply middleware chain (inside-out): mux -> ProxyHandler -> BasicAuth -> AccessLog
+	// Request flow: AccessLog -> BasicAuth -> ProxyHandler -> mux
+	handler := ProxyHandler(s.config.APIURL, s.config.BearerToken, s.httpClient, s.logger)(mux)
+	handler = BasicAuth(s.config.Username, s.config.Password, s.logger)(handler)
+	handler = AccessLog(handler, s.logger)
+
+	s.httpServer.Handler = handler
+
+	s.wg.Add(1)
+	go func() {
+		defer s.wg.Done()
+		s.logger.Printf("Proxy server listening on %s", s.httpServer.Addr)
+
+		if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
+			s.logger.Printf("Proxy server error: %v", err)
+		}
+	}()
+
+	<-serverCtx.Done()
+	s.logger.Println("Shutting down proxy server...")
+
+	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
+	defer cancel()
+
+	if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
+		return fmt.Errorf("proxy server shutdown failed: %w", err)
+	}
+
+	s.wg.Wait()
+	s.logger.Println("Proxy server stopped")
+
+	return nil
+}
+
+// Stop gracefully shuts down the HTTP server.
+func (s *Server) Stop() error {
+	s.cancelMu.Lock()
+	if s.cancel != nil {
+		s.cancel()
+	}
+	s.cancelMu.Unlock()
+
+	s.wg.Wait()
+
+	return nil
+}
+
+// healthHandler returns 200 OK for health checks.
+// DL-008: Matches existing API health endpoint pattern
+func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
+	w.WriteHeader(http.StatusOK)
+	fmt.Fprintln(w, "OK")
+}
```

### Milestone 2: Auth: HTTP Basic Auth middleware

**Files**: pkg/proxy/auth.go

**Acceptance Criteria**:

- BasicAuth validates Authorization header correctly
- Constant-time comparison prevents timing attacks
- 401 response includes WWW-Authenticate header
- /health bypasses auth
- Auth failures logged with source IP

**Tests**:

- TestBasicAuthMiddleware
- TestBasicAuthTimingAttack
- TestBasicAuthHealthBypass
- TestBasicAuthLogging

#### Code Intent

- **CI-M-002-001** `pkg/proxy/auth.go`: BasicAuth middleware validates Authorization header format (Basic base64(user:pass)). Uses crypto/subtle.ConstantTimeCompare for password validation. Returns 401 Unauthorized with WWW-Authenticate header on failure. Bypasses auth for /health endpoint. Logs authentication failures at WARN level with source IP. (refs: DL-002, DL-007)

#### Code Changes

**CC-M-002-001** (pkg/proxy/auth.go) - implements CI-M-002-001

```diff
--- a/pkg/proxy/auth.go
+++ b/pkg/proxy/auth.go
@@ -0,0 +1,70 @@
+package proxy
+
+import (
+	"crypto/subtle"
+	"encoding/base64"
+	"fmt"
+	"log"
+	"net/http"
+	"strings"
+)
+
+// BasicAuth middleware validates HTTP Basic Auth credentials.
+// DL-002: Uses HTTP Basic Auth (RFC 7617) for browser-native authentication
+// DL-007: Constant-time password comparison prevents timing attacks
+func BasicAuth(username, password string, logger *log.Logger) func(http.Handler) http.Handler {
+	return func(next http.Handler) http.Handler {
+		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+			// DL-008: Health endpoint bypasses auth (matches existing API pattern)
+			if r.URL.Path == "/health" {
+				next.ServeHTTP(w, r)
+				return
+			}
+
+			auth := r.Header.Get("Authorization")
+			if auth == "" {
+				// DL-002: 401 response includes WWW-Authenticate header for browser dialog
+				w.Header().Set("WWW-Authenticate", `Basic realm="Proxy"`)
+				writeProxyError(w, http.StatusUnauthorized, "Missing Authorization header")
+				return
+			}
+
+			// Validate "Basic <base64(user:pass)>" format
+			const prefix = "Basic "
+			if len(auth) < len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) {
+				w.Header().Set("WWW-Authenticate", `Basic realm="Proxy"`)
+				writeProxyError(w, http.StatusUnauthorized, "Invalid Authorization header format")
+				return
+			}
+
+			// Decode base64 credentials
+			decoded, err := base64.StdEncoding.DecodeString(auth[len(prefix):])
+			if err != nil {
+				w.Header().Set("WWW-Authenticate", `Basic realm="Proxy"`)
+				writeProxyError(w, http.StatusUnauthorized, "Invalid credentials encoding")
+				return
+			}
+
+			// Parse "user:pass" format
+			credentials := string(decoded)
+			colonIdx := strings.Index(credentials, ":")
+			if colonIdx < 0 {
+				w.Header().Set("WWW-Authenticate", `Basic realm="Proxy"`)
+				writeProxyError(w, http.StatusUnauthorized, "Invalid credentials format")
+				return
+			}
+
+			providedUser := credentials[:colonIdx]
+			providedPass := credentials[colonIdx+1:]
+
+			// DL-007: Constant-time comparison prevents timing attacks
+			userMatch := subtle.ConstantTimeCompare([]byte(providedUser), []byte(username)) == 1
+			passMatch := subtle.ConstantTimeCompare([]byte(providedPass), []byte(password)) == 1
+
+			if !userMatch || !passMatch {
+				// DL-007: Log auth failures with source IP for audit (R-002 mitigation)
+				clientIP := getClientIP(r)
+				logger.Printf("WARN: proxy auth failed from %s", clientIP)
+				w.Header().Set("WWW-Authenticate", `Basic realm="Proxy"`)
+				writeProxyError(w, http.StatusUnauthorized, "Invalid credentials")
+				return
+			}
+
+			next.ServeHTTP(w, r)
+		})
+	}
+}
+
+// writeProxyError writes a JSON error response.
+func writeProxyError(w http.ResponseWriter, status int, message string) {
+	w.Header().Set("Content-Type", "application/json")
+	w.WriteHeader(status)
+	fmt.Fprintf(w, `{"error":"%s"}`, message)
+}
+
+// getClientIP extracts client IP from request.
+func getClientIP(r *http.Request) string {
+	ip := r.RemoteAddr
+	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
+		if parts := strings.Split(forwarded, ","); len(parts) > 0 {
+			ip = strings.TrimSpace(parts[0])
+		}
+	} else if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
+		ip = realIP
+	}
+	return ip
+}
```

### Milestone 3: Forwarding: Request proxying with Bearer token injection

**Files**: pkg/proxy/handler.go, pkg/proxy/logging.go

**Acceptance Criteria**:

- Proxy forwards requests with Bearer token injection
- Incoming Authorization header stripped
- Upstream errors return 502 Bad Gateway
- Timeouts return 504 Gateway Timeout
- Access logged with method/path/IP/status/latency

**Tests**:

- TestProxyHandler
- TestProxyBearerInjection
- TestProxyErrorHandling
- TestProxyTimeout
- TestProxyAccessLogging

#### Code Intent

- **CI-M-003-001** `pkg/proxy/handler.go`: ProxyHandler creates http.Handler that forwards requests to API URL. Strips incoming Authorization header, injects Bearer token from config. Copies request method, path, headers (except Authorization), body. Returns upstream response to client. Returns 502 Bad Gateway on upstream connection error, 504 Gateway Timeout on timeout. Logs requests at INFO level (method, path, source IP, status code, latency). Logs forwarding errors at ERROR level. (refs: DL-003, DL-011, DL-013)
- **CI-M-003-002** `pkg/proxy/logging.go`: AccessLog middleware logs all requests at INFO level: method, path, source IP, response status code, latency. Extracts source IP from X-Forwarded-For (first hop) or X-Real-IP header, falls back to RemoteAddr. Uses structured logging format. (refs: DL-002, DL-007)

#### Code Changes

**CC-M-003-001** (pkg/proxy/handler.go) - implements CI-M-003-001

```diff
--- a/pkg/proxy/handler.go
+++ b/pkg/proxy/handler.go
@@ -0,0 +1,101 @@
+package proxy
+
+import (
+	"context"
+	"fmt"
+	"io"
+	"log"
+	"net/http"
+	"time"
+)
+
+// hopByHopHeaders are headers that should not be forwarded to upstream.
+// These are removed per RFC 2616 Section 13.5.1
+var hopByHopHeaders = []string{
+	"Connection",
+	"Keep-Alive",
+	"Proxy-Authenticate",
+	"Proxy-Authorization",
+	"Te",
+	"Trailers",
+	"Transfer-Encoding",
+	"Upgrade",
+}
+
+// ProxyHandler creates a handler that forwards requests to the upstream API.
+// DL-003: Proxy injects Bearer token when forwarding to API
+// DL-013: Returns 502 on upstream failure, 504 on timeout
+func ProxyHandler(apiURL, bearerToken string, client *http.Client, logger *log.Logger) func(http.Handler) http.Handler {
+	return func(next http.Handler) http.Handler {
+		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+			// Skip proxying for health endpoint (handled directly)
+			if r.URL.Path == "/health" {
+				next.ServeHTTP(w, r)
+				return
+			}
+
+			start := time.Now()
+			// Create upstream request
+			upstreamURL := apiURL + r.URL.Path
+			if r.URL.RawQuery != "" {
+				upstreamURL += "?" + r.URL.RawQuery
+			}
+
+			upstreamReq, err := http.NewRequestWithContext(r.Context(), r.Method, upstreamURL, r.Body)
+			if err != nil {
+				logger.Printf("ERROR: failed to create upstream request: %v", err)
+				writeProxyError(w, http.StatusInternalServerError, "Failed to create upstream request")
+				return
+			}
+
+			// Copy headers from original request
+			for key, values := range r.Header {
+				// DL-003: Strip incoming Authorization header, inject Bearer token
+				if key == "Authorization" {
+					continue
+				}
+				// Skip hop-by-hop headers
+				skipHeader := false
+				for _, hopHeader := range hopByHopHeaders {
+					if http.CanonicalHeaderKey(key) == hopHeader {
+						skipHeader = true
+						break
+					}
+				}
+				if !skipHeader {
+					for _, value := range values {
+						upstreamReq.Header.Add(key, value)
+					}
+				}
+			}
+
+			// DL-003: Inject Bearer token for API authentication
+			upstreamReq.Header.Set("Authorization", "Bearer "+bearerToken)
+
+			// Forward request to upstream
+			resp, err := client.Do(upstreamReq)
+			if err != nil {
+				if ctxErr := r.Context().Err(); ctxErr == context.DeadlineExceeded {
+					// DL-013: Timeout returns 504 Gateway Timeout
+					logger.Printf("ERROR: upstream timeout: %v", err)
+					writeProxyError(w, http.StatusGatewayTimeout, "Upstream timeout")
+					return
+				}
+				// DL-013: Connection error returns 502 Bad Gateway
+				logger.Printf("ERROR: upstream connection failed: %v", err)
+				writeProxyError(w, http.StatusBadGateway, "Upstream connection failed")
+				return
+			}
+			defer resp.Body.Close()
+
+			// Copy response headers
+			for key, values := range resp.Header {
+				for _, value := range values {
+					w.Header().Add(key, value)
+				}
+			}
+
+			// Copy response status and body
+			w.WriteHeader(resp.StatusCode)
+			if _, copyErr := io.Copy(w, resp.Body); copyErr != nil {
+				logger.Printf("ERROR: response body copy failed: %v", copyErr)
+			}
+
+			logger.Printf("INFO: %s %s -> %d (%v)", r.Method, r.URL.Path, resp.StatusCode, time.Since(start))
+		})
+	}
+}
```

**CC-M-003-002** (pkg/proxy/logging.go) - implements CI-M-003-002

```diff
--- a/pkg/proxy/logging.go
+++ b/pkg/proxy/logging.go
@@ -0,0 +1,41 @@
+package proxy
+
+import (
+	"log"
+	"net/http"
+	"strings"
+	"time"
+)
+
+// AccessLog middleware logs all requests at INFO level.
+// DL-007: Extracts source IP from X-Forwarded-For (first hop) or X-Real-IP header
+func AccessLog(next http.Handler, logger *log.Logger) http.Handler {
+	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		start := time.Now()
+
+		// Wrap response writer to capture status code
+		wrapped := &loggingResponseWriter{ResponseWriter: w, status: http.StatusOK}
+
+		next.ServeHTTP(wrapped, r)
+
+		clientIP := getClientIP(r)
+
+		duration := time.Since(start)
+		logger.Printf("INFO: %s %s from %s - %d (%v)",
+			r.Method,
+			r.URL.Path,
+			clientIP,
+			wrapped.status,
+			duration,
+		)
+	})
+}
+
+// loggingResponseWriter wraps http.ResponseWriter to capture status code.
+type loggingResponseWriter struct {
+	http.ResponseWriter
+	status int
+}
+
+func (rw *loggingResponseWriter) WriteHeader(status int) {
+	rw.status = status
+	rw.ResponseWriter.WriteHeader(status)
+}
```

### Milestone 4: Integration: main.go optional proxy startup and documentation

**Files**: main.go, README.md, .env.example, Containerfile

**Acceptance Criteria**:

- PROXY_ENABLED controls proxy startup
- Proxy starts in goroutine alongside API
- Graceful shutdown stops proxy cleanly
- README documents proxy configuration and security tradeoffs
- .env.example includes proxy vars

**Tests**:

- TestProxyIntegration
- TestProxyGracefulShutdown
- TestProxyEnabledFalse

#### Code Intent

- **CI-M-004-001** `main.go`: Add proxy initialization: read PROXY_ENABLED env var, create proxy.Server if enabled, start in goroutine alongside API server. Add proxy to graceful shutdown sequence. Wire PROXY_BEARER_TOKEN from API_BEARER_TOKEN. Fatal error if PROXY_ENABLED=true but credentials missing/invalid. (refs: DL-009, DL-015)
- **CI-M-004-002** `README.md`: Add Proxy section: describe purpose (browser-native auth), configuration env vars (PROXY_ENABLED, PROXY_PORT, PROXY_API_URL, PROXY_USER, PROXY_PASSWORD), usage examples, security considerations (Basic Auth vs Bearer tradeoffs). (refs: DL-002, DL-006)
- **CI-M-004-003** `.env.example`: Add proxy configuration examples: PROXY_ENABLED=true, PROXY_PORT=8080, PROXY_API_URL=http://localhost:3001, PROXY_USER=admin, PROXY_PASSWORD=your-secure-password (refs: DL-004, DL-005, DL-006)
- **CI-M-004-004** `Containerfile`: Add EXPOSE 8080 for proxy port. Document proxy port in comments. (refs: DL-004)

#### Code Changes

**CC-M-004-001** (.env.example) - implements CI-M-004-003

```diff
--- a/.env.example
+++ b/.env.example
@@ -15,3 +15,10 @@
 # CORS: comma-separated list of allowed origins (empty = no CORS)
 API_CORS_ORIGINS=https://example.com
 API_TRUSTED_PROXY_IPS=
+
+# Proxy configuration (optional)
+# PROXY_ENABLED=true
+# PROXY_PORT=8080
+# PROXY_API_URL=http://localhost:3001
+# PROXY_USER=admin
+# PROXY_PASSWORD=your-secure-password
```

**CC-M-004-002** (Containerfile) - implements CI-M-004-004

```diff
--- a/Containerfile
+++ b/Containerfile
@@ -32,6 +32,7 @@ USER 1001
 # Expose ports
 # 3001: Bot API server (optional, when API_ENABLED=true)
 EXPOSE 3001
+EXPOSE 8080

 # Set environment variables (replace at runtime)
 ENV DISCORD_TOKEN=""
```

**CC-M-004-003** (main.go) - implements CI-M-004-001

```diff
--- a/main.go
+++ b/main.go
@@ -22,6 +22,7 @@ import (
 	"github.com/bombom/absa-ac/api"
 	"github.com/bwmarrin/discordgo"
 	"net"
+	"github.com/bombom/absa-ac/pkg/proxy"
 )
```

**CC-M-004-004** (README.md) - implements CI-M-004-002

```diff
--- a/README.md
+++ b/README.md
@@ -252,6 +252,35 @@ curl -X POST \

 Response format and error handling details...

+## Proxy Server (Optional)
+
+The bot includes an optional reverse proxy server for browser-based API access. When enabled, the proxy accepts HTTP Basic Auth credentials and forwards authenticated requests to the API server.
+
+### Enabling the Proxy
+
+Set the following environment variables:
+
+```bash
+# Enable the proxy server
+PROXY_ENABLED=true
+
+# Proxy server port (default: 8080)
+PROXY_PORT=8080
+
+# HTTP Basic Auth credentials (required if PROXY_ENABLED=true)
+PROXY_USER=admin
+PROXY_PASSWORD=your-secure-password
+```
+
+### Usage
+
+With the proxy enabled, access the admin UI in your browser:
+
+```
+http://localhost:8080/admin/
+```
+
+Your browser will prompt for username/password via HTTP Basic Auth.
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PROXY_ENABLED` | false | Enable the reverse proxy |
| `PROXY_PORT` | 8080 | Port for proxy server |
| `PROXY_API_URL` | http://localhost:3001 | API server URL to proxy to |
| `PROXY_USER` | (required) | Basic Auth username |
| `PROXY_PASSWORD` | (required) | Basic Auth password (8+ chars) |

## Files to Create/Modify

| File | Action | Purpose |
|------|--------|---------|
| `pkg/proxy/config.go` | Create | Configuration struct and env loading |
| `pkg/proxy/server.go` | Create | HTTP server with graceful shutdown |
| `pkg/proxy/auth.go` | Create | Basic Auth middleware |
| `pkg/proxy/handler.go` | Create | Request forwarding with Bearer injection |
| `pkg/proxy/logging.go` | Create | Access logging middleware |
| `main.go` | Modify | Add optional proxy startup |
| `README.md` | Modify | Add proxy documentation section |
| `.env.example` | Modify | Add proxy env var examples |
| `Containerfile` | Modify | Add EXPOSE 8080 |
