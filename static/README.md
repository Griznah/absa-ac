# Static Frontend Architecture

Web interface for managing AC Bot configuration using Alpine.js and the REST API.

## Architecture

```
Browser
  index.html (Alpine.js)  styles.css (B&W theme)  app.js (API client)
         │                   │                   │
         └───────────────────┴───────────────────┘
                             │
                             ▼
                    sessionStorage (token)
                             │
                             │ HTTPS
                             ▼
Go Container
  API Handler (existing)
    - GET /api/config
    - PATCH /api/config
    - POST /api/config/validate
  Static File Server (new)
    - GET / -> index.html
    - GET /app.js -> static/js/app.js
    - GET /styles.css -> static/css/styles.css
```

The frontend lives entirely within the Go container, served from `/static` at the root path. Alpine.js provides reactivity without a build step. API bearer tokens persist in sessionStorage (cleared on browser close). Static files and API endpoints share the same origin, eliminating CORS complexity.

## Data Flow

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
- **Token presence**: All API requests include Authorization header -> enforced by API client wrapper
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
| Session storage token | Cleared on close (security) | User must re-enter token each browser session |
| PATCH for edits | Smaller payloads, atomic merge | Must merge client-side before sending |

## Security Considerations

**Token storage**: Session storage accessible via DevTools (documented risk). Recommend rotating tokens regularly. Tokens cleared on browser close (security) but persist across tab refreshes (UX).

**XSS prevention**: Alpine.js auto-escapes HTML in text bindings. Server names and user input sanitized on display. Server-side validation exists (see `validateConfigStructSafeRuntime` in main.go).

**CSP compliance**: Alpine.js bundled locally (not CDN) to maintain `default-src 'self'` policy without weakening security posture.

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

- On fetch error: `pollBackoffInterval = Math.min(interval * 2, 300000) + random(0, 5000)`
- Max backoff: 300s (5 minutes)
- Reset to 30s on successful fetch
- Jitter (0-5s random) prevents synchronized retry storms

Network failures display error message with retry button. 401 responses clear token and show login modal.
