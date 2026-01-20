# Static Frontend Architecture

Web interface for managing AC Bot configuration using Alpine.js and session-based authentication.

## Architecture

```
Browser
  index.html (Alpine.js)  styles.css (B&W theme)  app.js (API client)
         │                   │                   │
         └───────────────────┴───────────────────┘
                             │
                             ▼
                    HTTP-only session cookie
                             │
                             │ HTTPS
                             ▼
Go Container
  Proxy Handler (session-based auth)
    - POST /proxy/login
    - POST /proxy/logout
    - GET/POST/PATCH /proxy/api/*
  Bot API Handler (Bearer token auth)
    - GET /api/config
    - PATCH /api/config
    - POST /api/config/validate
  Static File Server
    - GET / -> index.html
    - GET /app.js -> static/js/app.js
    - GET /styles.css -> static/css/styles.css
```

The frontend lives entirely within the Go container, served from `/static` at the root path. Alpine.js provides reactivity without a build step. Session authentication uses HTTP-only cookies (inaccessible to JavaScript XSS), eliminating Bearer token exposure in browser storage. Static files and API endpoints share the same origin, eliminating CORS complexity.

## Data Flow

```
User loads page
    │
    ├─> Fetch static files from Go FileServer
    │   └─> Alpine.js initializes
    │
    ├─> Check for session cookie
    │   ├─> Found: Auto-login (skip auth modal)
    │   └─> Not found: Show login modal
    │
    ├─> User enters Bearer token (or auto-login)
    │   └─> POST /proxy/login {token: "..."}
    │       └─> Receive HTTP-only session cookie
    │
    ├─> Fetch config from GET /proxy/api/config
    │   └─> Alpine reactive state updated
    │
    ├─> User edits form field
    │   └─> Alpine watches changes -> marks dirty
    │
    ├─> User clicks "Save"
    │   ├─> Validate inputs client-side
    │   ├─> PATCH /proxy/api/config with changed fields only
    │   ├─> On success: Refetch full config, clear dirty flag
    │   └─> On error: Display server validation message
    │
    └─> Auto-poll every 30s
        └─> GET /proxy/api/config -> Update if remote changed
```

**Authentication flow**: User enters Bearer token ONCE during login. Frontend sends token to `/proxy/login`, backend validates against bot API, returns HTTP-only session cookie. Subsequent requests include cookie automatically (browser behavior). Backend validates session, adds Bearer token server-side, proxies to bot API. Bearer token never stored in frontend (sessionStorage/localStorage), eliminating XSS exposure risk.

**State synchronization strategy**: Every PATCH triggers a full GET to ensure UI matches server truth. Polling runs at 30s intervals (matching bot's update_interval). When dirty flag is set (user has unsaved edits), polling updates skip config state to prevent data loss; instead, a warning indicator appears showing remote config changed.

## Why This Structure

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

## Invariants

- **Config consistency**: Every PATCH followed by full GET -> ensures UI matches server truth
- **Session authentication**: All API requests include session cookie (automatic) -> backend validates and adds Bearer token server-side
- **Bearer token isolation**: Token never stored in frontend -> HTTP-only cookie prevents JavaScript access
- **Category validation**: Server category dropdown only shows valid categories -> prevents invalid submissions
- **Port range**: Port input validates 1-65535 before sending -> fails fast on client side
- **Non-empty required fields**: Client-side validation mirrors server rules -> reduces round-trips
- **Polling guard**: isPollingRestart mutex prevents concurrent polling restarts -> eliminates race conditions in interval management

## Tradeoffs

| Decision | Benefit | Cost |
|----------|---------|------|
| Alpine.js bundled locally | CSP compliant, self-contained, no network dependency | Must commit vendor file (~50KB) to repo |
| Same container deployment | Simple ops, no CORS | Frontend updates require container rebuild |
| 30s poll interval | Keeps UI in sync, minimal bandwidth | 30s max delay for concurrent edits |
| Session-based auth (HTTP-only cookie) | Bearer token not exposed to XSS, automatic cookie handling | Requires backend proxy implementation |
| PATCH for edits | Smaller payloads, atomic merge | Must merge client-side before sending |

## Security Considerations

**Session-based authentication**: HTTP-only session cookie blocks JavaScript XSS access to Bearer token. Token stored server-side only. Session scoped to `/proxy` path with SameSite=Strict attribute. 4-hour session timeout balances security and UX.

**XSS prevention**: Alpine.js auto-escapes HTML in text bindings. Server names and user input sanitized on display. Server-side validation exists (see `validateConfigStructSafeRuntime` in main.go). Bearer token never accessible via JavaScript (HTTP-only cookie).

**CSP compliance**: Alpine.js bundled locally (not CDN) to maintain `default-src 'self'` policy without weakening security posture. Session cookies use SameSite=Strict to prevent CSRF attacks.

## Dirty Flag State Machine

Three-state dirty flag prevents race conditions between user edits and polling updates:

- **false**: Clean state (no local or remote changes)
- **'local'**: User has unsaved edits (save button enabled)
- **'remote'**: Config changed externally (no local edits yet)

Valid transitions:
- false → 'local': User edits field
- false → 'remote': Polling detects external change
- 'local' → false: Save succeeds
- 'remote' → 'local': User edits remote change

Invariant: Save only valid when dirty === 'local'. Polling skips config update when dirty === 'local' (user's edits take precedence). When dirty === 'local' and polling detects remote change, remoteChanged flag shows warning without overwriting user's unsaved edits.

## Polling Error Recovery

Exponential backoff with jitter prevents thundering herd across concurrent users:

- On fetch error: `pollBackoffInterval = Math.min(interval * 2, 808000) + random(0, 5000)`
- Max backoff: 300s (5 minutes)
- Reset to 30s on successful fetch
- Jitter (0-5s random) prevents synchronized retry storms

Network failures display error message with retry button. 401 responses clear token and show login modal.
