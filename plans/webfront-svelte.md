# Svelte Web Frontend with Bearer Token Authentication

## Overview

Implement a modern single-page application (SPA) using Svelte 5 with TypeScript that provides a browser-based interface for the existing REST API. The frontend will use a custom login form for Bearer token authentication (stored in sessionStorage) and deploy as static files served either by a standalone nginx server or by the existing Go backend.

This approach balances developer ergonomics with operational simplicity, providing a reactive UI with minimal runtime overhead and no server-side rendering complexity.

## Planning Context

### Decision Log

| Decision | Reasoning Chain |
|----------|-----------------|
| Svelte over React/Vue | Smaller bundle size and faster runtime due to compile-time framework -> No virtual DOM diffing overhead -> Built-in reactivity with runes (Svelte 5) -> TypeScript support out of the box -> Simpler learning curve for this CRUD use case |
| Vite over webpack/esbuild | Vite is the official Svelte build tool -> Instant hot module replacement for fast development -> Optimized production builds with Rollup -> Native ES module support in dev -> Zero-config TypeScript handling |
| Bearer Token Form over Basic Auth | Custom UI provides better UX than browser's Basic Auth popup -> Token stored in sessionStorage (cleared on browser close) -> Enables "logout" functionality (not possible with Basic Auth) -> Consistent with existing API_BEARER_TOKEN infrastructure -> No need for additional Basic Auth credentials management |
| sessionStorage over localStorage | Token automatically cleared when browser/tab closes -> Reduces risk of persistent token exposure -> Session-limited auth matches admin use case -> Still survives page refreshes within same session |
| Static files over SSR/SSG | No server-side rendering complexity -> Can be served by any static file server (nginx, Go http.FileServer) -> Enables deployment flexibility (CDN, separate container, or embedded in Go) -> App is purely client-side, API already handles all business logic -> Build once, deploy anywhere pattern |
| TypeScript over JavaScript | Type safety reduces runtime errors in API interaction code -> Better IDE support and autocomplete for config schema types -> Catches typos in API endpoint paths and response handling -> Easier refactoring as UI grows |
| Svelte 5 Runes over Svelte 4 stores | Runes provide more intuitive reactivity ($state, $derived) -> Better TypeScript inference -> Simpler mental model for component state -> Official stable release (Feb 2026) -> Future-proof choice |

### Rejected Alternatives

| Alternative | Why Rejected |
|-------------|--------------|
| React with Vite | Larger bundle size (~42KB vs ~17KB for Svelte), virtual DOM overhead for simple CRUD UI, more boilerplate code. Svelte provides same DX with smaller runtime. |
| Basic Auth with nginx | Requires managing additional user credentials (WEBFRONT_USERNAME/PASSWORD), browser cannot logout without closing browser, less polished UX for admin interface. |
| localStorage for token | Persists across browser sessions, increases attack surface if XSS vulnerability found, harder to implement "logout" functionality. |
| SvelteKit (SSR) | Adds complexity with server-side rendering, requires Node.js server or adapter configuration, overkill for single-page CRUD app. Static build is simpler. |
| Vanilla JS with no build | No type safety, harder to maintain as UI grows, no hot module replacement, manual DOM manipulation is more error-prone than Svelte's reactivity. |
| OAuth2/Session-based auth | Requires additional API endpoints, session storage backend, CSRF tokens. Overkill for single-admin or small-team use case. Bearer token already exists. |

### Constraints & Assumptions

**Technical:**
- Existing REST API runs on configurable port (API_PORT env var, default 3001)
- API uses Bearer token authentication (API_BEARER_TOKEN env var)
- CORS controlled via API_CORS_ORIGINS env var (must include frontend origin)
- Project has no existing Node.js dependencies (greenfield frontend)
- Static files deploy to `webfront/dist/` directory after build
- Browser must support ES2020+ (Edge 88+, Firefox 78+, Chrome 80+, Safari 14+)
- Svelte 5.50+ with TypeScript 5.x
- Vite 6.x as build tool

**Organizational:**
- Admin team is small (1-5 users typical)
- No dedicated frontend developer on team
- Deployment preference: static files served by Go http.FileServer (containerized with bot)
- Alternative deployment: nginx on separate host/container
- Single-page application (no multi-page routing needed)

**Dependencies:**
- `svelte` 5.50+ (UI framework)
- `svelte-check` (TypeScript checking)
- `@sveltejs/vite-plugin-svelte` (Vite integration)
- `typescript` (TypeScript compiler)
- `vite` (Build tool)
- `eslint` + `eslint-plugin-svelte` (Code quality)

**Default conventions:**
- Self-documenting code over comments
- Conventional commits for git
- Svelte 5 runes syntax ($state, $derived, $effect)
- TypeScript strict mode enabled

### Known Risks

| Risk | Mitigation | Anchor |
|------|-----------|--------|
| XSS through sessionStorage token exposure | Input sanitization, CSP headers, no innerHTML usage | Svelte's auto-escaping prevents XSS by default |
| Token in browser memory accessible via DevTools | Acceptable risk for admin interface, sessionStorage cleared on tab close | Same as any client-side token storage |
| CORS misconfiguration blocking API access | Clear documentation of API_CORS_ORIGINS setup, health check endpoint for validation | Milestone 3 integration tests |
| Svelte 5 breaking changes (future) | Pin Svelte version in package.json, follow semver updates | Build will fail on breaking changes |
| Static file serving exposes source maps | Disable source maps in production build, use separate dev build | Vite production build excludes source maps |
| Token visible in network requests (Bearer header) | All traffic should use HTTPS in production | SECURITY.md requires TLS in production |

## Invisible Knowledge

### Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                          Browser (Client Side)                               │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                      Svelte SPA (compiled to JS)                     │   │
│  │  ┌────────────────────────────────────────────────────────────────┐  │   │
│  │  │ Components:                                                    │  │   │
│  │  │  - App.svelte (root, router-like)                              │  │   │
│  │  │  - LoginForm.svelte (Bearer token input)                       │  │   │
│  │  │  - Dashboard.svelte (main config view)                         │  │   │
│  │  │  - ServerList.svelte (servers array editor)                    │  │   │
│  │  │  - CategoryEditor.svelte (categories/emojis)                   │  │   │
│  │  └────────────────────────────────────────────────────────────────┘  │   │
│  │  ┌────────────────────────────────────────────────────────────────┐  │   │
│  │  │ Stores/Services:                                               │  │   │
│  │  │  - authStore.ts (sessionStorage token wrapper)                 │  │   │
│  │  │  - apiClient.ts (fetch wrapper with Bearer auth)               │  │   │
│  │  │  - configStore.ts (Svelte 5 runes for config state)            │  │   │
│  │  │  - types.ts (TypeScript interfaces for config schema)          │  │   │
│  │  └────────────────────────────────────────────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
│                              │                                               │
│                              │ fetch() with Authorization: Bearer <token>    │
│                              ▼                                               │
│  ┌─────────────────────────────────────────────────────────────────────┐   │
│  │                      Static File Server                              │   │
│  │                      (nginx OR Go http.FileServer)                   │   │
│  │  ┌────────────────────────────────────────────────────────────────┐  │   │
│  │  │ Serves: index.html, assets/*.js, assets/*.css                  │  │   │
│  │  │ Location: webfront/dist/                                        │  │   │
│  │  └────────────────────────────────────────────────────────────────┘  │   │
│  └─────────────────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
                                      │
                                      │ HTTP/JSON
                                      ▼
┌─────────────────────────────────────────────────────────────────────────────┐
│                         Existing Go Backend API                             │
│  ┌────────────────────────────────────────────────────────────────────┐    │
│  │ Endpoints:                                                          │    │
│  │  - GET  /api/config        (get full config)                        │    │
│  │  - GET  /api/config/servers (get servers only)                      │    │
│  │  - PATCH /api/config        (partial update)                        │    │
│  │  - PUT  /api/config        (replace full config)                    │    │
│  │  - POST /api/config/validate (validate config)                      │    │
│  │  - GET  /health              (health check)                          │    │
│  │  - GET  /api/csrf-token     (CSRF token for form POST)              │    │
│  └────────────────────────────────────────────────────────────────────┘    │
│  ┌────────────────────────────────────────────────────────────────────┐    │
│  │ Middleware:                                                          │    │
│  │  - BearerAuth (validates API_BEARER_TOKEN)                          │    │
│  │  - RateLimit (10 req/sec per IP)                                    │    │
│  │  - CORS (validates API_CORS_ORIGINS)                                │    │
│  │  - SecurityHeaders                                                  │    │
│  └────────────────────────────────────────────────────────────────────┘    │
└─────────────────────────────────────────────────────────────────────────────┘
```

### Data Flow

```
┌──────────────────────────────────────────────────────────────────────────────┐
│                         Authentication Flow                                  │
└──────────────────────────────────────────────────────────────────────────────┘

User Opens Browser
    │
    ▼
Svelte App Loads (index.html)
    │
    ├─> authStore.checkSession()
    │       │
    │       ├─> sessionStorage.getItem('bearer_token')
    │       │
    │       ├─> Token found? ──Yes──> Dashboard.svelte
    │       │
    │       └─> No token ──> LoginForm.svelte
    │
    ▼
User Enters Bearer Token in LoginForm
    │
    ├─> authStore.login(token)
    │       │
    │       ├─> apiClient.get('/health') with token
    │       │
    │       ├─> Success? ──Yes──> sessionStorage.setItem('bearer_token', token)
    │       │                └─> Navigate to Dashboard
    │       │
    │       └─> Error ──> Show error message in LoginForm
    │
    ▼
Dashboard Loads
    │
    ├─> configStore.loadConfig()
    │       │
    │       ├─> apiClient.get('/api/config')
    │       │
    │       └─> Display config in editable form
    │
    ▼
User Edits Config → Clicks "Save"
    │
    ├─> configStore.saveConfig(partialData)
    │       │
    │       ├─> apiClient.patch('/api/config', partialData)
    │       │
    │       ├─> Success? ──Yes──> Show success toast
    │       │                └─> Reload config from API
    │       │
    │       └─> Error ──> Show error toast with details
    │
    ▼
User Clicks "Logout"
    │
    ├─> authStore.logout()
    │       │
    │       ├─> sessionStorage.removeItem('bearer_token')
    │       │
    │       └─> Navigate to LoginForm

┌──────────────────────────────────────────────────────────────────────────────┐
│                          Deployment Flow                                    │
└──────────────────────────────────────────────────────────────────────────────┘

Development:
    npm run dev
        │
        ├─> Vite dev server (port 5173)
        ├─> HMR for instant updates
        └─> Proxy /api to Go backend at localhost:3001

Production Build:
    npm run build
        │
        ├─> Vite + Rollup build
        ├─> Output: webfront/dist/
        │   ├─> index.html
        │   ├─> assets/*.js (minified, hashed)
        │   └─> assets/*.css (minified, hashed)
        └─> No source maps (production)

Deployment Option A (Go embedded):
    1. Build Svelte app: npm run build
    2. Copy dist/ to Go project
    3. Go serves via http.FileServer
    4. Single container, single process

Deployment Option B (nginx separate):
    1. Build Svelte app: npm run build
    2. Copy dist/ to nginx container
    3. nginx serves static files
    4. nginx proxies /api to Go backend
    5. Two containers, separate scaling
```

### Why This Structure

**Svelte SPA as client-side only:**
- No server-side rendering means simpler deployment (just static files)
- All business logic remains in Go API (single source of truth)
- UI is purely presentational layer
- Enables independent frontend/backend development

**Bearer token in sessionStorage:**
- Reuses existing API_BEARER_TOKEN infrastructure (no new auth mechanism)
- Token cleared automatically when browser closes
- "Logout" button can manually clear token
- No persistent credentials on client device
- Session-limited security matches admin use case

**Static file deployment flexibility:**
- Can be served by Go (single container deployment)
- Can be served by nginx (separate frontend/backend scaling)
- Can be deployed to CDN (edge caching for static assets)
- Build artifacts are environment-agnostic
- Same build works in dev, staging, production

**TypeScript types for config schema:**
- Compile-time validation of config structure
- IDE autocomplete for config fields
- Catches typos before runtime
- Easier onboarding for new developers

### Invariants

1. **Token never persisted to disk**: sessionStorage only, cleared on browser close
2. **Token never logged**: All API logs redact Bearer tokens (existing Go behavior)
3. **All config changes go through API**: UI never writes files directly, only PATCH/PUT endpoints
4. **CORS must allow frontend origin**: API_CORS_ORIGINS must include frontend URL
5. **Static files are environment-agnostic**: Same dist/ works for dev/staging/prod
6. **No secrets in client bundle**: API token entered by user, never embedded in build

### Tradeoffs

**Build complexity vs. type safety:**
- Requires Node.js/npm for build (new dependency for Go project)
- Acceptable tradeoff: TypeScript prevents runtime errors, better DX
- Build output is still just static files (no Node.js runtime needed in production)

**Single-page vs. multi-page:**
- SPA requires client-side routing (or no routing for single-view app)
- Simpler for this use case: one dashboard view, no complex navigation
- Acceptable: config editor is a single-screen application

**Bearer token form vs. OAuth:**
- Bearer form: copy-paste token from env var
- OAuth: additional redirect flow, more API endpoints
- Bearer form accepted: small team, token rotation is manual process anyway

**sessionStorage vs. httpOnly cookie:**
- sessionStorage: accessible to JS, cleared on close
- httpOnly cookie: not accessible to JS, more secure
- sessionStorage accepted: XSS risk mitigated by CSP, simpler logout

## Milestones

### Milestone 1: Svelte Project Setup with TypeScript

**Files**:
- `webfront/package.json` (new file)
- `webfront/svelte.config.js` (new file)
- `webfront/vite.config.ts` (new file)
- `webfront/tsconfig.json` (new file)
- `webfront/src/main.ts` (new file)
- `webfront/src/App.svelte` (new file)
- `webfront/index.html` (new file)
- `webfront/src/app.css` (new file)
- `webfront/.gitignore` (new file)
- `webfront/.eslintrc.cjs` (new file)

**Flags**:
- `security`: Dependency versions, TypeScript strict mode
- `needs-rationale`: Build tool configuration choices

**Requirements**:
- Initialize Svelte 5 project with TypeScript strict mode
- Configure Vite with /api proxy to localhost:3001 in dev mode
- Create base App.svelte component with Svelte 5 runes syntax
- Configure ESLint with svelte-plugin
- Set up .gitignore for node_modules, dist, .env
- Add npm scripts: dev, build, preview, check, lint
- Create empty LoginForm and Dashboard component stubs
- Configure Svelte to compile to custom element or div target

**Acceptance Criteria**:
- `npm install` completes without errors
- `npm run dev` starts Vite dev server on port 5173
- `npm run build` creates dist/ directory with index.html and assets/
- `npm run check` runs svelte-check with TypeScript strict mode (no errors)
- App.svelte renders without console errors in browser
- Vite proxies /api requests to http://localhost:3001 in dev mode
- ESLint runs without configuration errors

**Tests**:
- **Test type**: manual (build verification)
- **Backing**: user-specified
- **Scenarios**:
  - Normal: `npm install && npm run build` succeeds
  - Edge: TypeScript error caught by `npm run check`
  - Error: Invalid Svelte syntax caught by linter

**Code Intent**:
- `package.json`: Dependencies (svelte@^5.50.0, vite@^6.x, typescript@^5.x, @sveltejs/vite-plugin-svelte)
- `vite.config.ts`: Dev server proxy for /api to Go backend
- `tsconfig.json`: `"strict": true`, `"moduleResolution": "bundler"`
- `svelte.config.js`: Vite plugin configuration
- `App.svelte`: Root component with `<slot />` for child components
- `main.ts`: Mount App.svelte to `#app` div

### Milestone 2: TypeScript Types and API Client

**Files**:
- `webfront/src/types.ts` (new file)
- `webfront/src/lib/apiClient.ts` (new file)
- `webfront/src/lib/configStore.ts` (new file)
- `webfront/src/lib/authStore.ts` (new file)

**Flags**:
- `security`: Token handling, error handling
- `needs-rationale`: Store architecture (Svelte runes vs. stores)

**Requirements**:
- Define TypeScript interfaces matching Go Config struct
- Create apiClient with fetch wrapper for Bearer auth
- Implement authStore using Svelte 5 $state runes
- Implement configStore using Svelte 5 $state runes
- Add error handling for 401/403 responses (clear token, redirect to login)
- Add retry logic for failed requests (optional, max 1 retry)
- Create typed functions for each API endpoint

**Acceptance Criteria**:
- `types.ts` has interfaces: Config, Server, CategoryEmoji, ErrorResponse
- `apiClient.ts` exports: getConfig(), patchConfig(), putConfig(), validateConfig(), getHealth()
- `apiClient` includes Authorization: Bearer <token> header on all requests
- `authStore` has: login(token), logout(), isAuthenticated(), getToken()
- `authStore` uses sessionStorage for token persistence
- `configStore` has: loadConfig(), updateConfig(field, value), saveConfig()
- 401/403 responses trigger authStore.logout() and redirect to login form
- TypeScript compiles without type errors

**Tests**:
- **Test files**: `webfront/src/lib/apiClient.test.ts` (vitest)
- **Test type**: unit
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Valid getConfig() returns typed Config object
  - Edge: 401 response triggers logout
  - Error: Network error throws ApiError with details

**Code Intent**:
- `types.ts`:
  ```typescript
  export interface Server {
    name: string;
    port: number;
    category: string;
  }
  export interface Config {
    server_ip: string;
    update_interval: number;
    category_order: string[];
    category_emojis: Record<string, string>;
    servers: Server[];
  }
  export interface ErrorResponse {
    error: string;
    details?: string;
  }
  ```
- `apiClient.ts`:
  - `class ApiClient` with private `baseUrl` and `getToken()` callback
  - Methods: `get<T>(path)`, `patch<T>(path, body)`, `put<T>(path, body)`
  - Automatic Bearer token injection from authStore
  - Generic return types for type safety
- `authStore.ts`:
  - `let token = $state<string | null>(null)`
  - `function login(newToken: string)` validates token with /health
  - `function logout()` clears sessionStorage and sets token to null
  - Initialize token from sessionStorage on load
- `configStore.ts`:
  - `let config = $state<Config | null>(null)`
  - `let loading = $state(false)`
  - `let error = $state<string | null>(null)`
  - `async function loadConfig()` calls apiClient.getConfig()

### Milestone 3: Login Form Component

**Files**:
- `webfront/src/components/LoginForm.svelte` (new file)

**Flags**:
- `security`: Token masking, error display
- `needs-rationale`: UX choices (auto-focus, password visibility toggle)

**Requirements**:
- Create LoginForm component with password input field
- Mask token input (type="password") with visibility toggle
- Show error message for invalid token
- Submit button with loading state
- Auto-focus input on mount
- Clear token from sessionStorage on failed login
- Redirect to Dashboard on successful login
- Add "show/hide" eye icon for token visibility

**Acceptance Criteria**:
- LoginForm renders with password input and submit button
- Token input is masked by default (dots)
- Eye icon toggles token visibility
- Submit button calls authStore.login(token)
- Loading spinner shows during authentication
- Error message displays on invalid token (401/403)
- Successful login navigates to Dashboard
- Enter key submits form
- Form is accessible (labels, ARIA attributes)

**Tests**:
- **Test files**: `webfront/src/components/LoginForm.test.ts` (vitest)
- **Test type**: unit
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Valid token redirects to Dashboard
  - Edge: Empty input shows validation error
  - Error: Invalid token shows error message

**Code Intent**:
- `LoginForm.svelte`:
  - Use Svelte 5 `<script>` with runes syntax
  - `let tokenInput = $state("")`
  - `let showToken = $state(false)`
  - `let errorMsg = $state<string | null>(null)`
  - `let loading = $state(false)`
  - `async function handleSubmit()` calls authStore.login(), handles errors

### Milestone 4: Dashboard Component

**Files**:
- `webfront/src/components/Dashboard.svelte` (new file)
- `webfront/src/components/ServerList.svelte` (new file)
- `webfront/src/components/CategoryEditor.svelte` (new file)
- `webfront/src/components/GeneralSettings.svelte` (new file)

**Flags**:
- `security`: XSS prevention (Svelte auto-escapes)
- `needs-rationale`: UI layout, form validation

**Requirements**:
- Create Dashboard as main config view
- Split into sections: General Settings, Categories, Servers
- Add "Save Changes" button with loading state
- Add "Refresh" button to reload from API
- Show success/error toasts after save
- Validate inputs (port range, required fields)
- Add "Logout" button in header
- Show loading spinner during initial load

**Acceptance Criteria**:
- Dashboard displays all config fields
- GeneralSettings edits server_ip and update_interval
- CategoryEditor edits category_order and category_emojis
- ServerList displays servers array with add/edit/delete
- Save button sends PATCH request to /api/config
- Success toast shows on save
- Error toast shows with details on failure
- Logout button clears token and redirects to LoginForm
- All inputs use Svelte two-way binding ($bind)
- Form validation prevents invalid saves (port range, required fields)

**Tests**:
- **Test files**: `webfront/src/components/Dashboard.test.ts` (vitest)
- **Test type**: unit
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Edit field, save, success toast shows
  - Edge: Invalid port shows validation error
  - Error: API error shows error toast

**Code Intent**:
- `Dashboard.svelte`:
  - Use configStore for state
  - `let showSaveToast = $state(false)`
  - `let showErrorToast = $state(false)`
  - `let toastMessage = $state("")`
  - `async function handleSave()` calls configStore.saveConfig()
  - Layout: Header (title, logout button), Main (form sections), Footer (save/cancel buttons)
- `ServerList.svelte`:
  - Use `<table>` for servers display
  - Add row button adds empty server
  - Delete button removes server
  - Each row: name (text), port (number), category (select from category_order)
- `CategoryEditor.svelte`:
  - category_order: drag-and-drop list or up/down buttons
  - category_emojis: key-value pairs editor

### Milestone 5: App Routing and Layout

**Files**:
- `webfront/src/App.svelte` (modify from M1)
- `webfront/src/routes.ts` (new file - simple route constants)

**Flags**:
- `needs-rationale`: Routing approach (conditional rendering vs. router)

**Requirements**:
- Implement simple routing in App.svelte (conditional rendering)
- Routes: LOGIN, DASHBOARD (no complex router needed)
- Protect Dashboard route (redirect to login if not authenticated)
- Add global error boundary for unhandled errors
- Add loading state during initial auth check
- Add toast notification system (success/error messages)

**Acceptance Criteria**:
- App.svelte shows LoginForm if not authenticated
- App.svelte shows Dashboard if authenticated
- Direct access to Dashboard redirects to login if no token
- Unhandled errors show error page
- Loading spinner shows during initial auth check
- Toast notifications appear fixed at top/bottom of screen

**Tests**:
- **Test files**: `webfront/src/App.test.ts` (vitest)
- **Test type**: unit
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Valid token shows Dashboard
  - Edge: No token shows LoginForm
  - Error: API error during auth check shows LoginForm

**Code Intent**:
- `App.svelte`:
  ```svelte
  <script lang="ts">
    import { onMount } from 'svelte';
    import { authStore } from '$lib/authStore';
    import LoginForm from '$components/LoginForm.svelte';
    import Dashboard from '$components/Dashboard.svelte';

    let initialized = $state(false);

    onMount(async () => {
      await authStore.checkSession();
      initialized = true;
    });
  </script>

  {#if !initialized}
    <LoadingSpinner />
  {:else if !authStore.isAuthenticated()}
    <LoginForm />
  {:else}
    <Dashboard />
  {/if}
  ```

### Milestone 6: Production Build and Go Integration

**Files**:
- `main.go` (modify - add static file serving)
- `webfront/.env.example` (new file)
- `Containerfile` (modify - add static file copy)
- `PODMAN.md` (modify - add webfront documentation)

**Flags**:
- `security`: File permissions, CORS configuration
- `needs-rationale`: Serving strategy (Go vs. nginx)

**Requirements**:
- Add WEBFRONT_ENABLED env var (default: false)
- Serve static files from webfront/dist/ when enabled
- Add /webfront path prefix for static assets
- Update Containerfile to copy dist/ after build
- Add WEBFRONT_PORT env var (default: 8080)
- Document CORS setup in PODMAN.md (API_CORS_ORIGINS)
- Add health check for static file serving

**Acceptance Criteria**:
- `WEBFRONT_ENABLED=true` serves Svelte app on WEBFRONT_PORT
- `WEBFRONT_ENABLED=false` skips webfront
- Containerfile builds Svelte app during docker build
- Container includes webfront/dist/ directory
- PODMAN.md documents API_CORS_ORIGINS setup for webfront
- Static files served with correct Content-Type headers
- SPA routing works (all paths return index.html)

**Tests**:
- **Test files**: Integration test (manual container test)
- **Test type**: manual
- **Backing**: user-specified
- **Scenarios**:
  - Normal: Container serves Svelte app at /webfront
  - Edge: WEBFRONT_ENABLED=false skips serving
  - Error: Missing dist/ fails container build

**Code Intent**:
- Modify `main.go`:
  - Add webfront env vars (WEBFRONT_ENABLED, WEBFRONT_PORT)
  - Add static file server using `http.FileServer`
  - Serve on separate port from API
  - Use `embed.FS` or filesystem path for dist/
- Modify `Containerfile`:
  ```dockerfile
  # Install Node.js for build
  RUN apk add --no-cache nodejs npm

  # Build Svelte app
  WORKDIR /app/webfront
  COPY webfront/package*.json ./
  RUN npm ci
  COPY webfront/ ./
  RUN npm run build

  # Copy dist to final location
  RUN mkdir -p /app/webfront/dist && \
      cp -r dist/* /app/webfront/dist/

  EXPOSE 3001 8080
  ```

### Milestone 7: Documentation

**Delegated to**: @agent-technical-writer (mode: post-implementation)

**Files**:
- `webfront/README.md` (new file)
- `webfront/CLAUDE.md` (new file)
- `CLAUDE.md` (modify - add webfront section)
- `webfront/.env.example` (modify - add comments)

**Requirements**:
- Document development workflow (npm run dev)
- Document production build process (npm run build)
- Document API integration (CORS setup)
- Document environment variables
- Document deployment options (Go vs. nginx)
- Document authentication flow (Bearer token)

**Acceptance Criteria**:
- README.md exists with quick start guide
- CLAUDE.md exists with file index
- Root CLAUDE.md updated with webfront section
- .env.example documented

## Milestone Dependencies

```
M1 (Svelte Setup)
  │
  ├──> M2 (Types + API Client)
  │           │
  │           ├──> M3 (LoginForm)
  │           │       │
  │           │       └──> M5 (App Routing)
  │           │               │
  │           │               └──> M6 (Go Integration)
  │           │                       │
  │           │                       └──> M7 (Documentation)
  │           │
  │           └──> M4 (Dashboard)
  │                   │
  │                   └──> M5 (App Routing)
  │
  └──> M6 (Go Integration) ──> M7 (Documentation)
```

Parallel execution: M3 and M4 can run in parallel after M2 completes.

## Waves

### Wave 1: Foundation (M1, M2)
Set up Svelte project, TypeScript types, and API client. Enables parallel development of UI components.

### Wave 2: UI Components (M3, M4, M5)
Build LoginForm, Dashboard, and App routing. Creates functional UI.

### Wave 3: Integration (M6)
Go static file serving, container build, CORS configuration. Enables deployment.

### Wave 4: Polish (M7)
Documentation, examples, final testing. Production ready.
