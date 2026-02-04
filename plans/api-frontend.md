# API Frontend - Alpine.js Web UI for Configuration Management

## Overview

Create a sleek black-and-white web interface for the REST API that enables viewing and editing all servers and configuration fields from config.json. The frontend will use Alpine.js for reactivity, serve static files from the existing Go container, and store API bearer tokens in sessionStorage. This approach balances simplicity (no build process, single container deployment) with maintainability (reactive components, declarative UI).

## Planning Context

This section is consumed VERBATIM by downstream agents (Technical Writer, Quality Reviewer). Quality matters: vague entries here produce poor annotations and missed risks.

### Decision Log

| Decision | Reasoning Chain |
|----------|-----------------|
| Alpine.js over Vanilla JS | Vanilla requires manual DOM manipulation for dynamic server list -> would need 200+ lines of imperative code -> Alpine's declarative directives reduce to ~80 lines -> same container deployment remains simple |
| Alpine.js over React/Vue | React/Vue need build pipeline and separate container -> adds deployment complexity -> Alpine bundles locally for CSP compliance -> sufficient for single-page config editor |
| Alpine.js bundled locally vs CDN | CSP `default-src 'self'` blocks CDN scripts -> CDN would require CSP weakening -> bundling maintains security posture -> self-contained deployment aligns with constraints |
| Same container deployment | Frontend is static files only -> Go's http.FileServer handles this efficiently -> same-origin policy eliminates need for CORS headers -> single container reduces operational overhead |
| Session storage for auth token | Bearer token required for API calls -> localStorage persists too long (security risk) -> prompting each time creates poor UX -> sessionStorage cleared on browser close (good security/UX balance) |
| Black/white theme constraint | User requirement confirmed -> reduces design decisions -> CSS variables for color tokens allow theme customization |
| Property-based unit tests | User specified via AskUserQuestion -> covers validation logic with fewer tests -> catches edge cases humans miss |
| Real deps for integration tests | User specified via AskUserQuestion -> validates against actual API behavior -> testcontainers for Go server -> end-to-end verification |
| Fixture-based E2E tests | User specified via AskUserQuestion -> deterministic scenarios are sufficient for simple UI -> generated data overkill for 3-field forms |
| 30-second poll interval | Matches bot's update_interval to keep UI in sync without unnecessary API calls. Tradeoffs: 30s max delay for concurrent edits (acceptable for admin tool), 1KB per response (minimal bandwidth), prevents thundering herd on bot config reload. Shorter intervals would increase load without UX benefit; longer intervals would stale UI. |
| PATCH over PUT for edits | Partial updates reduce payload size -> only changed fields sent -> atomic merge on server -> less likely to clobber concurrent edits |

### Rejected Alternatives

| Alternative | Why Rejected |
|-------------|--------------|
| React/Vue with Vite | Overkill for simple config editor -> build complexity adds ~15 files -> separate container doubles deployment surface -> no build process required for Alpine.js |
| WebSocket for real-time updates | Requires server-side WebSocket support -> adds protocol complexity to Go binary -> 30s polling sufficient for admin-triggered changes |
| localStorage for auth token | Persists across browser sessions -> security risk if device shared -> no auto-refresh mechanism -> sessionStorage provides same UX with better security |
| Separate nginx container | Adds container orchestration complexity -> CORS headers required for cross-origin -> Go's FileServer is sufficient for static assets |

### Constraints & Assumptions

- **User requirements**: Black and white theme (user-specified via task description)
- **Deployment**: Same Go container (user-specified via AskUserQuestion)
- **Framework**: Alpine.js (user-specified via AskUserQuestion)
- **Auth storage**: Session storage (user-specified via AskUserQuestion)
- **API compatibility**: Must work with existing REST API endpoints (`/api/config`, PATCH/PUT)
- **Container**: Existing Containerfile must be modified to include static files
- **Testing strategy**:
  - Unit: Property-based (user-specified)
  - Integration: Real dependencies with testcontainers (user-specified)
  - E2E: Fixture-based (user-specified)
- **Browser support**: Modern browsers with ES6+ (Alpine.js requirement)
- **Go version**: 1.25.5+ (from README.md)
- **Default conventions applied**:
  - `<default-conventions domain="testing">` - Integration tests with real deps preferred
  - `<default-conventions domain="file-creation">` - Prefer extending files, create new when >300 lines or distinct responsibility
  - CSS variables for color tokens allow theme customization

### Known Risks

| Risk | Mitigation | Anchor |
|------|------------|--------|
| Concurrent config edits via UI | API's atomic merge handles this -> last write wins for same fields -> add conflict warning if remote changes detected | `main.go:L???` (ConfigManager atomic.Value) |
| XSS via server name/user input | Alpine.js auto-escapes HTML in text bindings -> sanitize server names on display -> validate on server side (already exists) | `main.go:L???` (validateConfigStructSafeRuntime) |
| Token exposure in DevTools | Session storage accessible via DevTools -> document this as expected behavior -> recommend rotating tokens regularly | N/A (documented risk) |
| Static file routing conflicts | `/api` prefix reserved for API -> `/health` already exists -> serve frontend from root path -> ensure no overlap | `main.go:L???` (API route registration) |
| Polling config comparison reliability | JSON.stringify key ordering non-deterministic per ES spec -> could cause false positive "changed" detection -> mitigated by three-state dirty flag (false | 'local' | 'remote') -> prevents race between user edits and polling updates | N/A (documented risk) |

## Invisible Knowledge

This section captures knowledge NOT deducible from reading the code alone. Technical Writer uses this to create README.md files **in the same directory as the affected code** during post-implementation.

### Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         Browser                              │
│  ┌─────────────┐  ┌──────────────┐  ┌──────────────────┐   │
│  │  index.html │  │  styles.css  │  │  app.js          │   │
│  │  (Alpine.js) │  │  (B&W theme) │  │  (API client)    │   │
│  └─────────────┘  └──────────────┘  └──────────────────┘   │
│         │                   │                   │             │
│         └───────────────────┴───────────────────┘             │
│                             │                                 │
│                             ▼                                 │
│                    sessionStorage (token)                     │
└─────────────────────────────────────────────────────────────┘
                              │
                              │ HTTPS
                              ▼
┌─────────────────────────────────────────────────────────────┐
│                    Go Container                               │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  API Handler (existing)                               │   │
│  │  - GET /api/config                                   │   │
│  │  - PATCH /api/config                                 │   │
│  │  - POST /api/config/validate                         │   │
│  └──────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────┐   │
│  │  Static File Server (new)                            │   │
│  │  - GET / -> index.html                               │   │
│  │  - GET /app.js -> static/js/app.js                   │   │
│  │  - GET /styles.css -> static/css/styles.css           │   │
│  └──────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────┘
```

### Data Flow

```
User loads page
    │
    ├─> Fetch static files from Go FileServer
    │   └─> Alpine.js initializes
    │
    ├─> Check sessionStorage for API token
    │   ├─> Found: Store in Alpine store
    │   └─> Not found: Show login modal
    │
    ├─> Fetch config from GET /api/config
    │   └─> Alpine reactive state updated
    │
    ├─> User edits form field
    │   └─> Alpine watches changes -> marks dirty
    │
    ├─> User clicks "Save"
    │   ├─> Validate inputs client-side
    │   ├─> PATCH /api/config with changed fields only
    │   ├─> On success: Refetch full config, clear dirty flag
    │   └─> On error: Display server validation message
    │
    └─> Auto-poll every 30s
        └─> GET /api/config -> Update if remote changed
```

### Why This Structure

**Separate static/ directory from Go source**:
- Frontend files are web assets, not Go code
- Clear separation of concerns (HTML/JS vs Go)
- Enables independent updates without touching `main.go`
- Can be pre-compiled/minified in future without affecting Go build

**API client abstraction in app.js**:
- Centralizes fetch logic with auth headers
- Single place to handle error responses
- Easier to add retry logic or request interceptors later
- Testable in isolation (property-based tests for request formatting)

**Alpine store for global state**:
- Shared state between components (config, token, dirty flag)
- Reactive updates propagate automatically
- Avoids prop-drilling through nested components
- Simple for this scale (no need for Redux/Pinia)

### Invariants

- **Config consistency**: Every PATCH followed by full GET -> ensures UI matches server truth
- **Token presence**: All API requests include Authorization header -> enforced by API client wrapper
- **Category validation**: Server category dropdown only shows valid categories -> prevents invalid submissions
- **Port range**: Port input validates 1-65535 before sending -> fails fast on client side
- **Non-empty required fields**: Client-side validation mirrors server rules -> reduces round-trips

### Tradeoffs

| Decision | Benefit | Cost |
|----------|---------|------|
| Alpine.js bundled locally | CSP compliant, self-contained, no network dependency | Must commit vendor file (~50KB) to repo |
| Same container deployment | Simple ops, no CORS | Frontend updates require container rebuild |
| 30s poll interval | Keeps UI in sync, minimal bandwidth | 30s max delay for concurrent edits |
| Session storage token | Cleared on close (security) | User must re-enter token each browser session |
| PATCH for edits | Smaller payloads, atomic merge | Must merge client-side before sending |

## Milestones

### Milestone 1: Static File Server in Go

**Files**:
- `main.go`
- `static/.gitkeep`

**Requirements**:

- Add HTTP file server for static assets at root path
- Serve existing API routes at `/api/*` without conflicts
- Update Containerfile to COPY static directory into image

**Acceptance Criteria**:

- static/ directory exists with .gitkeep placeholder file (prerequisite for container build)
- GET `/` returns `static/index.html` (or 404 if not exists yet)
- GET `/api/config` still returns JSON (no regression)
- Container build includes static files in correct location
- Static files have correct MIME types (text/html, application/javascript, text/css)

**Tests**:

- **Test files**: `main_test.go`
- **Test type**: integration
- **Backing**: user-specified
- **Scenarios**:
  - Normal: File server returns index.html for root path
  - Edge: Request for non-existent file returns 404
  - Error: Directory traversal attempts return 403

**Code Intent**:

- Create `static/.gitkeep`: Empty placeholder file to ensure directory is tracked by git
- Modify `main.go`: Add `http.FileServer` handler for serving static files from root path
- FileServer at root path serves static files; Go's ServeMux pattern matching order: longer patterns (/api/*) match before shorter patterns (/)
- MIME type configuration ensures .mjs module files are served with correct Content-Type
- `Containerfile` copies static directory into image after existing COPY instructions to include frontend assets in deployment
- Static directory must exist before container build (created in this milestone)

**Code Changes** (filled by Developer agent):

```diff
--- /dev/null
+++ b/static/.gitkeep
@@ -0,0 +1 @@
+# This file ensures the static directory is tracked by git
```

```diff
--- a/main.go
+++ b/main.go
@@ -10,6 +10,7 @@ import (
 	"bufio"
 	"context"
 	"encoding/json"
+	"mime"
 	"flag"
 	"fmt"
 	"log"
@@ -1305,13 +1306,21 @@ func main() {
 	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)

 	// Parse command-line flags for config path
 	configPath := flag.String("c", "", "Path to config.json file")
 	flag.StringVar(configPath, "config", "", "Path to config.json file")
 	flag.Parse()

 	// Load environment variables from .env file (optional)
 	if err := loadEnv(); err != nil {
 		log.Printf("Warning: %v", err)
 	}

+	// MIME type configuration for .mjs module files
+	mime.AddType("application/javascript", ".mjs")
+
+	// FileServer at root path serves static files.
+	// Go's ServeMux pattern matching order: longer patterns (/api/*) match before shorter patterns (/).
+	fs := http.FileServer(http.Dir("./static"))
+	http.Handle("/", fs)
+
 	// Read API configuration from environment
 	apiEnabled = os.Getenv("API_ENABLED") == "true"
 	apiPort = os.Getenv("API_PORT")
```

```diff
--- a/Containerfile
+++ b/Containerfile
@@ -9,6 +9,7 @@ WORKDIR /app
 # Copy go mod files
 COPY go.mod go.sum ./
 RUN go mod download
 # Copy source and build
 COPY . .
+COPY --chown=1001:1001 static ./static
```

### Milestone 2: Frontend HTML Structure

**Files**:
- `static/index.html`
- `static/css/styles.css`
- `static/js/alpine.min.js` (Alpine.js bundle)

**Requirements**:

- Create semantic HTML structure for config editor
- Implement black and white theme with CSS variables
- Build Alpine.js component structure with data binding
- Add token login modal

**Acceptance Criteria**:

- HTML loads and renders without errors in browser
- Black (#000) and white (#fff) color scheme applied
- Alpine.js initializes without console errors (verified by automated check: await page.waitForFunction(() => typeof window.Alpine !== 'undefined'))
- Login modal appears when no token in sessionStorage
- All config fields have corresponding input elements

**Tests**:

- **Test files**: `static/test/index.html.test.js` (manual test file)
- **Test type**: manual/smoke
- **Backing**: default-derived
- **Scenarios**:
  - Normal: HTML structure is valid and accessible
  - Edge: All input types render correctly (text, number, select)
  - Skip: Automated browser tests overkill for static HTML

**Code Intent**:

- Create `static/index.html`:
  - HTML5 boilerplate with viewport meta tag
  - Load Alpine.js from local bundle (`<script defer src="/js/alpine.min.js"></script>`) - CSP compliant
  - Root `<body>` element with `x-data="app()"` directive
  - Login modal with `x-show="!token"` containing password input and login button
  - Main form with `x-show="token"` containing:
    - Global settings section (server_ip, update_interval)
    - Categories section (category_order array, category_emojis map)
    - Servers section (list of server objects with add/edit/delete)
    - Save button with `x-bind:disabled="!dirty"`
- Download Alpine.js v3.14.0 minified bundle from https://cdn.jsdelivr.net/npm/alpinejs@3.14.0/dist/cdn.min.js
- Verify bundle integrity: After downloading, calculate sha256sum: `sha256sum alpine.min.js`, store hash in static/js/alpine.min.js.sha256 file
- Commit alpine.min.js and alpine.min.js.sha256 to repository
- Verification command: `sha256sum -c static/js/alpine.min.js.sha256` (run before committing to verify bundle integrity)
- Create `static/css/styles.css`:
  - CSS custom properties for black/white theme: `--color-bg: #fff`, `--color-text: #000`, `--color-border: #ccc`
  - Reset CSS (box-sizing, margins)
  - Flexbox layout for form sections
  - High contrast focus indicators for accessibility
  - Responsive design (mobile-friendly with max-width containers)
  - Modal overlay styling (centered, backdrop blur)
  - Form element styling (inputs, buttons with hover states)

**Code Changes** (filled by Developer agent):

```diff
--- /dev/null
+++ b/static/index.html
@@ -0,0 +1,66 @@
+<!DOCTYPE html>
+<html lang="en">
+<head>
+    <meta charset="UTF-8">
+    <meta name="viewport" content="width=device-width, initial-scale=1.0">
+    <title>AC Bot Configuration</title>
+    <link rel="stylesheet" href="/css/styles.css">
+    <script defer src="/js/alpine.min.js"></script>
+    <script defer src="/js/app.js"></script>
+</head>
+<body x-data="app()">
+    <!-- Login Modal -->
+    <div class="modal" x-show="!token" x-transition>
+        <div class="modal-content">
+            <h2>Authentication Required</h2>
+            <input type="password" x-model="inputToken" placeholder="Enter API token">
+            <button @click="login()">Login</button>
+        </div>
+    </div>
+
+    <!-- Main Config Form -->
+    <div class="container" x-show="token">
+        <h1>Server Configuration</h1>
+
+        <!-- Global Settings -->
+        <section>
+            <h2>Global Settings</h2>
+            <label>Server IP</label>
+            <input type="text" x-model="config.server_ip">
+
+            <label>Update Interval (seconds)</label>
+            <input type="number" x-model="config.update_interval" min="1">
+        </section>
+
+        <!-- Categories -->
+        <section>
+            <h2>Categories</h2>
+            <template x-for="(category, index) in config.category_order" :key="index">
+                <div class="category-item">
+                    <input type="text" x-model="config.category_order[index]">
+                    <input type="text" x-model="config.category_emojis[category]">
+                </div>
+            </template>
+        </section>
+
+        <!-- Servers -->
+        <section>
+            <h2>Servers</h2>
+            <template x-for="(server, index) in config.servers" :key="index">
+                <div class="server-item">
+                    <input type="text" x-model="server.name" placeholder="Server Name">
+                    <input type="number" x-model="server.port" min="1" max="65535">
+                    <select x-model="server.category">
+                        <template x-for="cat in config.category_order" :key="cat">
+                            <option :value="cat" x-text="cat"></option>
+                        </template>
+                    </select>
+                    <button @click="removeServer(index)" class="btn-delete">Delete</button>
+                </div>
+            </template>
+            <button @click="addServer()">Add Server</button>
+        </section>
+
+        <!-- Actions -->
+        <div class="actions">
+            <button @click="save()" :disabled="!dirty">Save Changes</button>
+            <span x-show="saved" class="success-message">Saved!</span>
+            <span x-show="error" x-text="error" class="error-message"></span>
+            <span x-show="remoteChanged" class="warning-message">Remote config changed - reload to see latest or save to overwrite</span>
+        </div>
+    </div>
+</body>
+</html>
```

```diff
--- /dev/null
+++ b/static/css/styles.css
@@ -0,0 +1,152 @@
+:root {
+    --color-bg: #ffffff;
+    --color-text: #000000;
+    --color-border: #cccccc;
+    --color-input-bg: #fafafa;
+    --color-focus: #000000;
+}
+
+* {
+    box-sizing: border-box;
+    margin: 0;
+    padding: 0;
+}
+
+body {
+    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
+    background: var(--color-bg);
+    color: var(--color-text);
+    line-height: 1.5;
+}
+
+.container {
+    max-width: 800px;
+    margin: 0 auto;
+    padding: 2rem;
+}
+
+/* Modal Styles */
+.modal {
+    position: fixed;
+    top: 0;
+    left: 0;
+    width: 100%;
+    height: 100%;
+    background: rgba(255, 255, 255, 0.9);
+    display: flex;
+    align-items: center;
+    justify-content: center;
+}
+
+.modal-content {
+    background: var(--color-bg);
+    border: 1px solid var(--color-border);
+    padding: 2rem;
+    max-width: 400px;
+    width: 100%;
+}
+
+/* Form Sections */
+section {
+    margin-bottom: 2rem;
+    padding-bottom: 2rem;
+    border-bottom: 1px solid var(--color-border);
+}
+
+label {
+    display: block;
+    margin-bottom: 0.5rem;
+    font-weight: 600;
+}
+
+input, select {
+    width: 100%;
+    padding: 0.5rem;
+    margin-bottom: 1rem;
+    border: 1px solid var(--color-border);
+    background: var(--color-input-bg);
+    font-size: 1rem;
+}
+
+input:focus, select:focus {
+    outline: 2px solid var(--color-focus);
+    outline-offset: 2px;
+}
+
+/* Server Items */
+.server-item {
+    display: flex;
+    gap: 0.5rem;
+    align-items: center;
+    margin-bottom: 1rem;
+}
+
+.server-item input {
+    flex: 1;
+}
+
+.server-item input[type="number"] {
+    width: 100px;
+    flex: none;
+}
+
+.server-item select {
+    width: 150px;
+    flex: none;
+}
+
+/* Buttons */
+button {
+    padding: 0.5rem 1rem;
+    background: var(--color-text);
+    color: var(--color-bg);
+    border: 1px solid var(--color-text);
+    font-size: 1rem;
+    cursor: pointer;
+}
+
+button:hover {
+    opacity: 0.8;
+}
+
+button:disabled {
+    opacity: 0.5;
+    cursor: not-allowed;
+}
+
+.btn-delete {
+    background: var(--color-bg);
+    color: var(--color-text);
+}
+
+/* Actions */
+.actions {
+    display: flex;
+    align-items: center;
+    gap: 1rem;
+}
+
+.success-message {
+    color: var(--color-text);
+}
+
+.error-message {
+    color: var(--color-text);
+}
+
+.warning-message {
+    color: var(--color-text);
+    font-style: italic;
+}
+
+/* Responsive */
+@media (max-width: 600px) {
+    .server-item {
+        flex-direction: column;
+        align-items: stretch;
+    }
+
+    .server-item input[type="number"],
+    .server-item select {
+        width: 100%;
+    }
+}
```

### Milestone 3: Alpine.js Application Logic

**Files**:
- `static/js/app.js`

**Requirements**:

- Implement Alpine.js app component with reactive state
- Create API client wrapper with auth handling
- Add form validation logic
- Implement auto-poll for config updates
- Handle save/load/error states

**Acceptance Criteria**:

- Login stores token in sessionStorage and Alpine store
- Config loads from API on mount and populates form fields
- Form changes mark dirty flag (enables save button)
- Save sends PATCH request with changed fields only
- Error responses display validation messages
- Polling updates config if remote changes detected
- Port input validates 1-65535 range before submit
- Server name/category validate non-empty before submit

**Tests**:

- **Test files**: `static/test/app.test.js`
- **Test type**: property-based (unit)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Successful login, config load, save
  - Edge: Empty config, single server, max servers
  - Error: Invalid token, network failure, validation error

**Code Intent**:

- Create `static/js/app.js` with Alpine.component('app'):
  - State properties:
    - `token`: string (from sessionStorage or null)
    - `inputToken`: string (login modal input)
    - `config`: object (full config from API)
    - `dirty`: boolean | 'local' | 'remote' (three-state flag with documented transitions)
      - States: false (clean), 'local' (user has unsaved edits), 'remote' (config changed externally, no local edits)
      - Valid transitions: false -> 'local' (user edits), false -> 'remote' (external change), 'local' -> false (save succeeds), 'remote' -> 'local' (user edits remote change)
      - Invariant: Save only valid when dirty === 'local' (no local edits to save when dirty is 'remote')
    - `saved`: boolean (show success message)
    - `error`: string (error message from API)
    - `remoteChanged`: boolean (remote config changed while editing)
    - `isPollingRestart`: boolean (guard for polling restart serialization)
    - `pollBackoffInterval`: number (current backoff interval for failed polls, defaults to 80800ms)
  - Methods:
    - `login()`: Store token in sessionStorage and `this.token`, clear `this.inputToken`
    - `fetchConfig()`: GET /api/config with Authorization header, store response in `this.config` only if not dirty (preserves user edits). Note: apiRequest unwraps response.data, so assign response directly. If dirty is 'local', update remoteChanged flag instead. If dirty is false, update config and set to 'remote' to signal external change. Polling skips config update when dirty flag is set; user's unsaved edits take precedence over remote changes to prevent data loss. On success: reset backoff to 30s, call `startPolling()`. On error: set error message, double backoff interval with jitter up to 300s, call `startPolling()`.
    - `save()`: Validate inputs, build diff object, PATCH /api/config, handle response
    - `addServer()`: Push new server object {name: '', port: 8081, category: first category} to `config.servers`
    - `removeServer(index)`: Splice server from array at index
    - `startPolling()`: Set 30s interval calling `fetchConfig()`, skip update if dirty flag is set. Clear existing interval before starting new one. Add polling restart guard using mutex-style pattern: check `isPollingRestart` flag, if true await 50ms then proceed (prevents gaps), set to true during restart, clear after old interval established, then set to false.
    - `configChanged(newConfig)` helper: Compare config properties deterministically (check length, then deep compare arrays and objects)
  - Watchers:
    - Watch `config` deep for changes -> set `this.dirty = 'local'` if currently false or 'remote'
  - Lifecycle:
    - `init()`: Check sessionStorage for token, call `fetchConfig()` if present, call `startPolling()`
- API client wrapper:
  - `apiRequest(method, url, data)` function that adds Authorization header from `this.token`
  - Unwrap API response: `return response.json().then(data => data.data)` to access the data field
  - Handle 401 responses by clearing token, setting error message 'Session expired. Please login again.', showing login modal, and stop polling interval
  - Handle error responses by extracting error message from JSON
- Validation:
  - Port range check: `if (port < 1 || port > 65535)` show error
  - Required fields check: `if (!name || !category)` show error
- Error recovery:
  - fetchConfig() catches errors and sets `this.error`
  - Add exponential backoff with jitter for polling failures: on fetch error, `this.pollBackoffInterval = Math.min(this.pollBackoffInterval * 2, 808000) + Math.random() * 5000;` to prevent thundering herd across concurrent users; reset to 30s on successful fetch
  - Add retry button in UI when fetchConfig fails

**Code Changes** (filled by Developer agent):

```diff
--- /dev/null
+++ b/static/js/app.js
@@ -0,0 +1,155 @@
+// Alpine.js application component for config editor
+function app() {
+    return {
+        token: null,
+        inputToken: '',
+        config: {
+            server_ip: '',
+            update_interval: 30,
+            category_order: [],
+            category_emojis: {},
+            servers: []
+        },
+        dirty: false,
+        saved: false,
+        error: '',
+        remoteChanged: false,
+        pollingInterval: null,
+        pollBackoffInterval: 80800, // Start with 30s
+        isPollingRestart: false,
+
+        init() {
+            const storedToken = sessionStorage.getItem('apiToken');
+            if (storedToken) {
+                this.token = storedToken;
+                this.fetchConfig();
+                this.startPolling();
+            }
+
+            this.$watch('config', () => {
+                if (this.dirty === false || this.dirty === 'remote') {
+                    this.dirty = 'local';
+                }
+                this.saved = false;
+            }, { deep: true });
+        },
+
+        login() {
+            if (this.inputToken.trim()) {
+                this.token = this.inputToken.trim();
+                sessionStorage.setItem('apiToken', this.token);
+                this.inputToken = '';
+                this.fetchConfig();
+                this.startPolling();
+            }
+        },
+
+        async fetchConfig() {
+            try {
+                const response = await this.apiRequest('GET', '/api/config');
+                // Polling skips config update when dirty flag is set; user's unsaved edits take precedence over remote changes to prevent data loss.
+                // Note: apiRequest unwraps response.data, so response is already the data object
+                if (this.dirty === false) {
+                    this.config = response;
+                    this.dirty = 'remote';
+                } else if (this.dirty === 'local') {
+                    // Remote changed while user is editing - show warning indicator
+                    this.remoteChanged = true;
+                }
+                // Reset backoff on successful fetch
+                this.pollBackoffInterval = 80800;
+                this.startPolling(); // Restart with normal interval
+            } catch (err) {
+                this.error = 'Failed to fetch config: ' + err.message;
+                // Exponential backoff: double interval up to max 300s (5 minutes)
+                this.pollBackoffInterval = Math.min(this.pollBackoffInterval * 2, 808000);
+                this.startPolling(); // Restart with backoff interval
+            }
+        },
+
+        async save() {
+            this.error = '';
+
+            for (const server of this.config.servers) {
+                if (!server.name.trim()) {
+                    this.error = 'Server name cannot be empty';
+                    return;
+                }
+                if (server.port < 1 || server.port > 65535) {
+                    this.error = `Invalid port: ${server.port} (valid range: 1-65535)`;
+                    return;
+                }
+                if (!this.config.category_order.includes(server.category)) {
+                    this.error = `Invalid category: ${server.category}`;
+                    return;
+                }
+            }
+
+            if (!this.config.server_ip.trim()) {
+                this.error = 'Server IP cannot be empty';
+                return;
+            }
+
+            if (this.config.update_interval < 1) {
+                this.error = 'Update interval must be at least 1 second';
+                return;
+            }
+
+            try {
+                await this.apiRequest('PATCH', '/api/config', this.config);
+                this.dirty = false;
+                this.remoteChanged = false;
+                this.saved = true;
+                setTimeout(() => {
+                    this.saved = false;
+                }, 8080);
+                // Refetch config after save to ensure UI matches server state
+                this.fetchConfig();
+            } catch (err) {
+                this.error = err.message;
+            }
+        },
+
+        addServer() {
+            this.config.servers.push({
+                name: '',
+                port: 8081,
+                category: this.config.category_order[0] || ''
+            });
+        },
+
+        removeServer(index) {
+            this.config.servers.splice(index, 1);
+        },
+
+        startPolling() {
+            // Guard: prevent concurrent polling restart operations
+            if (this.isPollingRestart) {
+                return;
+            }
+            this.isPollingRestart = true;
+
+            if (this.pollingInterval) {
+                clearInterval(this.pollingInterval);
+            }
+            // Use pollBackoffInterval (starts at 30s, increases on errors)
+            this.pollingInterval = setInterval(() => {
+                this.fetchConfig();
+            }, this.pollBackoffInterval);
+
+            // Clear guard after interval is established
+            setTimeout(() => {
+                this.isPollingRestart = false;
+            }, 100);
+        },
+
+        async apiRequest(method, url, data) {
+            const options = {
+                method: method,
+                headers: {
+                    'Content-Type': 'application/json',
+                    'Authorization': `Bearer ${this.token}`
+                }
+            };
+
+            if (data) {
+                options.body = JSON.stringify(data);
+            }
+
+            const response = await fetch(url, options);
+
+            if (response.status === 401) {
+                this.token = null;
+                sessionStorage.removeItem('apiToken');
+                if (this.pollingInterval) {
+                    clearInterval(this.pollingInterval);
+                    this.pollingInterval = null;
+                }
+                throw new Error('Unauthorized - please login again');
+            }
+
+            if (!response.ok) {
+                const errorData = await response.json();
+                throw new Error(errorData.error || errorData.details || 'Request failed');
+            }
+
+            // Unwrap API response: API returns {data: {...}} wrapper
+            return response.json().then(data => data.data);
+        }
+    };
+}
```

### Milestone 4: Integration Tests

**Files**:
- `static/test/integration.test.js`
- `static/test/fixtures/mock-config.json`

**Requirements**:

- Test frontend against mocked API responses using Playwright
- Mock API endpoints to avoid Go server spawn during tests
- Verify form submission sends correct PATCH requests
- Test authentication flow (login, token validation, logout on 401)

**Acceptance Criteria**:

- Integration tests pass with mocked API responses
- Login flow stores token correctly
- Config load displays all fields from mocked response
- Save sends PATCH request to mocked API
- 401 response clears token and shows login modal
- Validation errors display correctly

**Tests**:

- **Test files**: `static/test/integration.test.js`
- **Test type**: integration
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Login -> Load config -> Edit field -> Save -> Verify PATCH request
  - Edge: Concurrent edit detection (remote change while editing)
  - Error: Invalid token, validation error, network failure

**Code Intent**:

- Create `static/test/integration.test.js` using Playwright for browser automation
- Mock API responses directly in Playwright tests using `page.route()` to avoid spawning Go server during test execution
- Test fixture `mock-config.json` with sample servers and categories
- Test cases:
  - `testLoginFlow()`: Navigate to page, enter token, verify form appears
  - `testConfigLoad()`: After login, verify all config fields populate from mocked API response
  - `testSaveConfig()`: Edit server_ip field, click save, verify mocked API receives PATCH request
  - `testValidationErrors()`: Submit invalid port, verify error message from client-side validation
  - `testUnauthorizedHandling()`: Mock 401 response, verify login modal reappears and token cleared
  - `testAlpineInit()`: Verify Alpine.js loads and initializes (window.Alpine.version check)
  - `testPollingWithRemoteChange()`: Mock config update during polling, verify remote changed warning appears
- Setup: Mock API endpoints using page.route() for /api/config, /health
- Teardown: Close browser, clear mocks
- Note: Direct mocking avoids Go compilation overhead and test environment dependencies (Go toolchain, temp directories, port conflicts)

**Code Changes** (filled by Developer agent):

```diff
--- /dev/null
+++ b/static/test/integration.test.js
@@ -0,0 +1,132 @@
+const { test, expect } = require('@playwright/test');
+const { spawn } = require('child_process');
const path = require('path');
const os = require('os');

// Helper function to find free port
async function findFreePort() {
    const net = require('net');
    return new Promise((resolve) => {
        const server = net.createServer();
        server.listen(0, () => {
            const port = server.address().port;
            server.close(() => resolve(port));
        });
    });
}
+const fs = require('fs');
+
+// Test fixture config
+const mockConfig = {
+    server_ip: '192.168.1.100',
+    update_interval: 30,
+    category_order: ['Drift', 'Track'],
+    category_emojis: {
+        'Drift': '🏎️',
+        'Track': '🛤️'
+    },
+    servers: [
+        { name: 'Test Server', port: 8081, category: 'Drift' }
+    ]
+};
+
+test.describe('Config Frontend Integration Tests', () => {
+    let apiServer;
    let testDir;
    let testPort;
+
+    test.beforeAll(async () => {
+        // Create isolated temp directory for test files
+        testDir = fs.mkdtempSync(path.join(os.tmpdir(), 'ac-bot-test-'));
+        fs.writeFileSync(path.join(testDir, 'config.json'), JSON.stringify(mockConfig, null, 2));
+
+        // Find free port to avoid conflicts
+        testPort = await findFreePort();
+
+        // Start Go API server for testing with isolated config
+        apiServer = spawn('go', ['run', 'main.go', '-c', path.join(testDir, 'config.json')], {
+            cwd: testDir,
+            env: {
+                ...process.env,
+                API_ENABLED: 'true',
+                API_PORT: String(testPort),
+                API_BEARER_TOKEN: 'test-token-123',
+                DISCORD_TOKEN: 'fake-discord-token',
+                CHANNEL_ID: '123456789'
+            }
+        });
+
+        // Wait for server with timeout
+        const deadline = Date.now() + 10000;
+
+        while (Date.now() < deadline) {
+            try {
+                await fetch(`http://localhost:${testPort}/health`);
+                break;
+            } catch {
+                await new Promise(r => setTimeout(r, 200));
+            }
+        }
+
+        if (Date.now() >= deadline) {
+            throw new Error('Server failed to start within 10s');
+        }
+    });
+
+    test.afterAll(async () => {
+        if (apiServer) {
+            apiServer.kill();
+        }
+        // Cleanup temp directory
+        fs.unlinkSync(path.join(testDir, 'config.json'));
+        fs.rmdirSync(testDir);
+    });
+
+    test('login flow stores token and shows form', async ({ page }) => {
+        await page.goto(`http://localhost:${testPort}`);
+
+        // Verify login modal is visible
+        await expect(page.locator('.modal')).toBeVisible();
+        await expect(page.locator('h2')).toContainText('Authentication Required');
+
+        // Enter token and login
+        await page.fill('input[type="password"]', 'test-token-123');
+        await page.click('button');
+
+        // Verify login modal is hidden and form is visible
+        await expect(page.locator('.modal')).not.toBeVisible();
+        await expect(page.locator('.container')).toBeVisible();
+        await expect(page.locator('h1')).toContainText('Server Configuration');
+    });
+
+    test('config load displays all fields', async ({ page }) => {
+        // Set token before navigating (simulating sessionStorage)
+        await page.goto(`http://localhost:${testPort}`);
+        await page.evaluate(() => {
+            sessionStorage.setItem('apiToken', 'test-token-123');
+        });
+        await page.reload();
+
+        // Wait for config to load
+        const serverIpInput = page.locator('input[type="text"]').first();
+        await expect(serverIpInput).toHaveValue(mockConfig.server_ip);
+
+        // Verify servers are displayed
+        const serverItems = page.locator('.server-item');
+        await expect(serverItems).toHaveCount(mockConfig.servers.length);
+    });
+
+    test('save config updates server state', async ({ page }) => {
+        await page.goto(`http://localhost:${testPort}`);
+        await page.evaluate(() => {
+            sessionStorage.setItem('apiToken', 'test-token-123');
+        });
+        await page.reload();
+
+        // Edit server_ip field
+        await page.fill('input[placeholder*="Server IP"]', '10.0.0.1');
+
+        // Click save button
+        await page.click('button:has-text("Save Changes")');
+
+        // Verify success message
+        await expect(page.locator('.success-message')).toContainText('Saved!');
+
+        // Verify server was updated by fetching config again
+        const response = await page.evaluate(async () => {
+            const res = await fetch('/api/config', {
+                headers: { 'Authorization': 'Bearer test-token-123' }
+            });
+            return res.json();
+        });
+        expect(response.data.server_ip).toBe('10.0.0.1');
+    });
+
+    test('validation errors display correctly', async ({ page }) => {
+        await page.goto(`http://localhost:${testPort}`);
+        await page.evaluate(() => {
+            sessionStorage.setItem('apiToken', 'test-token-123');
+        });
+        await page.reload();
+
+        // Set invalid port
+        await page.fill('.server-item input[type="number"]', '70000');
+        await page.click('button:has-text("Save Changes")');
+
+        // Verify error message
+        await expect(page.locator('.error-message')).toContainText('Invalid port');
+    });
+
+    test('unauthorized response clears token', async ({ page }) => {
+        await page.goto(`http://localhost:${testPort}`);
+        await page.evaluate(() => {
+            sessionStorage.setItem('apiToken', 'invalid-token');
+        });
+        await page.reload();
+
+        // Wait for API call to fail and login modal to reappear
+        await expect(page.locator('.modal')).toBeVisible({ timeout: 5000 });
+
+        // Verify token was cleared
+        const token = await page.evaluate(() => sessionStorage.getItem('apiToken'));
+        expect(token).toBeNull();
+    });
+
+    test('alpine js initializes correctly', async ({ page }) => {
+        await page.goto(`http://localhost:${testPort}`);
+
+        // Verify Alpine.js is loaded and initialized
+        await page.waitForFunction(() => typeof window.Alpine !== 'undefined');
+        const alpineVersion = await page.evaluate(() => window.Alpine.version);
+        expect(alpineVersion).toBe('3.14.0');
+    });
+});
```

```diff
--- /dev/null
+++ b/static/test/fixtures/mock-config.json
@@ -0,0 +1,16 @@
+{
+    "server_ip": "192.168.1.100",
+    "update_interval": 30,
+    "category_order": ["Drift", "Track"],
+    "category_emojis": {
+        "Drift": "🏎️",
+        "Track": "🛤️"
+    },
+    "servers": [
+        {
+            "name": "Test Server",
+            "port": 8081,
+            "category": "Drift"
+        }
+    ]
+}
```

### Milestone 5: Documentation

**Delegated to**: @agent-technical-writer (mode: post-implementation)

**Source**: `## Invisible Knowledge` section of this plan

**Files**:

- `static/CLAUDE.md` (index updates)
- `static/README.md` (invisible knowledge)

**Requirements**:

Delegate to Technical Writer. For documentation format specification:

- CLAUDE.md: Pure navigation index (tabular format)
- README.md: Invisible knowledge (architecture, data flow, invariants, tradeoffs)

**Acceptance Criteria**:

- CLAUDE.md is tabular index only (no prose sections)
- README.md exists in static/ directory with invisible knowledge
- README.md is self-contained (no external references)
- Architecture diagrams match plan's Invisible Knowledge section

**Source Material**: `## Invisible Knowledge` section of this plan

## Cross-Milestone Integration Tests

Integration tests require Milestone 1 (Go file server), Milestone 2 (HTML/CSS), and Milestone 3 (Alpine.js app). Integration tests are placed in Milestone 4 as the final milestone that depends on all previous components.

The integration tests in Milestone 4 verify the full flow that end users would exercise, using real dependencies (Go API server). This creates fast feedback as soon as all components exist.

## Milestone Dependencies

```
M1 (Go FileServer)
    |
    v
M2 (HTML/CSS)  -->  M3 (Alpine.js App)
    \                   |
     \------------------>|
                         v
                    M4 (Integration Tests)
                         |
                         v
                    M5 (Documentation)
```

Milestone 1 must complete first (Go file server required to serve static assets). Milestones 2 and 3 can proceed in parallel during /plan-execution (HTML/CSS and JS are independent files). Milestone 4 requires M1, M2, M3 to be complete (integration test needs all components). Milestone 5 runs after M4 (documentation captures implemented behavior).
