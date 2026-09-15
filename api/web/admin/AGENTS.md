# api/web/admin/

Admin SPA: login + config editor. Vanilla JS, embedded in Go binary.

| File | What |
| ---- | ---- |
| `README.md` | Design decisions: vanilla JS, sessionStorage, CSRF flow, CSP |
| `index.html` | Page structure, login form, editor, script load order |
| `auth.js` | Login/logout, sessionStorage token, CSRF fetch |
| `api.js` | Fetch wrapper: auto Authorization + X-CSRF-Token headers, download/upload |
| `app.js` | Init, config editor CRUD, XSS prevention, download/upload handlers |
| `styles.css` | Dark theme, responsive layout |
