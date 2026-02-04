# Static Frontend

Alpine.js web UI for configuration management.

## Files

| File | What | When to read |
| ---- | ---- | ------------ |
| `README.md` | Architecture, data flow, invariants, tradeoffs for static frontend | Understanding frontend design decisions, deployment model, security considerations |
| `index.html` | Main HTML structure with Alpine.js directives and form layout | Modifying UI structure, adding new config fields, understanding component hierarchy |
| `css/styles.css` | Black/white theme styles with CSS variables for color tokens | Adjusting visual design, theming, fixing layout issues |
| `js/app.js` | Alpine.js application logic with API client, reactive state, polling | Understanding state management, API integration, adding new features |
| `js/alpine.min.js` | Alpine.js v3.14.0 bundled locally (vendor file) | Verifying version, CSP compliance, debugging framework issues |

## Subdirectories

| Directory | What | When to read |
| --------- | ---- | ------------ |
| `test/` | Frontend tests (property-based unit tests, integration tests with Playwright) | Understanding test coverage, running tests, adding new tests |
