# Auto-Refresh Admin GUI

## Overview

`validateConfig()` does not refresh UI after successful validation, unlike `saveConfig()` and `handleFileSelect()` which both call `loadConfig()` to show current server state.

**Approach**: Add `await this.loadConfig()` call after successful validateConfig response, following established pattern.

**Note**: Upload already calls `loadConfig()` after success (app.js:372), so this plan only addresses the validate button.

## Planning Context

### Decision Log

| ID | Decision | Reasoning Chain |
|---|---|---|
| DL-001 | Add loadConfig call after validateConfig success | saveConfig and handleFileSelect already call loadConfig after success -> validateConfig should follow same pattern for consistency -> user sees current server state after validate |

### Constraints

- MUST: Maintain vanilla JS approach (no framework)
- MUST: Preserve XSS prevention patterns (textContent, no innerHTML for user content)
- MUST: Handle async operations properly (await loadConfig after actions)

### Known Risks

- **loadConfig failure after successful validate leaves UI with stale config state**: User sees validation success but UI shows old data. Mitigation: Existing error handling in `loadConfig()` will show error message to user.

## Invisible Knowledge

### System

Admin GUI is vanilla JS SPA; `loadConfig()` fetches from `GET /api/config`; validate endpoint only checks JSON syntax.

### Invariants

- `loadConfig()` always replaces `this.config` AND calls `renderConfig()` to update UI (app.js:126-134)
- `validateConfig()` sends current form state to `/config/validate` endpoint for JSON syntax check only
- Upload writes config via `WriteConfigAny` and returns updated config

### Tradeoffs

- Validate endpoint returns 501 with `json_syntax` info but does NOT return config (handlers.go:180-186) - this is why `loadConfig()` call is needed after validate (unlike upload which returns updated config)

## Milestones

### Milestone 1: Auto-refresh after validate

**Files**: api/web/admin/app.js

**Acceptance Criteria**:

- After pressing validate button with valid JSON, UI refreshes with current server config
- Pattern matches existing saveConfig and handleFileSelect implementations

#### Code Changes

**CC-M-001-001** (api/web/admin/app.js) - implements CI-M-001-001

**Code:**

```diff
@@ -322,12 +322,13 @@ const App = {
     // Validate config via API
     async validateConfig() {
         this.collectFormChanges();
         const response = await window.APIClient.post('/config/validate', this.buildConfigPayload());
         // 501 = JSON syntax valid, full validation requires PUT
         // 400 = JSON syntax error
         if (response.ok || response.status === 501) {
             this.showMessage('JSON syntax is valid', 'success');
+            await this.loadConfig(); // Refresh from server
         } else {
             this.showMessage('Validation failed: ' + response.error, 'error');
         }
     },
```

**Documentation:**

```diff
--- a/api/web/admin/app.js
+++ b/api/web/admin/app.js
@@ -320,6 +320,8 @@ const App = {
     },

+    // Refreshes config from server after validation to show current server state.
+    // Matches refresh pattern used in saveConfig and handleFileSelect. (ref: DL-001)
     // Validate config via API
     async validateConfig() {

```

## Testing

Manual browser testing:
1. Load admin UI
2. Make config changes
3. Press validate button
4. Verify UI shows current server state after validation succeeds
