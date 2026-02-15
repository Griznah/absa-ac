# ABSA AC Bot Web Frontend

Single-page application for managing the ABSA AC Bot configuration via REST API. Built with Svelte 5, TypeScript, and Vite.

## Overview

The web frontend provides a user-friendly interface for:

- Bearer token authentication
- Viewing and editing bot configuration
- Managing server entries
- Configuring categories and emojis
- Real-time configuration updates

## Prerequisites

- **Node.js** 18+ or 20+
- **npm** (comes with Node.js)

## Development Setup

```bash
# Install dependencies
cd webfront/
npm install

# Start Vite dev server (with API proxy)
npm run dev
```

The dev server runs on `http://localhost:5173` and proxies `/api` requests to `http://localhost:3001` (the Go backend).

## Build

```bash
# Production build
npm run build
```

Output: `dist/` directory containing:
- `index.html` - Entry point
- `assets/*.js` - Minified, hashed JavaScript bundles
- `assets/*.css` - Minified, hashed stylesheets

## Environment Variables

Create `.env` in the webfront directory (see `.env.example`):

```bash
# API endpoint for the backend (default: /api)
VITE_API_BASE_URL=http://localhost:3001
```

**Note:** In development, Vite proxies `/api` to the backend, so you typically don't need to set this unless using a different backend URL.

## Available Scripts

| Command | Description |
| ------- | ----------- |
| `npm run dev` | Start Vite dev server with hot module replacement |
| `npm run build` | Build for production (output to `dist/`) |
| `npm run preview` | Preview production build locally |
| `npm run check` | Run TypeScript and Svelte type checking |
| `npm run lint` | Run ESLint code quality checks |

## Deployment

### Option A: Go Serves Static Files

Set environment variables on the Go backend:

```bash
# Enable web frontend
WEBFRONT_ENABLED=true

# Port for serving static files (default: 8080)
WEBFRONT_PORT=8080
```

Go serves the `dist/` directory via `http.FileServer`.

### Option B: nginx Serves Static Files

1. Build the frontend: `npm run build`
2. Copy `dist/` contents to nginx document root
3. Configure nginx to serve static files and proxy `/api` to the Go backend

Example nginx config:

```nginx
server {
    listen 80;
    server_name your-domain.com;

    root /var/www/webfront;
    index index.html;

    location / {
        try_files $uri $uri/ /index.html;
    }

    location /api/ {
        proxy_pass http://localhost:3001;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

## Authentication

The frontend uses Bearer token authentication:

1. Enter your API bearer token on the login page
2. Token is validated against the `/api/health` endpoint
3. Valid token stored in `sessionStorage`
4. Token included in `Authorization` header for all API requests

## Architecture

- **Svelte 5** - Reactive UI framework with runes (`$state`, `$effect`)
- **TypeScript** - Type-safe component props and state
- **Vite** - Fast dev server and optimized production builds
- **Svelte Stores** - State management for auth and config
- **Fetch API** - HTTP client with Bearer token injection

## Project Structure

```
webfront/
├── src/
│   ├── components/       # Svelte components
│   │   ├── LoginForm.svelte
│   │   ├── Dashboard.svelte
│   │   ├── GeneralSettings.svelte
│   │   ├── CategoryEditor.svelte
│   │   └── ServerList.svelte
│   ├── lib/            # Core logic
│   │   ├── authStore.ts
│   │   ├── configStore.ts
│   │   └── apiClient.ts
│   ├── types.ts        # TypeScript interfaces
│   ├── App.svelte      # Root component with routing
│   └── main.ts         # Application entry point
├── static/             # Static assets
├── index.html          # HTML template
├── vite.config.ts      # Vite configuration
├── tsconfig.json       # TypeScript configuration
└── package.json        # Dependencies and scripts
```

See `CLAUDE.md` for detailed file documentation.
