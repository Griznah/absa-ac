# Web Frontend Documentation

Single-page application for managing ABSA AC Bot configuration. Svelte 5 SPA with TypeScript, bearer token authentication, and REST API integration.

## Files

| File | What | When to read |
| ---- | ---- | ------------ |
| `package.json` | npm dependencies and build scripts | Setting up development, adding dependencies |
| `vite.config.ts` | Vite build tool config with dev server proxy | Understanding build process, API proxy setup |
| `tsconfig.json` | TypeScript compiler configuration | Understanding type checking, module resolution |
| `svelte.config.js` | Svelte compiler configuration for Vite | Understanding Svelte preprocessing |
| `.eslintrc.cjs` | ESLint code quality rules | Running linting, understanding code standards |
| `index.html` | HTML entry point with Vite module loading | Understanding app initialization, script loading |

## Source Files

| File | What | When to read |
| ---- | ---- | ------------ |
| `src/main.ts` | Application entry point, mounts App.svelte | Understanding initialization, Svelte runtime |
| `src/App.svelte` | Root component with auth-based routing | Understanding application flow, login/dashboard logic |
| `src/types.ts` | TypeScript interfaces (Config, Server, ErrorResponse) | Understanding data structures, API contracts |
| `src/routes.ts` | Route definitions for conditional rendering | Understanding navigation, routing logic |

## Components

| File | What | When to read |
| ---- | ---- | ------------ |
| `src/components/LoginForm.svelte` | Bearer token form with validation, visibility toggle | Understanding authentication flow, form handling |
| `src/components/Dashboard.svelte` | Main container with toast notifications, logout | Understanding authenticated UI, error handling |
| `src/components/GeneralSettings.svelte` | Server IP and update interval configuration | Understanding general config editing |
| `src/components/CategoryEditor.svelte` | Category ordering and emoji configuration | Understanding category management UI |
| `src/components/ServerList.svelte` | Server list with add/edit/delete operations | Understanding server CRUD operations |

## Stores

| File | What | When to read |
| ---- | ---- | ------------ |
| `src/lib/authStore.ts` | Authentication state (token, isAuthenticated) with sessionStorage persistence | Understanding auth flow, token storage, logout |
| `src/lib/configStore.ts` | Configuration state with load/save API calls | Understanding config management, error handling |
| `src/lib/apiClient.ts` | Fetch wrapper with Bearer token injection and error handling | Understanding API communication, auth headers |

## Authentication Flow

1. User enters token in `LoginForm.svelte`
2. Token validated via `api.health()` (calls `/api/health`)
3. On success, `authStore.login(token)` stores token in sessionStorage
4. `App.svelte` subscribes to authStore, re-renders to show `Dashboard`
5. All API calls include `Authorization: Bearer <token>` header via `apiClient`
6. On 401/403, `apiClient` calls `authStore.logout()` and redirects to `/`

## API Client

The `apiClient.ts` module provides a typed fetch wrapper:

```typescript
// GET /health - validate bearer token
api.health()

// GET /config - fetch current configuration
api.getConfig()

// PATCH /config - update configuration
api.updateConfig({ server_ip: '192.168.1.1', update_interval: 60 })
```

Auto-includes Bearer token from `authStore`, handles 401/403 by logging out, throws on errors.

## State Management

Uses Svelte stores (`writable`) for reactive state:

- **authStore**: `{ isAuthenticated, token }` - persists to sessionStorage
- **configStore**: `{ config, loading, error }` - fetches from API

Components subscribe via `$authStore` or `$configStore` runes for reactive updates.

## Build Process

**Development:**
- Vite dev server on port 5173
- Proxies `/api` to `http://localhost:3001`
- Hot module replacement for fast iteration

**Production:**
- `vite build` outputs to `dist/`
- Minified JS/CSS with content hashes for caching
- Single `index.html` entry point
- Deployed by Go backend (`WEBFRONT_ENABLED=true`) or nginx

## Environment Variables

```bash
# API endpoint (default: /api)
VITE_API_BASE_URL=http://localhost:3001
```

Set via `.env` file during development. In production, typically `/api` (relative path).

## Deployment

**Option A: Go Backend**
```bash
WEBFRONT_ENABLED=true    # Enable static file serving
WEBFRONT_PORT=8080        # Port for web interface
```

**Option B: nginx**
Serve `dist/` as static files, proxy `/api` to Go backend.

See `README.md` for complete deployment instructions.
