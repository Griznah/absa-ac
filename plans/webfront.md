# Web Frontend Implementation Plan

**Status:** APPROVED
**Approach:** Svelte 5 SPA with TypeScript, Vite build tool, Bearer token authentication, static file deployment
**Supersedes:** `plans/webfront-simple.md` - framework chosen for type safety and better UX

## Overview

Build a modern single-page web application for managing AC Discord Bot configuration through the existing REST API. The frontend uses Svelte 5 with TypeScript, implements Bearer token authentication via a custom login form, and deploys as static files served by either the Go backend (integrated) or nginx (standalone).

## Decision Log

| Decision | Rationale |
|----------|------------|
| **Svelte 5 over React/Vue** | Smaller bundle (~17KB vs 42KB React), compile-time reactivity via runes, built-in TypeScript support, official stable release (Feb 2026) |
| **Svelte over vanilla JS (webfront-simple.md)** | Type safety prevents runtime API errors, reactivity simplifies state management, better DX for CRUD UI with form validation. Tradeoff: requires Node.js build step |
| **Vite over webpack/esbuild** | Official Svelte build tool, instant HMR, optimized Rollup production builds, zero-config TypeScript |
| **Bearer Token Form over Basic Auth** | Custom UI provides better UX, enables logout functionality, reuses existing API_BEARER_TOKEN, no additional credential management |
| **sessionStorage over localStorage** | Auto-clears on browser close, reduced XSS exposure, session-limited auth matches admin use case |
| **Static files over SSR/SSG** | No server complexity, deploy anywhere (CDN/nginx/Go), build-once pattern |
| **TypeScript strict mode** | Compile-time validation, IDE autocomplete, prevents runtime errors in API interaction code |

## Rejected Alternatives

| Alternative | Why Rejected |
|-------------|--------------|
| React | Larger bundle size, virtual DOM overhead for simple CRUD UI |
| Vue 2/3 | Larger bundle, less opinionated than Svelte for this use case |
| Vanilla JS (webfront-simple.md) | No type safety, harder to maintain as UI grows, manual DOM manipulation error-prone. Type safety justified for API integration |
| Basic Auth | Requires additional credentials (WEBFRONT_USERNAME/PASSWORD), no logout capability, worse UX |
| localStorage | Persistent XSS attack surface, harder to implement logout |
| SvelteKit SSR | Adds complexity, requires Node.js runtime, overkill for SPA |
| OAuth2 | Requires additional API endpoints, session storage, CSRF protection - overkill for small admin team |

## Architecture

```
Browser (Svelte SPA)
    ├─ LoginForm.svelte (Bearer token input, sessionStorage)
    ├─ Dashboard.svelte (Config editor, save/refresh)
    ├─ ServerList.svelte (Servers array CRUD)
    ├─ CategoryEditor.svelte (Categories/Emojis)
    └─ GeneralSettings.svelte (server_ip, update_interval)
         │
         │ fetch() with Authorization: Bearer <token>
         │
    Static File Server (Go http.FileServer OR nginx)
         │
    Existing Go API (Bearer auth, rate limiting, CORS)
```

## Invisible Knowledge

### System
- Svelte SPA compiles to vanilla JS, runs entirely in browser
- API remains single source of truth
- Build artifacts in `webfront/dist/` are environment-agnostic
- Token stored in sessionStorage, cleared on browser close

### Invariants
1. Token never persisted to disk (sessionStorage only)
2. Token never logged (Go redacts Bearer headers)
3. All config changes via API (PATCH/PUT endpoints)
4. CORS must allow frontend origin (API_CORS_ORIGINS)
5. Static files are environment-agnostic
6. No secrets in client bundle (API token entered by user)

### Tradeoffs
- Build complexity vs type safety: Requires Node.js for build, but TypeScript prevents runtime errors
- Single-page vs multi-page: SPA requires client-side routing, simpler for single-view app
- Bearer form vs OAuth: Copy-paste token accepted for small team
- sessionStorage vs httpOnly cookie: JS-accessible accepted, simpler logout

## Constraints & Assumptions

**Technical:**
- Existing REST API runs on configurable port (API_PORT env var, default 3001)
- API uses Bearer token authentication (API_BEARER_TOKEN env var)
- CORS controlled via API_CORS_ORIGINS env var (must include frontend origin)
- Svelte 5.50+ with TypeScript 5.x, Vite 6.x
- ES2020+ browser support required

**Organizational:**
- Admin team is small (1-5 users typical)
- No dedicated frontend developer on team
- Deployment preference: static files served by Go http.FileServer (containerized with bot)
- Alternative deployment: nginx on separate host/container

## Known Risks

| Risk | Mitigation | Anchor |
|------|-----------|--------|
| XSS via sessionStorage token | Svelte auto-escaping, CSP headers | Svelte escapes by default |
| Token visible in DevTools | Accepted risk for admin interface | sessionStorage cleared on close |
| CORS misconfiguration | Documented in README, health check validation | Milestone 6 integration tests |
| Build requires Node.js | New dependency, acceptable tradeoff for type safety | Existing Go project unchanged |

## Milestones

### Milestone 1: Svelte Project Setup

**Files:**
- `webfront/package.json`
- `webfront/svelte.config.js`
- `webfront/vite.config.ts`
- `webfront/tsconfig.json`
- `webfront/src/main.ts`
- `webfront/src/App.svelte`
- `webfront/index.html`
- `webfront/src/app.css`
- `webfront/.gitignore`
- `webfront/.eslintrc.cjs`

**Acceptance Criteria:**
- `npm install` completes without errors
- `npm run dev` starts Vite dev server on port 5173
- `npm run build` creates dist/ directory
- `npm run check` runs svelte-check with TypeScript strict mode
- Vite proxies /api to http://localhost:3001 in dev mode

### Milestone 2: TypeScript Types and API Client

**Files:**
- `webfront/src/types.ts` - Config, Server, CategoryEmoji, ErrorResponse interfaces
- `webfront/src/lib/apiClient.ts` - fetch wrapper with Bearer auth
- `webfront/src/lib/configStore.ts` - Svelte 5 runes for config state
- `webfront/src/lib/authStore.ts` - sessionStorage token wrapper

**Acceptance Criteria:**
- TypeScript interfaces match Go Config struct
- apiClient includes Authorization: Bearer <token> header
- 401/403 responses trigger authStore.logout() and redirect to login
- authStore uses sessionStorage for token persistence

### Milestone 3: Login Form Component

**Files:**
- `webfront/src/components/LoginForm.svelte`

**Acceptance Criteria:**
- Password input field with visibility toggle
- Token validation via /health endpoint
- Redirect to Dashboard on successful login
- Error message display on authentication failure
- Auto-focus input on mount
- Accessible (labels, ARIA attributes)

### Milestone 4: Dashboard Component

**Files:**
- `webfront/src/components/Dashboard.svelte`
- `webfront/src/components/ServerList.svelte`
- `webfront/src/components/CategoryEditor.svelte`
- `webfront/src/components/GeneralSettings.svelte`

**Acceptance Criteria:**
- Displays all config fields (server_ip, update_interval, categories, servers)
- Save button sends PATCH request to /api/config
- Success/error toasts after save
- Form validation (port range 1-65535, required fields)
- Logout button in header

### Milestone 5: App Routing and Layout

**Files:**
- `webfront/src/App.svelte` (modify)
- `webfront/src/routes.ts` (route constants)

**Acceptance Criteria:**
- Conditional rendering (LoginForm if !authenticated, Dashboard if authenticated)
- Protected routes (redirect to login if no token)
- Loading state during initial auth check
- Toast notification system

### Milestone 6: Production Build and Go Integration

**Files:**
- `main.go` (modify - add static file serving)
- `Containerfile` (modify - copy webfront/dist/)
- `PODMAN.md` (modify - webfront deployment docs)

**Acceptance Criteria:**
- WEBFRONT_ENABLED=true serves Svelte app on WEBFRONT_PORT (default 8080)
- WEBFRONT_ENABLED=false skips webfront serving
- Containerfile builds Svelte app during docker build
- PODMAN.md documents CORS setup (API_CORS_ORIGINS)
- SPA routing works (all paths return index.html)

### Milestone 7: Documentation

**Files:**
- `webfront/README.md`
- `webfront/CLAUDE.md`
- `CLAUDE.md` (modify - add webfront section)
- `webfront/.env.example`

**Acceptance Criteria:**
- README.md with quick start guide
- CLAUDE.md with file index
- Root CLAUDE.md updated with webfront references
- .env.example documented

## Waves

### Wave 1: Foundation (M1, M2)
Svelte project setup, TypeScript types, API client

### Wave 2: UI Components (M3, M4, M5)
LoginForm, Dashboard, App routing

### Wave 3: Integration (M6)
Go static file serving, container build

### Wave 4: Polish (M7)
Documentation, final testing

## Milestone Dependencies

```
M1 (Svelte Setup)
  │
  └──> M2 (Types + API Client)
           │
           ├──> M3 (LoginForm) ──┐
           │                   │
           │                   └──> M5 (App Routing)
           │                           │
           │                           └──> M6 (Go Integration) ──> M7 (Documentation)
           │
           └──> M4 (Dashboard) ──┘
                   │
                   └──> M5 (App Routing)
```

Parallel execution: M3 and M4 can run in parallel after M2 completes.
