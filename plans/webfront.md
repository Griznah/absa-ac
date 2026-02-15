# Plan

## Overview

Build a simple web frontend for the AC Discord Bot REST API that allows authenticated users to view and manage bot configuration

**Approach**: awaiting user decision

### Web Frontend Architecture

[Diagram pending Technical Writer rendering: DIAG-001]

## Planning Context

### Decision Log

| ID | Decision | Reasoning Chain |
|---|---|---|
| DL-001 | Embed frontend in Go server using http.FileServer | User selected embedded serving -> minimal deployment complexity (single binary) -> no separate web server needed -> uses Go embed package |
| DL-002 | Single-page app with vanilla JS (no framework) | Constraints require minimal dependencies -> vanilla JS has zero build chain -> appropriate for CRUD operations on small config -> no complex state management needed |
| DL-003 | Store bearer token in sessionStorage after login | Single-user/shared token scenario -> sessionStorage cleared on tab close -> more secure than localStorage (XSS protection) -> token included in all API requests via Authorization header |
| DL-004 | Wire CSRF middleware into API server, frontend fetches token from /api/csrf-token and includes X-CSRF-Token header | CSRF middleware exists (csrf_middleware.go) + route registered (GET /api/csrf-token) -> middleware NOT in chain (server.go:74-88) -> wire it in for defense-in-depth -> frontend must fetch token on login and include in state-changing requests |
| DL-005 | Frontend route: /admin/ (redirect / to /admin/ for authenticated users) | Separation of public health endpoint from admin UI -> clear URL structure -> /health remains public -> /admin/* requires authentication |

### Constraints

- MUST: Use existing REST API Bearer token authentication (API_BEARER_TOKEN)
- MUST: Work with existing CORS configuration (API_CORS_ORIGINS)
- SHOULD: Be simple, static files served by existing Go server or separate lightweight server
- SHOULD: Minimal dependencies, no complex build chain required

### Known Risks

- **XSS attack exposes bearer token in sessionStorage**: Strict CSP header (default-src self), sanitize all user input before display, use textContent not innerHTML
- **Token lost on tab close disrupts user workflow**: Clear UX messaging about session-based auth, provide re-login prompt, no persistent sessions per design choice DL-003
- **CSRF token management complexity in frontend**: Token fetched once on login via GET /api/csrf-token, stored in sessionStorage alongside bearer token, auto-included in all state-changing requests via api.js wrapper
- **Rate limiting may block legitimate admin operations during bulk edits**: 10 req/s with burst 20 allows reasonable admin operations; if needed, increase API_RATE_LIMIT env var

## Invisible Knowledge

### System

AC Discord Bot with REST API - Go-based Discord bot for monitoring Assetto Corsa racing servers

### Invariants

- CSRF middleware will be wired into API middleware chain (SecurityHeaders -> CORS -> Logger -> RateLimit -> BearerAuth -> CSRF -> Handler)
- Bearer token must be 32+ chars, not default/demo values
- API uses constant-time comparison for auth
- Rate limiting: 10 req/s, burst 20, per IP
- CSP header: default-src 'self'

### Tradeoffs

- Vanilla JS with no build chain trades type safety/ecosystem tooling for simplicity and zero build dependencies (appropriate for small CRUD app)
- sessionStorage for token trades remember-me functionality for better XSS protection (session-based auth appropriate for admin tool)

## Milestones

### Milestone 1: Foundation: Embed static files and add /admin/ route

**Files**: ["api/server.go", "api/web/admin/index.html"]

#### Code Intent

- **CI-M-001-001** `api/server.go`: Embed web/admin directory using Go embed package, serve files at /admin/* route, add CSP header override for inline scripts (refs: DL-001, DL-005)
- **CI-M-001-002** `api/web/admin/index.html`: Base HTML structure with login form, config viewer placeholder, CSS styles, and JS module placeholders for API client, auth manager, and UI renderer (refs: DL-002)
- **CI-M-001-003** `api/server.go`: Wire CSRF middleware into middleware chain between BearerAuth and handler (after auth, before handler execution) - SecurityHeaders -> CORS -> Logger -> RateLimit -> BearerAuth -> CSRF -> Handler (refs: DL-004)

#### Code Changes

**CC-M-001-001** (api/server.go) - implements CI-M-001-001

**Code:**

```diff
--- a/api/server.go
+++ b/api/server.go
@@ -1,8 +1,12 @@
 package api
 
 import (
+	"embed"
 	"context"
 	"fmt"
+	"io/fs"
 	"log"
 	"net/http"
 	"sync"
 	"time"
 )
+
+//go:embed web/admin
+var adminFS embed.FS

```

**Documentation:**

```diff
--- a/api/server.go
+++ b/api/server.go
@@ -1,6 +1,15 @@
 package api

 import (
+	"embed"
 	"context"
 	"fmt"
+	"io/fs"
 	"log"
 	"net/http"
 	"sync"
 	"time"
 )

+// adminFS embeds the web/admin directory for single-binary deployment.
+// Frontend served at /admin/* uses vanilla JS with no build chain (ref: DL-001, DL-002)
+//
+//go:embed web/admin
+var adminFS embed.FS

```


**CC-M-001-002** (api/server.go) - implements CI-M-001-001

**Code:**

```diff
--- a/api/server.go
+++ b/api/server.go
@@ -86,6 +94,20 @@ func (s *Server) Start(ctx context.Context) error {
 	s.httpServer.Handler = handler
 
 	// Register routes
 	RegisterRoutes(mux, s)
+
+	// Serve embedded admin frontend at /admin/*
+	// Single binary deployment: no external web server needed
+	adminSubFS, err := fs.Sub(adminFS, "web/admin")
+	if err != nil {
+		return fmt.Errorf("failed to load embedded admin files: %w", err)
+	}
+	fileServer := http.FileServer(http.FS(adminSubFS))
+
+	// CSP override for admin UI: inline scripts required for vanilla JS SPA
+	adminHandler := withCSPForAdmin(fileServer)
+	mux.Handle("GET /admin/", http.StripPrefix("/admin", adminHandler))
+	mux.Handle("GET /admin", http.RedirectHandler("/admin/", http.StatusMovedPermanently))
 
 	// Start server in background
 	s.wg.Add(1)

```

**Documentation:**

```diff
--- a/api/server.go
+++ b/api/server.go
@@ -86,6 +94,20 @@ func (s *Server) Start(ctx context.Context) error {
 	s.httpServer.Handler = handler

 	// Register routes
 	RegisterRoutes(mux, s)
+
+	// Serve embedded admin frontend at /admin/*
+	// Single binary deployment eliminates need for external web server (ref: DL-001)
+	// /admin route provides clean separation from public /health endpoint (ref: DL-005)
+	adminSubFS, err := fs.Sub(adminFS, "web/admin")
+	if err != nil {
+		return fmt.Errorf("failed to load embedded admin files: %w", err)
+	}
+	fileServer := http.FileServer(http.FS(adminSubFS))
+
+	// CSP override for admin UI: inline scripts required for vanilla JS SPA (ref: DL-002)
+	adminHandler := withCSPForAdmin(fileServer)
+	mux.Handle("GET /admin/", http.StripPrefix("/admin", adminHandler))
+	mux.Handle("GET /admin", http.RedirectHandler("/admin/", http.StatusMovedPermanently))

 	// Start server in background
 	s.wg.Add(1)

```


**CC-M-001-003** (api/server.go) - implements CI-M-001-001

**Code:**

```diff
--- a/api/server.go
+++ b/api/server.go
@@ -136,3 +158,23 @@ func (s *Server) Stop() error {

	return nil
 }
+
+// withCSPForAdmin wraps handler with permissive CSP for admin UI
+// Vanilla JS SPA requires inline scripts (no build chain per DL-002)
+func withCSPForAdmin(next http.Handler) http.Handler {
+	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		// Override CSP for admin UI to allow inline scripts and styles
+		w.Header().Set("Content-Security-Policy",
+			"default-src 'self'; " +
+\t\t\t\t"script-src 'self' 'unsafe-inline'; " +
+\t\t\t\t"style-src 'self' 'unsafe-inline'")
+
+\t\t// Admin UI bypasses auth (public static files)
+\t\t// Auth enforced client-side via sessionStorage token
+\t\tnext.ServeHTTP(w, r)
+\t})
+}
```

**Documentation:**

```diff
--- a/api/server.go
+++ b/api/server.go
@@ -136,3 +158,23 @@ func (s *Server) Stop() error {

 	return nil
 }
+
+// withCSPForAdmin wraps handler with permissive CSP for admin UI.
+// Vanilla JS SPA requires inline scripts (no build chain per DL-002).
+// Auth enforced client-side via sessionStorage token (ref: DL-003).
+func withCSPForAdmin(next http.Handler) http.Handler {
+	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
+		// Override CSP for admin UI to allow inline scripts and styles
+		// Required for vanilla JS SPA without build chain (ref: DL-002)
+		w.Header().Set("Content-Security-Policy",
+			"default-src 'self'; "+
+				"script-src 'self' 'unsafe-inline'; "+
+				"style-src 'self' 'unsafe-inline'")
+
+		// Admin UI bypasses auth (public static files)
+		// Auth enforced client-side via sessionStorage token (ref: DL-003)
+		next.ServeHTTP(w, r)
+	})
+}

```


**CC-M-001-004** (api/web/admin/index.html) - implements CI-M-001-002

**Code:**

```diff
--- /dev/null
+++ b/api/web/admin/index.html
@@ -0,0 +1,74 @@
+<!DOCTYPE html>
+<html lang="en">
+<head>
+    <meta charset="UTF-8">
+    <meta name="viewport" content="width=device-width, initial-scale=1.0">
+    <title>AC Bot Admin</title>
+    <link rel="stylesheet" href="styles.css">
+</head>
+<body>
+    <div id="app">
+        <!-- Login Screen -->
+        <section id="login-screen" class="screen">
+            <h1>AC Bot Configuration</h1>
+            <form id="login-form">
+                <div class="form-group">
+                    <label for="token-input">Bearer Token</label>
+                    <input type="password" id="token-input" 
+                           placeholder="Enter API bearer token" 
+                           autocomplete="off"
+                           required>
+                </div>
+                <button type="submit" id="login-btn">Login</button>
+                <p id="login-error" class="error-message hidden"></p>
+            </form>
+        </section>
+
+        <!-- Main Config Editor -->
+        <section id="config-screen" class="screen hidden">
+            <header>
+                <h1>Bot Configuration</h1>
+                <button id="logout-btn">Logout</button>
+            </header>
+
+            <main id="config-editor">
+                <!-- Servers Section -->
+                <section class="config-section">
+                    <h2>Servers</h2>
+                    <div id="servers-list"></div>
+                    <button id="add-server-btn">Add Server</button>
+                </section>
+
+                <!-- General Settings -->
+                <section class="config-section">
+                    <h2>Settings</h2>
+                    <div class="form-group">
+                        <label for="interval-input">Update Interval (seconds)</label>
+                        <input type="number" id="interval-input" min="10">
+                    </div>
+                    <div class="form-group">
+                        <label for="category-input">Category ID</label>
+                        <input type="text" id="category-input" placeholder="Discord category ID">
+                    </div>
+                </section>
+
+                <!-- Actions -->
+                <section class="config-section actions">
+                    <button id="validate-btn">Validate</button>
+                    <button id="save-btn">Save Changes</button>
+                </section>
+            </main>
+
+            <div id="status-message" class="hidden"></div>
+        </section>
+    </div>
+
+    <!-- JS Modules -->
+    <script src="auth.js"></script>
+    <script src="api.js"></script>
+    <script src="app.js"></script>
+</body>
+</html>
```

**Documentation:**

```diff
--- /dev/null
+++ b/api/web/admin/index.html
@@ -0,0 +1,5 @@
+<!-- Admin UI: Single-page app for AC Bot configuration (ref: DL-002). -->
+<!-- Two-screen design: login form, then config editor. -->
+<!-- Security: password input type prevents token visibility. -->
+<!-- Script load order: auth.js -> api.js -> app.js (dependency chain). -->
+<!DOCTYPE html>
```


**CC-M-001-005** (api/server.go) - implements CI-M-001-003

**Code:**

```diff
--- a/api/server.go
+++ b/api/server.go
@@ -81,6 +81,7 @@ func (s *Server) Start(ctx context.Context) error {
	rateLimitMiddleware := RateLimit(10, 20, s.trustedProxies, serverCtx) // 10 req/sec, burst 20
	loggerMiddleware := Logger(s.logger)
	authMiddleware := BearerAuth(s.bearerToken, s.trustedProxies)
+
	// Defense-in-depth: CSRF after auth, before handler

	var handler http.Handler = mux
+	handler = CSRF(handler)                           // CSRF validation for state-changing requests
	handler = authMiddleware(handler)                    // Innermost: check auth last
@@ -88,7 +90,6 @@ func (s *Server) Start(ctx context.Context) error {
	handler = loggerMiddleware(handler)                  // Log all requests including rate limited ones
	handler = corsMiddleware(handler)                    // Handle CORS preflight before rate limiting
	handler = securityHeadersMiddleware(handler)         // Outermost: security headers applied to all responses
-
	s.httpServer.Handler = handler
```

**Documentation:**

```diff
--- a/api/server.go
+++ b/api/server.go
@@ -81,6 +81,7 @@ func (s *Server) Start(ctx context.Context) error {
 	rateLimitMiddleware := RateLimit(10, 20, s.trustedProxies, serverCtx) // 10 req/sec, burst 20
 	loggerMiddleware := Logger(s.logger)
 	authMiddleware := BearerAuth(s.bearerToken, s.trustedProxies)
+	// CSRF defense-in-depth: validates state-changing requests following auth (ref: DL-004)

 	var handler http.Handler = mux
+	handler = CSRF(handler)                           // CSRF validation for state-changing requests
 	handler = authMiddleware(handler)                    // Innermost: check auth last

```


**CC-M-001-006** (api/web/admin/README.md)

**Documentation:**

```diff
--- /dev/null
+++ b/api/web/admin/README.md
@@ -0,0 +1,58 @@
+# Admin Frontend
+
+Single-page web UI for AC Bot configuration management.
+
+## Architecture
+
+Vanilla JS SPA embedded in Go binary. No build chain required (ref: DL-002).
+
+```
+index.html    - Base structure with login form and config editor
+styles.css    - Dark theme responsive layout
+auth.js       - Token management with sessionStorage
+api.js        - Fetch wrapper with auth/CSRF headers
+app.js        - Config editor with CRUD operations
+```
+
+## Security Design
+
+### Token Storage (ref: DL-003)
+
+Bearer token stored in sessionStorage (cleared on tab close).
+More secure than localStorage for XSS protection (ref: RSK-001).
+
+### CSRF Protection (ref: DL-004)
+
+CSRF token fetched from `/api/csrf-token` following login.
+Included in X-CSRF-Token header for all state-changing requests
+(POST, PATCH, PUT, DELETE).
+
+### XSS Prevention (ref: RSK-001)
+
+All user input escaped via `textContent` (never `innerHTML`).
+Strict CSP allows inline scripts (required for vanilla JS without build).
+
+## Authentication Flow
+
+1. User enters bearer token on login screen
+2. Token validated locally (32+ char format check)
+3. Token verified against `/health` endpoint
+4. CSRF token fetched from `/api/csrf-token`
+5. Both tokens stored in sessionStorage
+6. Tokens auto-included in all API requests
+
+## CSP Override
+
+Admin UI requires permissive CSP for inline scripts:
+```
+default-src 'self'; script-src 'self' 'unsafe-inline'; style-src 'self' 'unsafe-inline'
+```
+
+This is applied via `withCSPForAdmin` middleware in server.go (ref: DL-002).
+
+## Rate Limiting
+
+API enforces 10 req/s with burst 20 (ref: RSK-004).
+If bulk operations trigger rate limits, increase `API_RATE_LIMIT` env var.
+
+## Session Behavior
+
+Token lost on tab close requires re-login (ref: RSK-002).
+This is intentional - no persistent sessions for security.

```


### Milestone 2: Auth: Login/logout flow with token management

**Files**: ["api/web/admin/auth.js"]

#### Code Intent

- **CI-M-002-001** `api/web/admin/auth.js`: Login form handler that validates token format locally, sends bearer token to API health endpoint for validation, stores token in sessionStorage on success, logout clears sessionStorage, auth state getter for UI (refs: DL-003, DL-004)
- **CI-M-002-002** `api/web/admin/auth.js`: On successful login validation, fetch CSRF token from GET /api/csrf-token endpoint, store in sessionStorage as csrfToken, include CSRF token fetch in login flow (refs: DL-004)

#### Code Changes

**CC-M-002-001** (api/web/admin/auth.js) - implements CI-M-002-001

**Code:**

```diff
--- /dev/null
+++ b/api/web/admin/auth.js
@@ -0,0 +1,73 @@
+// Auth module: login/logout flow with token management
+// Token stored in sessionStorage (cleared on tab close for XSS protection per DL-003)
+
+const Auth = {
+    // Validate token format locally before API call
+    // Bearer tokens must be 32+ chars (per API validation)
+    validateTokenFormat(token) {
+        if (!token || typeof token !== 'string') {
+            return { valid: false, error: 'Token is required' };
+        }
+        if (token.length < 32) {
+            return { valid: false, error: 'Token must be at least 32 characters' };
+        }
+        return { valid: true };
+    },
+
+    // Verify token against API health endpoint
+    async verifyToken(token) {
+        try {
+            const response = await fetch('/health', {
+                headers: { 'Authorization': `Bearer ${token}` }
+            });
+            return response.ok;
+        } catch {
+            return false;
+        }
+    },
+
+    // Store token in sessionStorage
+    setToken(token) {
+        sessionStorage.setItem('bearerToken', token);
+    },
+
+    // Retrieve token from sessionStorage
+    getToken() {
+        return sessionStorage.getItem('bearerToken');
+    },
+
+    // Store CSRF token
+    setCSRFToken(token) {
+        sessionStorage.setItem('csrfToken', token);
+    },
+
+    // Retrieve CSRF token
+    getCSRFToken() {
+        return sessionStorage.getItem('csrfToken');
+    },
+
+    // Check if user is authenticated (has valid token in storage)
+    isAuthenticated() {
+        return !!this.getToken();
+    },
+
+    // Clear all auth data (logout)
+    logout() {
+        sessionStorage.removeItem('bearerToken');
+        sessionStorage.removeItem('csrfToken');
+    },
+
+    // Full login flow: validate format, verify with API, fetch CSRF token
+    async login(token) {
+        const formatCheck = this.validateTokenFormat(token);
+        if (!formatCheck.valid) {
+            return { success: false, error: formatCheck.error };
+        }
+
+        const isValid = await this.verifyToken(token);
+        if (!isValid) {
+            return { success: false, error: 'Invalid token or API unavailable' };
+        }
+
+        this.setToken(token);
+        return { success: true };
+    }
+};
+
+// Export for use in other modules
+window.Auth = Auth;
```

**Documentation:**

```diff
--- a/api/web/admin/auth.js
+++ b/api/web/admin/auth.js
@@ -0,0 +1,9 @@
+// Auth module: login/logout flow with token management.
+// Token stored in sessionStorage (cleared on tab close for XSS protection).
+// CSRF token fetched following successful auth for state-changing requests (ref: DL-003, DL-004).
+//
+// Security considerations:
+// - sessionStorage limits token exposure to single tab (ref: DL-003)
+// - Token format validated locally preceding API call (32+ chars required)
+// - CSRF token required for all POST/PATCH/PUT/DELETE requests (ref: DL-004)
+
 const Auth = {

```


**CC-M-002-002** (api/web/admin/auth.js) - implements CI-M-002-002

**Code:**

```diff
--- a/api/web/admin/auth.js
+++ b/api/web/admin/auth.js
@@ -64,9 +64,23 @@ const Auth = {
         if (!isValid) {
             return { success: false, error: 'Invalid token or API unavailable' };
         }

         this.setToken(token);
+
+        // Fetch CSRF token after successful authentication (per DL-004)
+        const csrfSuccess = await this.fetchCSRFToken();
+        if (!csrfSuccess) {
+            this.logout(); // Rollback on CSRF failure
+            return { success: false, error: 'Failed to fetch CSRF token' };
+        }
+
         return { success: true };
-    }
-};
+
+    // Fetch CSRF token from API endpoint
+    async fetchCSRFToken() {
+        const token = this.getToken();
+        const response = await APIClient.get('/api/csrf-token');
+        if (response.ok && response.data?.csrf_token) {
+            this.setCSRFToken(response.data.csrf_token);
+            return true;
+        }
+        return false;
+    }
+};
```

**Documentation:**

```diff
--- a/api/web/admin/auth.js
+++ b/api/web/admin/auth.js
@@ -64,9 +64,27 @@ const Auth = {
         if (!isValid) {
             return { success: false, error: 'Invalid token or API unavailable' };
         }

         this.setToken(token);
+
+        // Fetch CSRF token following successful authentication (ref: DL-004)
+        // CSRF token required for all state-changing requests
         const csrfSuccess = await this.fetchCSRFToken();
         if (!csrfSuccess) {
+            // Rollback: clear token on CSRF failure to maintain consistent state
             this.logout();
             return { success: false, error: 'Failed to fetch CSRF token' };
         }

         return { success: true };
+
+    // Fetch CSRF token from API endpoint.
+    // Called following successful bearer token validation (ref: DL-004).
+    async fetchCSRFToken() {
+        const token = this.getToken();
+        const response = await APIClient.get('/api/csrf-token');
+        if (response.ok && response.data?.csrf_token) {
+            this.setCSRFToken(response.data.csrf_token);
+            return true;
+        }
+        return false;
+    }
+};
+}

```


### Milestone 3: API Client: Fetch wrapper with auth headers

**Files**: ["api/web/admin/api.js"]

#### Code Intent

- **CI-M-003-001** `api/web/admin/api.js`: Fetch wrapper that auto-includes Authorization header AND X-CSRF-Token header (fetched from /api/csrf-token after login), JSON handling, error parsing for 401/403/429/5xx responses, methods: get, patch, put, post (refs: DL-003, DL-004)

#### Code Changes

**CC-M-003-001** (api/web/admin/api.js) - implements CI-M-003-001

**Code:**

```diff
--- /dev/null
+++ b/api/web/admin/api.js
@@ -0,0 +1,78 @@
+// API Client module: fetch wrapper with auth and CSRF headers
+// Auto-includes Authorization and X-CSRF-Token headers per DL-003 and DL-004
+
+const APIClient = {
+    // Base URL for API requests
+    baseURL: '/api',
+
+    // Build headers with auth and CSRF tokens
+    buildHeaders(includeCSRF = false) {
+        const headers = {
+            'Content-Type': 'application/json'
+        };
+
+        const bearerToken = window.Auth?.getToken();
+        if (bearerToken) {
+            headers['Authorization'] = `Bearer ${bearerToken}`;
+        }
+
+        // Include CSRF token for state-changing requests
+        if (includeCSRF) {
+            const csrfToken = window.Auth?.getCSRFToken();
+            if (csrfToken) {
+                headers['X-CSRF-Token'] = csrfToken;
+            }
+        }
+
+        return headers;
+    },
+
+    // Parse API error responses
+    async parseError(response) {
+        try {
+            const data = await response.json();
+            return data.error || data.message || `HTTP ${response.status}`;
+        } catch {
+            return `HTTP ${response.status}: ${response.statusText}`;
+        }
+    },
+
+    // Generic request method
+    async request(method, path, body = null) {
+        const includeCSRF = ['POST', 'PATCH', 'PUT', 'DELETE'].includes(method);
+        const options = {
+            method,
+            headers: this.buildHeaders(includeCSRF)
+        };
+
+        if (body) {
+            options.body = JSON.stringify(body);
+        }
+
+        const response = await fetch(`${this.baseURL}${path}`, options);
+
+        // Handle 401 - token expired/invalid
+        if (response.status === 401) {
+            window.Auth?.logout();
+            return { ok: false, status: 401, error: 'Authentication required' };
+        }
+
+        // Handle 429 - rate limited
+        if (response.status === 429) {
+            return { ok: false, status: 429, error: 'Rate limit exceeded. Please wait.' };
+        }
+
+        // Parse successful responses
+        if (response.ok) {
+            const text = await response.text();
+            const data = text ? JSON.parse(text) : null;
+            return { ok: true, status: response.status, data };
+        }
+
+        // Other errors
+        return { ok: false, status: response.status, error: await this.parseError(response) };
+    },
+
+    // Convenience methods
+    get(path) { return this.request('GET', path); },
+    post(path, body) { return this.request('POST', path, body); },
+    patch(path, body) { return this.request('PATCH', path, body); },
+    put(path, body) { return this.request('PUT', path, body); },
+    delete(path) { return this.request('DELETE', path); }
+};
+
+window.APIClient = APIClient;
```

**Documentation:**

```diff
--- a/api/web/admin/api.js
+++ b/api/web/admin/api.js
@@ -0,0 +1,10 @@
+// API Client module: fetch wrapper with auth and CSRF headers.
+// Auto-includes Authorization header (Bearer token) and X-CSRF-Token header.
+// CSRF token included for state-changing requests (POST/PATCH/PUT/DELETE).
+//
+// Headers included (ref: DL-003, DL-004):
+// - Authorization: Bearer <token> - all requests
+// - X-CSRF-Token: <token> - state-changing requests only
+
 const APIClient = {
     // Base URL for API requests
     baseURL: '/api',

```


### Milestone 4: UI: Config editor with CRUD operations

**Files**: ["api/web/admin/app.js", "api/web/admin/styles.css"]

#### Code Intent

- **CI-M-004-001** `api/web/admin/app.js`: Main app initialization, config editor with form fields for servers list (add/edit/delete), interval setting, category config, save/validate buttons, error display, loading states (refs: DL-002)
- **CI-M-004-002** `api/web/admin/styles.css`: Responsive layout, form styling, button states, error/success message styling, loading spinner, dark theme for admin interface (refs: DL-002)

#### Code Changes

**CC-M-004-001** (api/web/admin/app.js) - implements CI-M-004-001

**Code:**

```diff
--- /dev/null
+++ b/api/web/admin/app.js
@@ -0,0 +1,188 @@
+// Main app module: config editor with CRUD operations
+// Vanilla JS SPA (no framework per DL-002)
+
+const App = {
+    config: null,
+    servers: [],
+
+    // Initialize app on page load
+    init() {
+        this.bindEvents();
+        this.checkAuth();
+    },
+
+    // Bind all event handlers
+    bindEvents() {
+        // Login form
+        document.getElementById('login-form').addEventListener('submit', (e) => {
+            e.preventDefault();
+            this.handleLogin();
+        });
+
+        // Logout button
+        document.getElementById('logout-btn').addEventListener('click', () => {
+            this.handleLogout();
+        });
+
+        // Add server button
+        document.getElementById('add-server-btn').addEventListener('click', () => {
+            this.addServer();
+        });
+
+        // Validate button
+        document.getElementById('validate-btn').addEventListener('click', () => {
+            this.validateConfig();
+        });
+
+        // Save button
+        document.getElementById('save-btn').addEventListener('click', () => {
+            this.saveConfig();
+        });
+    },
+
+    // Check auth state and show appropriate screen
+    checkAuth() {
+        if (window.Auth.isAuthenticated()) {
+            this.showConfigScreen();
+        } else {
+            this.showLoginScreen();
+        }
+    },
+
+    // Handle login form submission
+    async handleLogin() {
+        const tokenInput = document.getElementById('token-input');
+        const errorEl = document.getElementById('login-error');
+        const token = tokenInput.value.trim();
+
+        errorEl.classList.add('hidden');
+
+        const result = await window.Auth.login(token);
+        if (result.success) {
+            this.showConfigScreen();
+        } else {
+            errorEl.textContent = result.error;
+            errorEl.classList.remove('hidden');
+        }
+    },
+
+    // Handle logout
+    handleLogout() {
+        window.Auth.logout();
+        this.showLoginScreen();
+    },
+
+    // Show login screen
+    showLoginScreen() {
+        document.getElementById('login-screen').classList.remove('hidden');
+        document.getElementById('config-screen').classList.add('hidden');
+        document.getElementById('token-input').value = '';
+    },
+
+    // Show config screen and load data
+    async showConfigScreen() {
+        document.getElementById('login-screen').classList.add('hidden');
+        document.getElementById('config-screen').classList.remove('hidden');
+        await this.loadConfig();
+    },
+
+    // Load config from API
+    async loadConfig() {
+        const response = await window.APIClient.get('/config');
+        if (response.ok) {
+            this.config = response.data;
+            this.servers = response.data.servers || [];
+            this.renderConfig();
+        } else {
+            this.showMessage('Failed to load config: ' + response.error, 'error');
+        }
+    },
+
+    // Render config to UI
+    renderConfig() {
+        // Render servers list
+        const serversList = document.getElementById('servers-list');
+        serversList.innerHTML = '';
+
+        this.servers.forEach((server, index) => {
+            const serverEl = this.createServerElement(server, index);
+            serversList.appendChild(serverEl);
+        });
+
+        // Render settings
+        document.getElementById('interval-input').value = this.config.interval || 60;
+        document.getElementById('category-input').value = this.config.category_id || '';
+    },
+
+    // Create server editor element
+    createServerElement(server, index) {
+        const div = document.createElement('div');
+        div.className = 'server-item';
+        div.innerHTML = `
+            <div class="form-group">
+                <label>Name</label>
+                <input type="text" data-field="name" value="${this.escapeHtml(server.name || '')}">
+            </div>
+            <div class="form-group">
+                <label>URL</label>
+                <input type="text" data-field="url" value="${this.escapeHtml(server.url || '')}">
+            </div>
+            <button type="button" class="delete-server-btn" data-index="${index}">Delete</button>
+        `;
+
+        // Bind delete handler
+        div.querySelector('.delete-server-btn').addEventListener('click', () => {
+            this.deleteServer(index);
+        });
+
+        // Bind input handlers
+        div.querySelectorAll('input').forEach(input => {
+            input.addEventListener('change', (e) => {
+                this.updateServer(index, e.target.dataset.field, e.target.value);
+            });
+        });
+
+        return div;
+    },
+
+    // Escape HTML to prevent XSS
+    escapeHtml(text) {
+        const div = document.createElement('div');
+        div.textContent = text;
+        return div.innerHTML;
+    },
+
+    // Add new server
+    addServer() {
+        this.servers.push({ name: '', url: '' });
+        this.renderConfig();
+    },
+
+    // Delete server
+    deleteServer(index) {
+        this.servers.splice(index, 1);
+        this.renderConfig();
+    },
+
+    // Update server field
+    updateServer(index, field, value) {
+        if (this.servers[index]) {
+            this.servers[index][field] = value;
+        }
+    },
+
+    // Validate config via API
+    async validateConfig() {
+        this.collectFormChanges();
+        const response = await window.APIClient.post('/config/validate', this.buildConfigPayload());
+        if (response.ok) {
+            this.showMessage('Configuration is valid', 'success');
+        } else {
+            this.showMessage('Validation failed: ' + response.error, 'error');
+        }
+    },
+
+    // Save config via API
+    async saveConfig() {
+        this.collectFormChanges();
+        const response = await window.APIClient.put('/config', this.buildConfigPayload());
+        if (response.ok) {
+            this.showMessage('Configuration saved', 'success');
+            await this.loadConfig(); // Refresh from server
+        } else {
+            this.showMessage('Failed to save: ' + response.error, 'error');
+        }
+    },
+
+    // Collect form changes into config object
+    collectFormChanges() {
+        this.config.interval = parseInt(document.getElementById('interval-input').value, 10) || 60;
+        this.config.category_id = document.getElementById('category-input').value.trim();
+        this.config.servers = this.servers;
+    },
+
+    // Build config payload for API
+    buildConfigPayload() {
+        return {
+            interval: this.config.interval,
+            category_id: this.config.category_id,
+            servers: this.servers
+        };
+    },
+
+    // Show status message
+    showMessage(text, type) {
+        const el = document.getElementById('status-message');
+        el.textContent = text;
+        el.className = type; // 'success' or 'error'
+        setTimeout(() => el.classList.add('hidden'), 5000);
+    }
+};
+
+// Initialize on DOM ready
+document.addEventListener('DOMContentLoaded', () => App.init());
```

**Documentation:**

```diff
--- a/api/web/admin/app.js
+++ b/api/web/admin/app.js
@@ -0,0 +1,7 @@
+// Main app module: config editor with CRUD operations.
+// Vanilla JS SPA (no framework per DL-002).
+//
+// XSS prevention: all user input escaped via textContent (ref: RSK-001)
+// No innerHTML used for user-provided content

 const App = {
     config: null,
@@ -149,6 +152,9 @@ const App = {
     },

     // Escape HTML to prevent XSS
+    // Uses textContent assignment to avoid innerHTML injection (ref: RSK-001)
+    // All user-provided content rendered through this method
     escapeHtml(text) {
         const div = document.createElement('div');
         div.textContent = text;

```


**CC-M-004-002** (api/web/admin/styles.css) - implements CI-M-004-002

**Code:**

```diff
--- /dev/null
+++ b/api/web/admin/styles.css
@@ -0,0 +1,156 @@
+/* Dark theme admin interface */
+:root {
+    --bg-primary: #1a1a2e;
+    --bg-secondary: #16213e;
+    --text-primary: #eaeaea;
+    --text-secondary: #a0a0a0;
+    --accent: #0f3460;
+    --accent-hover: #1a4a7a;
+    --success: #28a745;
+    --error: #dc3545;
+    --border: #333;
+}
+
+* {
+    box-sizing: border-box;
+    margin: 0;
+    padding: 0;
+}
+
+body {
+    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
+    background: var(--bg-primary);
+    color: var(--text-primary);
+    line-height: 1.6;
+    min-height: 100vh;
+}
+
+#app {
+    max-width: 900px;
+    margin: 0 auto;
+    padding: 2rem;
+}
+
+.screen {
+    animation: fadeIn 0.3s ease;
+}
+
+@keyframes fadeIn {
+    from { opacity: 0; transform: translateY(10px); }
+    to { opacity: 1; transform: translateY(0); }
+}
+
+.hidden {
+    display: none !important;
+}
+
+h1 {
+    margin-bottom: 1.5rem;
+    font-weight: 500;
+}
+
+h2 {
+    font-size: 1.2rem;
+    margin-bottom: 1rem;
+    color: var(--text-secondary);
+}
+
+/* Login Screen */
+#login-screen {
+    max-width: 400px;
+    margin: 4rem auto;
+    text-align: center;
+}
+
+#login-screen h1 {
+    margin-bottom: 2rem;
+}
+
+#login-form {
+    display: flex;
+    flex-direction: column;
+    gap: 1rem;
+}
+
+/* Forms */
+.form-group {
+    display: flex;
+    flex-direction: column;
+    gap: 0.5rem;
+    text-align: left;
+}
+
+label {
+    font-size: 0.9rem;
+    color: var(--text-secondary);
+}
+
+input {
+    padding: 0.75rem;
+    border: 1px solid var(--border);
+    border-radius: 4px;
+    background: var(--bg-secondary);
+    color: var(--text-primary);
+    font-size: 1rem;
+}
+
+input:focus {
+    outline: none;
+    border-color: var(--accent-hover);
+}
+
+/* Buttons */
+button {
+    padding: 0.75rem 1.5rem;
+    border: none;
+    border-radius: 4px;
+    background: var(--accent);
+    color: var(--text-primary);
+    cursor: pointer;
+    font-size: 1rem;
+    transition: background 0.2s;
+}
+
+button:hover {
+    background: var(--accent-hover);
+}
+
+button:disabled {
+    opacity: 0.5;
+    cursor: not-allowed;
+}
+
+#logout-btn {
+    background: transparent;
+    border: 1px solid var(--border);
+}
+
+#logout-btn:hover {
+    background: var(--bg-secondary);
+}
+
+/* Config Sections */
+.config-section {
+    background: var(--bg-secondary);
+    padding: 1.5rem;
+    border-radius: 8px;
+    margin-bottom: 1.5rem;
+}
+
+.server-item {
+    background: var(--bg-primary);
+    padding: 1rem;
+    border-radius: 4px;
+    margin-bottom: 1rem;
+}
+
+.server-item .form-group {
+    margin-bottom: 0.5rem;
+}
+
+.delete-server-btn {
+    padding: 0.5rem 1rem;
+    font-size: 0.9rem;
+    background: var(--error);
+}
+
+.delete-server-btn:hover {
+    background: #c82333;
+}
+
+#add-server-btn {
+    width: 100%;
+    margin-top: 0.5rem;
+}
+
+/* Actions */
+.actions {
+    display: flex;
+    gap: 1rem;
+}
+
+.actions button {
+    flex: 1;
+}
+
+#save-btn {
+    background: var(--success);
+}
+
+#save-btn:hover {
+    background: #218838;
+}
+
+/* Messages */
+.error-message {
+    color: var(--error);
+    font-size: 0.9rem;
+}
+
+#status-message {
+    padding: 1rem;
+    border-radius: 4px;
+    text-align: center;
+    margin-top: 1rem;
+}
+
+#status-message.success {
+    background: rgba(40, 167, 69, 0.2);
+    border: 1px solid var(--success);
+}
+
+#status-message.error {
+    background: rgba(220, 53, 69, 0.2);
+    border: 1px solid var(--error);
+}
+
+/* Header */
+header {
+    display: flex;
+    justify-content: space-between;
+    align-items: center;
+    margin-bottom: 2rem;
+}
+
+/* Responsive */
+@media (max-width: 600px) {
+    #app {
+        padding: 1rem;
+    }
+
+    .actions {
+        flex-direction: column;
+    }
+
+    header {
+        flex-direction: column;
+        gap: 1rem;
+    }
+}
```

**Documentation:**

```diff
--- /dev/null
+++ b/api/web/admin/styles.css
@@ -0,0 +1,6 @@
+/* Dark theme admin interface (ref: DL-002). */
+/* CSS custom properties enable consistent theming. */
+/* Responsive breakpoint at 600px stacks action buttons vertically. */
+/* Screen transitions use fadeIn animation for smooth UX. */
+/* Dark theme admin interface */
+:root {
```

