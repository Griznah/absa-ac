# Admin Frontend

Single-page web UI for AC Bot configuration management, embedded in Go binary.

## Files

| File | What | When to read |
| ---- | ---- | ------------ |
| `README.md` | Architecture decisions, security design, authentication flow, CSP requirements | Understanding why vanilla JS, sessionStorage choice, CSRF flow |
| `index.html` | Base HTML structure with login form, config editor sections, JS module loading | Understanding page structure, screen layout, script load order |
| `auth.js` | Login/logout flow, token management in sessionStorage, CSRF token fetch | Modifying auth behavior, understanding token storage strategy |
| `api.js` | Fetch wrapper with auto-included Authorization and X-CSRF-Token headers | Modifying API calls, understanding request/response handling |
| `app.js` | Main app initialization, config editor with CRUD operations, XSS prevention | Modifying UI behavior, understanding config editing flow |
| `styles.css` | Dark theme styling, responsive layout, form/button styling | Modifying visual appearance, understanding responsive breakpoints |
