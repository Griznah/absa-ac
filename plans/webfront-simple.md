# Simple Static Web Frontend with Basic Auth

## Overview

Implement a minimal static web frontend that provides a browser-based interface for the existing REST API. The frontend will use vanilla HTML/CSS/JavaScript (no framework dependencies) and implement HTTP Basic Authentication for access control. This approach maintains simplicity, reduces attack surface, and aligns with the project's "keep it simple" philosophy.

The static files will be served by a lightweight HTTP server written in Go that integrates with the existing API infrastructure, reusing security patterns like rate limiting and trusted proxy handling.

## Planning Context

### Decision Log

| Decision | Reasoning Chain |
|----------|-----------------|
| Static files over SPA framework | No framework dependencies reduces bundle size, complexity, and attack surface -> Simple HTML/CSS/JS is sufficient for CRUD config interface -> Static files can be served directly without build pipeline -> Faster iteration and easier maintenance |
| Basic HTTP Auth over OAuth/session tokens | Browser's native Basic Auth popup requires no UI implementation -> Zero server-side session state (no database, no cookies) -> Simplest auth mechanism for single-admin or small-team scenarios -> Can be upgraded to OAuth later if needed without API changes |
| Separate Go HTTP server over embedding in main bot | Enables independent deployment (frontend can run on different host/port) -> Allows serving static files without restarting Discord bot -> Cleaner separation of concerns (bot logic vs web UI) -> Can be scaled/restarted independently from bot |
| Same-origin deployment (inline JS, no bundler) | Eliminates CORS complexity for API calls from UI -> No build step required for development -> Simpler deployment (just copy HTML files) -> Project's simplicity philosophy favors no-build approach |
| Reuse API middleware (rate limiting, proxy handling) | Existing security patterns already battle-tested -> Consistent security posture across API and web UI -> Reduces code duplication and maintenance burden -> Same trusted proxy validation applies to both interfaces |

### Rejected Alternatives

| Alternative | Why Rejected |
|-------------|--------------|
| React/Vue SPA | Adds build pipeline complexity, larger bundle size, and unnecessary dependencies for simple CRUD interface. Project values simplicity over developer ergonomics for this use case. |
| OAuth2/session-based auth | Requires server-side session storage, CSRF protection, and more complex UI. Overkill for small admin team use case. |
| nginx/nginx-based static serving | Adds second container/language (Go bot + nginx) and requires managing separate process. Implementing static file serving in Go is trivial with `http.FileServer`. |
| Embedding static files in Go binary | Complicates development workflow (requires `go:embed` rebuild on file changes) and separates source files from their editable location. Serving from filesystem allows live editing. |

### Constraints & Assumptions

**Technical:**
- Existing REST API runs on port 3001 (configurable via `API_PORT`)
- API uses Bearer token authentication (`API_BEARER_TOKEN`)
- Project uses Go 1.25+ (current: golang:1.25.5-alpine in Containerfile)
- Static files must be served from `/app/webfront/` directory (container deployment)
- HTTP Basic Auth is acceptable for small team access (<10 users)
- Browser native Basic Auth dialog will be used (no custom login UI)

**Organizational:**
- Frontend must be deployable without restarting Discord bot
- Admin team is small (1-5 users typical)
- No dedicated frontend developer on team
- Preference for zero-build-step development

**Dependencies:**
- Go standard library `net/http` for HTTP server
- Go standard library `embed` optional (not used for filesystem serving)
- Existing `api` package for middleware reuse potential

**Default conventions:**
- No npm/node.js dependencies
- Self-documenting code over comments
- Conventional commits for git

### Known Risks

| Risk | Mitigation | Anchor |
|------|-----------|--------|
| Basic Auth credentials sent with each request | Use HTTPS in production (TLS termination at reverse proxy) | main.go:1403 `API_PORT` default assumes proxy provides TLS |
| Basic Auth cannot be easily revoked without restart | Credentials stored in environment variables, restart required to change | Accepted: small team, infrequent credential changes |
| Static file serving exposes directory structure | Serve through `http.FileServer` with stripped prefix, disable directory listings | `http.FileServer` shows directory listings if no index.html - mitigate with empty index.html in nested dirs |
| XSS through user input reflected in UI | All config values displayed as textContent (not innerHTML), JSON encoded | Implementation requirement in Milestone 2 |
| CSRF not applicable with Basic Auth | Basic Auth includes token with every request, browser manages auth header | N/A - Basic Auth is inherently CSRF-resistant |

## Invisible Knowledge

### Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                      Browser Client                            │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │  Static Files (served from webfront/)                   │  │
│  │  ┌──────────┐  ┌──────────┐  ┌──────────────────┐     │  │
│  │  │ index.html│  │ style.css │  │ app.js            │     │  │
│  │  └──────────┘  └──────────┘  └──────────────────┘     │  │
│  └─────────────────────────────────────────────────────────┘  │
│                          │                                    │
│                          │ HTTP Basic Auth                   │
│                          ▼                                    │
│  ┌─────────────────────────────────────────────────────────┐  │
│  │  WebFront Server (Go)                                  │  │
│  │  ┌──────────────────────────────────────────────────┐  │  │
│  │  │ File Server (http.FileServer)                   │  │  │
│  │  │ Basic Auth Middleware                          │  │  │
│  │  │ Optional: Rate Limiter (reused from api/)      │  │  │
│  │  └──────────────────────────────────────────────────┘  │  │
│  └─────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
                           │
                           │ API calls (Bearer token from UI)
                           ▼
┌─────────────────────────────────────────────────────────────────┐
│                    Existing REST API                           │
│  ┌──────────────────────────────────────────────────────────┐  │
│  │ GET /api/config, PATCH /api/config, etc.               │  │
│  │ Bearer Auth middleware                                  │  │
│  └──────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────┘
```

### Data Flow

```
User Access Browser
    │
    ▼
Request: GET /
    │
    ├─> WebFront Server
    │       │
    │       ├─> Check Basic Auth header
    │       │       │
    │       │       ├─> Missing/Invalid → 401 WWW-Authenticate
    │       │       │
    │       │       └─> Valid → Serve index.html
    │       │
    │       └─> Return index.html, style.css, app.js
    │
    ▼
Browser renders UI
    │
    ├─> app.js: Fetch config from API
    │       │
    │       └─> Authorization: Bearer <token-from-UI-input>
    │
    ├─> API Server validates Bearer token
    │
    └─> Returns JSON config
            │
            ▼
        app.js renders config display
            │
            ▼
        User edits config fields
            │
            ▼
        User clicks "Save"
            │
            ▼
        app.js: PATCH /api/config with Bearer token
            │
            ▼
        API Server updates config.json
            │
            ▼
        ConfigManager reloads (mtime change)
            │
            ▼
        app.js: Refresh display from GET /api/config
```

### Why This Structure

**WebFront as separate Go server:**
- Frontend can be deployed/updated independently from Discord bot
- Enables serving static files without Go recompilation (just copy files)
- Clear separation: bot logic vs admin UI
- Can be containerized separately if needed (future flexibility)

**Basic Auth at webfront layer:**
- First line of defense: only authorized users can even load the UI
- Prevents unauthorized access to static files themselves
- API Bearer token still required for actual config changes (defense in depth)
- Two-factor security: something you know (Basic Auth) + something you have (API token)

**API Bearer token stored in UI:**
- Static UI needs API token to make authenticated requests
- Stored in JavaScript variable (loaded from environment or input)
- User enters API token once per session (stored in sessionStorage/memory)
- Token never persisted to disk or localStorage

### Invariants

1. **Static files never execute server-side code**: All rendering happens in browser. Go server only serves files and validates auth.
2. **Basic Auth required for all static file access**: No anonymous access to UI files.
3. **API Bearer token never stored in plaintext files**: Token entered by user in UI, never committed to git.
4. **Config changes always go through API**: UI never writes config.json directly, always via PATCH/PUT endpoints.
5. **WebFront port separate from API port**: Different ports enables independent firewall rules.

### Tradeoffs

**Usability vs. security (two auth prompts):**
- Users enter Basic Auth credentials once per browser session
- Users also enter API Bearer token once per UI session
- More prompts than single-sign-on, but clearer security boundary
- Accepted: small admin team, infrequent access

**Development simplicity vs. build optimization:**
- No bundling means multiple HTTP requests (html, css, js)
- Acceptable for small UI (<50KB total)
- Eliminates build pipeline complexity

**Filesystem serving vs. embedded files:**
- Filesystem serving enables live editing without recompile
- Requires file deployment alongside binary
- Embedded would require rebuild on any UI change
- Accepted: static files rarely change, deployment is simple copy

## Milestones

### Milestone 1: WebFront Go Server with Basic Auth

**Files**:
- `webfront/server.go` (new file)

**Flags**:
- `security`: Basic Auth implementation, credential handling
- `needs-rationale`: Port selection, timeout values

**Requirements**:
- Create `webfront` directory for static assets (empty for now)
- Implement HTTP server in `webfront/server.go` serving files from `webfront/`
- Implement Basic Auth middleware using `crypto/subtle.ConstantTimeCompare`
- Bind to configurable port via `WEBFRONT_PORT` env var (default: 3002)
- Load Basic Auth credentials from `WEBFRONT_USERNAME` and `WEBFRONT_PASSWORD` env vars
- Fail-fast if username/password not set when server starts
- Serve `index.html` at root `/`, all static files with proper Content-Type headers
- Implement graceful shutdown with 30-second timeout (matching API server pattern)
- Log all access attempts with IP address and username (redacted password)

**Acceptance Criteria**:
- Server starts only when `WEBFRONT_USERNAME` and `WEBFRONT_PASSWORD` are set
- Server binds to port from `WEBFRONT_PORT` (default: 3002)
- Request without Basic Auth header returns `401 Unauthorized` with `WWW-Authenticate: Basic` header
- Request with invalid credentials returns `401 Unauthorized`
- Request with valid credentials serves static files from `webfront/` directory
- Server logs access with: `[INFO] webfront access: user=<username> ip=<ip> path=<path>`
- Graceful shutdown completes within 30 seconds

**Tests**:
- **Test files**: `webfront/server_test.go`
- **Test type**: unit
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Valid credentials serves index.html
  - Edge: Missing credentials returns 401 with WWW-Authenticate header
  - Error: Invalid credentials returns 401
  - Error: Missing env vars at startup causes log.Fatal

**Code Intent**:
- New file `webfront/server.go`:
  - `Server` struct with port, username, password, fileSystem path
  - `NewServer()` constructor validating credentials exist
  - `basicAuthMiddleware()` handler wrapping FileServer
  - `Start(ctx context.Context)` method for server lifecycle
  - `Stop()` method for graceful shutdown
- Main server loop: `http.ListenAndServe()` with `basicAuthMiddleware` as root handler
- Basic auth parsing: extract `Authorization` header, validate `Basic ` prefix, base64 decode, compare username:password
- Constant-time comparison for password (username comparison can be fast-fail)

### Milestone 2: Static HTML/CSS/JS Frontend

**Files**:
- `webfront/index.html` (new file)
- `webfront/style.css` (new file)
- `webfront/app.js` (new file)

**Flags**:
- `security`: XSS prevention, safe DOM manipulation
- `needs-rationale`: UI design choices

**Requirements**:
- Create `index.html` with semantic HTML5 structure
- Create `style.css` with responsive, minimal styling (no framework)
- Create `app.js` with:
  - API endpoint configuration (base URL from window.location or configurable)
  - `fetchConfig()` function calling `GET /api/config` with Bearer token
  - `saveConfig()` function calling `PATCH /api/config` with Bearer token
  - `renderConfig()` function displaying config in editable form
  - Token input UI (password field) stored in memory only
  - Error handling with user-friendly messages (no alerts, use DOM elements)
- All user input displayed via `textContent` (not `innerHTML`) to prevent XSS
- JSON encoding for any dynamic content insertion
- Responsive design working on mobile and desktop
- Loading states during API calls

**Acceptance Criteria**:
- `index.html` loads when accessed via webfront server
- Form displays all config fields (server_ip, update_interval, category_order, category_emojis, servers)
- User can edit config values in text inputs
- "Save" button sends PATCH request to API
- Success/failure messages display in UI (not browser alerts)
- API Bearer token input is password-type field, not saved to localStorage
- Loading spinner shows during API requests
- Page refreshes config display on load

**Tests**:
- **Test files**: `webfront/app_test.html` (manual testing file)
- **Test type**: manual (browser-based)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: User enters API token, config loads and displays
  - Edge: Invalid API token shows error message
  - Edge: Save button sends PATCH, success message shows
  - Error: API unreachable shows error message

**Code Intent**:
- `index.html`:
  - HTML5 doctype, viewport meta tag
  - Semantic structure: header, main, footer
  - Token input form at top of page
  - Config form generated by JavaScript (not hardcoded HTML)
  - Save and Refresh buttons
  - Status message container
- `style.css`:
  - CSS custom properties for colors/spacing
  - Flexbox/Grid for layout
  - Mobile-first responsive breakpoints
  - No external font or icon dependencies (use system fonts, emoji for icons)
- `app.js`:
  - `const API_BASE = window.location.origin` (serves from same origin or proxy)
  - `let apiToken = null` (in-memory only)
  - `async function fetchConfig()` using `fetch()` API
  - `async function saveConfig(partialData)` using `fetch()` with PATCH method
  - `function renderConfig(config)` generating form fields dynamically
  - Event listeners for form submission
  - Error display in dedicated DOM element

### Milestone 3: Integration with Main Bot

**Files**:
- `main.go` (modify)
- `webfront/server.go` (modify from M1)

**Flags**:
- `conformance`: Follow existing API server pattern
- `needs-rationale`: WebFront enable/disable logic

**Requirements**:
- Add `WEBFRONT_ENABLED` environment variable (default: false)
- Add `WEBFRONT_PORT` environment variable (default: 3002)
- Add `WEBFRONT_USERNAME` environment variable (required when enabled)
- Add `WEBFRONT_PASSWORD` environment variable (required when enabled)
- Integrate WebFront server startup with existing `Start()` method
- WebFront starts in background goroutine when enabled (matching API server pattern)
- WebFront stops gracefully on shutdown signal
- Log "WebFront server started on port X" when enabled
- Validate credentials at startup before starting server

**Acceptance Criteria**:
- `WEBFRONT_ENABLED=false` (or unset) → WebFront does not start
- `WEBFRONT_ENABLED=true` → WebFront starts on configured port
- Missing username/password when enabled → startup fails with clear error
- SIGTERM/SIGINT → WebFront stops gracefully within 30 seconds
- Bot continues running if WebFront fails (only WebFront logs error)
- Logs show "WebFront server started" or "WebFront disabled"

**Tests**:
- **Test files**: `main_test.go` (add tests)
- **Test type**: integration
- **Backing**: user-specified
- **Scenarios**:
  - Normal: WEBFRONT_ENABLED=true starts webfront server
  - Normal: WEBFRONT_ENABLED=false skips webfront
  - Error: Missing username/password causes startup failure
  - Edge: Shutdown stops webfront gracefully

**Code Intent**:
- Modify `main.go`:
  - Add webfront env vars to global declarations (after api vars, line ~169)
  - Read webfront env vars in `main()` function
  - Validate `WEBFRONT_ENABLED` before starting webfront
  - Add `webfrontServer` and `webfrontCancel` fields to `Bot` struct
  - Create webfront server in `NewBot()` if enabled
  - Start webfront in `Start()` method (matching API server pattern)
  - Stop webfront in `WaitForShutdown()` method
- Modify `webfront/server.go`:
  - Add `NewServer()` returning `*Server` with configured port/creds
  - `Start(ctx)` method launching `http.ListenAndServe()` in goroutine
  - `Stop()` method calling `Shutdown()` with timeout

### Milestone 4: Container Deployment Updates

**Files**:
- `Containerfile` (modify)
- `PODMAN.md` (modify)

**Flags**:
- `security`: Non-root user, file permissions
- `needs-rationale`: Volume mount strategy

**Requirements**:
- Update `Containerfile` to copy `webfront/` directory to image
- Expose port 3002 for WebFront server
- Update `PODMAN.md` with WebFront deployment instructions
- Document volume mount for `webfront/` (allows live editing)
- Document all new environment variables

**Acceptance Criteria**:
- Container image includes `webfront/` directory with default static files
- Port 3002 exposed in Containerfile
- PODMAN.md has WebFront section with env var documentation
- Volume mount example shows local `webfront/` editing
- Security audit: webfront files owned by non-root user

**Tests**:
- **Test type**: manual (container build)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: `podman build` creates image with webfront files
  - Normal: Container with WEBFRONT_ENABLED=true serves UI
  - Edge: Volume-mounted webfront overrides built-in files
  - Security: `ls -la` in container shows non-root ownership

**Code Intent**:
- Modify `Containerfile`:
  - Add `COPY webfront/ /app/webfront/` after bot binary copy
  - Add `EXPOSE 3002` after port 3001 expose
- Modify `PODMAN.md`:
  - Add "## WebFront Configuration" section after API section
  - Table of env vars: WEBFRONT_ENABLED, WEBFRONT_PORT, WEBFRONT_USERNAME, WEBFRONT_PASSWORD
  - Volume mount example: `-v ./webfront:/app/webfront:Z`
  - Security note: HTTPS required for production (reverse proxy)

### Milestone 5: Documentation

**Delegated to**: @agent-technical-writer (mode: post-implementation)

**Source**: `## Invisible Knowledge` section of this plan

**Files**:
- `webfront/README.md` (new file)
- `webfront/CLAUDE.md` (new file)
- `CLAUDE.md` (modify - add webfront section)

**Requirements**:
Delegate to Technical Writer. For documentation format specification:
<file working-dir=".claude" uri="conventions/documentation.md" />

Key deliverables:
- `webfront/README.md`: User-facing documentation for webfront UI
- `webfront/CLAUDE.md`: Navigation index for webfront package
- Root `CLAUDE.md`: Add webfront row to files/sections tables

**Acceptance Criteria**:
- `README.md` exists with usage instructions, architecture diagram, troubleshooting
- `CLAUDE.md` is tabular index (files, when to read)
- Root `CLAUDE.md` updated with webfront references

**Source Material**: `## Invisible Knowledge` section of this plan

## Milestone Dependencies

```
M1 (Go Server)
  │
  ├──> M2 (Static UI) ──┐
  │                      │
  └──────────────────────┴──> M3 (Integration)
                                  │
                                  └──> M4 (Container)
                                          │
                                          └──> M5 (Documentation)
```

Independent milestones (can run in parallel): M1 and M2 initially (M2 needs M1 running for testing, but files don't depend on each other)
