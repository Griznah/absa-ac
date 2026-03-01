# Plan

## Overview

Current admin UI only edits name and url fields, but url does not exist in Server struct. Server has name, port, category. Global config has server_ip, update_interval, category_order, category_emojis that are not editable.

**Approach**: Align frontend fields with actual Server struct (name, port, category) and add global config editing. Category uses dropdown populated from category_order to prevent validation errors.

## Planning Context

### Decision Log

| ID | Decision | Reasoning Chain |
|---|---|---|
| DL-001 | Align UI fields with actual Server struct: name, port, category (remove url) | Current UI has url field that does not exist in Server struct -> Server has Name, Port, Category per main.go:L172-L177 -> UI must match struct to send valid payloads |
| DL-002 | Add global config fields: server_ip, update_interval, category_order, category_emojis | config.json.example shows these top-level fields -> Backend validates them in validateConfigStructSafeRuntime -> UI should allow editing all config fields for completeness |
| DL-003 | Category dropdown populated from category_order, stored as category string | Server.category must exist in category_order per validation -> Dropdown prevents invalid categories -> Simpler UX than free-text input |
| DL-004 | Use existing XSS prevention pattern (escapeHtml via textContent) | RSK-001 established escapeHtml pattern -> Continue using textContent/innerHTML round-trip for all user content |

### Constraints

- MUST: Keep it simple and to the point
- MUST: Match actual Server struct fields (name, port, category)
- SHOULD: Support global config fields (server_ip, category_order, category_emojis)

## Invisible Knowledge

### System

Vanilla JS SPA with no build chain (DL-002). Bearer token in sessionStorage. XSS prevention via escapeHtml using textContent/innerHTML round-trip.

### Invariants

- All user content rendered through escapeHtml function
- Server struct fields: Name, IP, Port, Category (IP set globally from server_ip)
- Config validation requires category in category_order and emoji in category_emojis

### Tradeoffs

- Category dropdown limits to existing categories (cannot add new category inline)
- No client-side port range validation (relies on backend validation)

## Milestones

### Milestone 1: Update server editor to match Server struct

**Files**: api/web/admin/app.js, api/web/admin/index.html, api/web/admin/styles.css

**Requirements**:

- Server editor must have name (text), port (number 1-65535), category (dropdown from category_order)
- Settings section must have server_ip (text), update_interval (number), category_order (text), category_emojis (key-value editor)
- All form fields must use escapeHtml for XSS prevention

**Acceptance Criteria**:

- Can edit server name, port, category and save successfully
- Port validation rejects values outside 1-65535
- Category dropdown only shows categories from category_order
- Can edit server_ip, update_interval, category_order, category_emojis
- PUT /api/config with edited config passes backend validation

#### Code Intent

- **CI-M-001-001** `api/web/admin/app.js`: Replace createServerElement to use name, port, category fields instead of name, url. Port is number input, category is dropdown from category_order. (refs: DL-001, DL-003)
- **CI-M-001-002** `api/web/admin/app.js`: Update renderConfig to populate settings section with server_ip, update_interval, category_order, category_emojis from config (refs: DL-002)
- **CI-M-001-003** `api/web/admin/app.js`: Update collectFormChanges to read all new form fields and build complete config payload matching Config struct (refs: DL-002)
- **CI-M-001-004** `api/web/admin/app.js`: Update addServer to create server with empty name, port 0, empty category instead of name, url (refs: DL-001)
- **CI-M-001-005** `api/web/admin/index.html`: Replace Settings section: remove interval-input and category-input, add server_ip (text input), update_interval (number input), category_order (comma-separated text input), and category_emojis key-value editor. The category_emojis editor uses a container of row divs where each row has: category name text input, emoji text input, and delete button. An 'Add Emoji' button appends new empty rows. (refs: DL-002)
- **CI-M-001-006** `api/web/admin/index.html`: Update servers-list section to use port (number) and category (dropdown) instead of url (refs: DL-001, DL-003)
- **CI-M-001-007** `api/web/admin/styles.css`: Add styles for category dropdown select element and category emoji editor grid (refs: DL-003)

#### Code Changes

**CC-M-001-001** (api/web/admin/app.js) - implements CI-M-001-001

**Code:**

```diff
--- a/api/web/admin/app.js
+++ b/api/web/admin/app.js
@@ -124,30 +124,56 @@
     // Create server editor element
     createServerElement(server, index) {
         const div = document.createElement('div');
         div.className = 'server-item';
-        div.innerHTML = `
-            <div class="form-group">
-                <label>Name</label>
-                <input type="text" data-field="name" value="${this.escapeHtml(server.name || '')}">
-            </div>
-            <div class="form-group">
-                <label>URL</label>
-                <input type="text" data-field="url" value="${this.escapeHtml(server.url || '')}">
-            </div>
-            <button type="button" class="delete-server-btn" data-index="${index}">Delete</button>
-        `;
+
+        const nameGroup = document.createElement('div');
+        nameGroup.className = 'form-group';
+        const nameLabel = document.createElement('label');
+        nameLabel.textContent = 'Name';
+        const nameInput = document.createElement('input');
+        nameInput.type = 'text';
+        nameInput.dataset.field = 'name';
+        nameInput.value = server.name || '';
+        nameGroup.appendChild(nameLabel);
+        nameGroup.appendChild(nameInput);
+
+        const portGroup = document.createElement('div');
+        portGroup.className = 'form-group';
+        const portLabel = document.createElement('label');
+        portLabel.textContent = 'Port';
+        const portInput = document.createElement('input');
+        portInput.type = 'number';
+        portInput.min = '1';
+        portInput.max = '65535';
+        portInput.dataset.field = 'port';
+        portInput.value = server.port || '';
+        portGroup.appendChild(portLabel);
+        portGroup.appendChild(portInput);
+
+        const categoryGroup = document.createElement('div');
+        categoryGroup.className = 'form-group';
+        const categoryLabel = document.createElement('label');
+        categoryLabel.textContent = 'Category';
+        const categorySelect = document.createElement('select');
+        categorySelect.dataset.field = 'category';
+        this.populateCategoryDropdown(categorySelect, server.category);
+        categoryGroup.appendChild(categoryLabel);
+        categoryGroup.appendChild(categorySelect);
+
+        const deleteBtn = document.createElement('button');
+        deleteBtn.type = 'button';
+        deleteBtn.className = 'delete-server-btn';
+        deleteBtn.textContent = 'Delete';
+
+        div.appendChild(nameGroup);
+        div.appendChild(portGroup);
+        div.appendChild(categoryGroup);
+        div.appendChild(deleteBtn);

         // Bind delete handler
-        div.querySelector('.delete-server-btn').addEventListener('click', () => {
+        deleteBtn.addEventListener('click', () => {
             this.deleteServer(index);
         });

         // Bind input handlers
-        div.querySelectorAll('input').forEach(input => {
-            input.addEventListener('change', (e) => {
-                this.updateServer(index, e.target.dataset.field, e.target.value);
-            });
+        nameInput.addEventListener('change', (e) => {
+            this.updateServer(index, 'name', e.target.value);
+        });
+        portInput.addEventListener('change', (e) => {
+            this.updateServer(index, 'port', parseInt(e.target.value, 10) || 0);
+        });
+        categorySelect.addEventListener('change', (e) => {
+            this.updateServer(index, 'category', e.target.value);
         });

         return div;
-    },
+    }
```

**Documentation:**

```diff
--- a/api/web/admin/app.js
+++ b/api/web/admin/app.js
@@ -124,6 +124,11 @@
     // Create server editor element
+    // Uses DOM APIs instead of innerHTML for XSS prevention (ref: DL-004).
+    // Fields: name (text), port (number 1-65535), category (dropdown).
+    // Category dropdown populated from category_order to ensure valid values (ref: DL-003).
     createServerElement(server, index) {

```


**CC-M-001-002** (api/web/admin/app.js) - implements CI-M-001-002

**Code:**

```diff
--- a/api/web/admin/app.js
+++ b/api/web/admin/app.js
@@ -105,18 +105,62 @@
     // Render config to UI
     renderConfig() {
         // Render servers list
         const serversList = document.getElementById('servers-list');
         if (!serversList) {
             this.showMessage('UI error: servers list not found', 'error');
             return;
         }
         serversList.innerHTML = '';

         this.servers.forEach((server, index) => {
             const serverEl = this.createServerElement(server, index);
             serversList.appendChild(serverEl);
         });

         // Render settings
-        document.getElementById('interval-input').value = this.config.interval || 60;
-        document.getElementById('category-input').value = this.config.category_id || '';
+        document.getElementById('server-ip-input').value = this.config.server_ip || '';
+        document.getElementById('update-interval-input').value = this.config.update_interval || 30;
+        document.getElementById('category-order-input').value = (this.config.category_order || []).join(', ');
+        this.renderCategoryEmojis();
+    },
+
+    // Populate category dropdown with options from category_order
+    populateCategoryDropdown(select, selectedCategory) {
+        select.innerHTML = '';
+        const categories = this.config.category_order || [];
+        categories.forEach(cat => {
+            const option = document.createElement('option');
+            option.value = cat;
+            option.textContent = cat;
+            if (cat === selectedCategory) {
+                option.selected = true;
+            }
+            select.appendChild(option);
+        });
+    },
+
+    // Render category emojis editor
+    renderCategoryEmojis() {
+        const container = document.getElementById('category-emojis-list');
+        container.innerHTML = '';
+        const emojis = this.config.category_emojis || {};
+        Object.entries(emojis).forEach(([category, emoji]) => {
+            this.addEmojiRow(category, emoji);
+        });
+    },
+
+    // Add a row to the emoji editor
+    addEmojiRow(category = '', emoji = '') {
+        const container = document.getElementById('category-emojis-list');
+        const row = document.createElement('div');
+        row.className = 'emoji-row';
+
+        const catInput = document.createElement('input');
+        catInput.type = 'text';
+        catInput.className = 'emoji-category-input';
+        catInput.placeholder = 'Category';
+        catInput.value = category;
+
+        const emojiInput = document.createElement('input');
+        emojiInput.type = 'text';
+        emojiInput.className = 'emoji-value-input';
+        emojiInput.placeholder = 'Emoji';
+        emojiInput.value = emoji;
+
+        const deleteBtn = document.createElement('button');
+        deleteBtn.type = 'button';
+        deleteBtn.className = 'delete-emoji-btn';
+        deleteBtn.textContent = 'X';
+        deleteBtn.addEventListener('click', () => row.remove());
+
+        row.appendChild(catInput);
+        row.appendChild(emojiInput);
+        row.appendChild(deleteBtn);
+        container.appendChild(row);
     },
```

**Documentation:**

```diff
--- a/api/web/admin/app.js
+++ b/api/web/admin/app.js
@@ -104,6 +104,10 @@
     // Render config to UI
+    // Populates server list and settings fields: server_ip, update_interval,
+    // category_order, category_emojis (ref: DL-002).
     renderConfig() {

```


**CC-M-001-003** (api/web/admin/app.js) - implements CI-M-001-003

**Code:**

```diff
--- a/api/web/admin/app.js
+++ b/api/web/admin/app.js
@@ -209,22 +209,42 @@
     // Collect form changes into config object
     collectFormChanges() {
-        this.config.interval = parseInt(document.getElementById('interval-input').value, 10) || 60;
-        this.config.category_id = document.getElementById('category-input').value.trim();
+        this.config.server_ip = document.getElementById('server-ip-input').value.trim();
+        this.config.update_interval = parseInt(document.getElementById('update-interval-input').value, 10) || 30;
+
+        const orderValue = document.getElementById('category-order-input').value.trim();
+        this.config.category_order = orderValue
+            ? orderValue.split(',').map(s => s.trim()).filter(s => s)
+            : [];
+
+        this.config.category_emojis = this.collectCategoryEmojis();
         this.config.servers = this.servers;
     },

+    // Collect category emojis from the editor
+    collectCategoryEmojis() {
+        const emojis = {};
+        const rows = document.querySelectorAll('#category-emojis-list .emoji-row');
+        rows.forEach(row => {
+            const cat = row.querySelector('.emoji-category-input').value.trim();
+            const emoji = row.querySelector('.emoji-value-input').value.trim();
+            if (cat && emoji) {
+                emojis[cat] = emoji;
+            }
+        });
+        return emojis;
+    },
+
     // Build config payload for API
     buildConfigPayload() {
         return {
-            interval: this.config.interval,
-            category_id: this.config.category_id,
+            server_ip: this.config.server_ip,
+            update_interval: this.config.update_interval,
+            category_order: this.config.category_order,
+            category_emojis: this.config.category_emojis,
             servers: this.servers
         };
     },
```

**Documentation:**

```diff
--- a/api/web/admin/app.js
@@ -209,6 +209,11 @@
     // Collect form changes into config object
+    // Gathers all config fields: server_ip, update_interval, category_order,
+    // category_emojis, and servers array (ref: DL-002).
     collectFormChanges() {

```


**CC-M-001-004** (api/web/admin/app.js) - implements CI-M-001-004

**Code:**

```diff
--- a/api/web/admin/app.js
+++ b/api/web/admin/app.js
@@ -167,7 +167,7 @@
     // Add new server
     addServer() {
-        this.servers.push({ name: '', url: '' });
+        this.servers.push({ name: '', port: 0, category: '' });
         this.renderConfig();
     },
```

**Documentation:**

```diff
--- a/api/web/admin/app.js
@@ -167,6 +167,9 @@
     // Add new server
+    // Creates server with default values matching Server struct (ref: DL-001).
     addServer() {

```


**CC-M-001-005** (api/web/admin/index.html) - implements CI-M-001-005

**Code:**

```diff
--- a/api/web/admin/index.html
+++ b/api/web/admin/index.html
@@ -46,13 +46,26 @@
                 <!-- General Settings -->
                 <section class="config-section">
                     <h2>Settings</h2>
                     <div class="form-group">
-                        <label for="interval-input">Update Interval (seconds)</label>
-                        <input type="number" id="interval-input" min="10">
+                        <label for="server-ip-input">Server IP</label>
+                        <input type="text" id="server-ip-input" placeholder="e.g. 192.168.1.100">
                     </div>
                     <div class="form-group">
-                        <label for="category-input">Category ID</label>
-                        <input type="text" id="category-input" placeholder="Discord category ID">
+                        <label for="update-interval-input">Update Interval (seconds)</label>
+                        <input type="number" id="update-interval-input" min="10">
+                    </div>
+                    <div class="form-group">
+                        <label for="category-order-input">Category Order (comma-separated)</label>
+                        <input type="text" id="category-order-input" placeholder="Drift, Touge, Track">
+                    </div>
+                    <div class="form-group">
+                        <label>Category Emojis</label>
+                        <div id="category-emojis-list"></div>
+                        <button type="button" id="add-emoji-btn">Add Emoji</button>
                     </div>
                 </section>
```

**Documentation:**

```diff
--- a/api/web/admin/index.html
+++ b/api/web/admin/index.html
@@ -46,6 +46,10 @@
                 <!-- General Settings -->
                 <section class="config-section">
+                    <!-- Global config fields: server_ip, update_interval, category_order, category_emojis (ref: DL-002) -->
+                    <!-- category_emojis uses key-value editor for category -> emoji mappings -->
                     <h2>Settings</h2>

```


**CC-M-001-006** (api/web/admin/index.html) - implements CI-M-001-006

**Code:**

```diff
--- a/api/web/admin/index.html
+++ b/api/web/admin/index.html
@@ -39,6 +39,7 @@
             <main id="config-editor">
                 <!-- Servers Section -->
                 <section class="config-section">
                     <h2>Servers</h2>
-                    <div id="servers-list"></div>
+                    <div id="servers-list" class="servers-grid"></div>
                     <button id="add-server-btn">Add Server</button>
                 </section>
```

**Documentation:**

```diff
--- a/api/web/admin/index.html
+++ b/api/web/admin/index.html
@@ -39,6 +39,9 @@
                 <!-- Servers Section -->
                 <section class="config-section">
+                    <!-- Server fields: name, port (1-65535), category (from category_order) (ref: DL-001, DL-003) -->
                     <h2>Servers</h2>

```


**CC-M-001-007** (api/web/admin/styles.css) - implements CI-M-001-007

**Code:**

```diff
--- a/api/web/admin/styles.css
+++ b/api/web/admin/styles.css
@@ -163,6 +163,50 @@
     background: #c82333;
 }

+/* Server editor dropdown */
+.server-item select {
+    padding: 0.75rem;
+    border: 1px solid var(--border);
+    border-radius: 4px;
+    background: var(--bg-secondary);
+    color: var(--text-primary);
+    font-size: 1rem;
+    width: 100%;
+    cursor: pointer;
+}
+
+.server-item select:focus {
+    outline: none;
+    border-color: var(--accent-hover);
+}
+
+/* Category emoji editor */
+#category-emojis-list {
+    display: flex;
+    flex-direction: column;
+    gap: 0.5rem;
+    margin-bottom: 0.5rem;
+}
+
+.emoji-row {
+    display: flex;
+    gap: 0.5rem;
+    align-items: center;
+}
+
+.emoji-category-input {
+    flex: 1;
+}
+
+.emoji-value-input {
+    width: 80px;
+    text-align: center;
+}
+
+.delete-emoji-btn {
+    padding: 0.5rem 0.75rem;
+    font-size: 0.9rem;
+    background: var(--error);
+    min-width: auto;
+}
+
+#add-emoji-btn {
+    padding: 0.5rem 1rem;
+    font-size: 0.9rem;
+}
+
 #add-server-btn {
     width: 100%;
     margin-top: 0.5rem;
```

**Documentation:**

```diff
--- a/api/web/admin/styles.css
+++ b/api/web/admin/styles.css
@@ -163,6 +163,10 @@
     background: #c82333;
 }

+/* Server editor category dropdown (ref: DL-003) */
+/* Category emoji key-value editor grid (ref: DL-002) */
 .server-item select {

```


**CC-M-001-008** (api/web/admin/app.js) - implements CI-M-001-002

**Code:**

```diff
--- a/api/web/admin/app.js
+++ b/api/web/admin/app.js
@@ -30,6 +30,10 @@
         // Add server button
         document.getElementById('add-server-btn').addEventListener('click', () => {
             this.addServer();
         });
+
+        // Add emoji button
+        document.getElementById('add-emoji-btn').addEventListener('click', () => {
+            this.addEmojiRow();
+        });

         // Validate button
         document.getElementById('validate-btn').addEventListener('click', () => {
```

**Documentation:**

```diff
--- a/api/web/admin/app.js
+++ b/api/web/admin/app.js
@@ -30,6 +30,10 @@
         // Add server button
         document.getElementById('add-server-btn').addEventListener('click', () => {
             this.addServer();
         });
+
+        // Add emoji button for category_emojis editor (ref: DL-002)
+        document.getElementById('add-emoji-btn').addEventListener('click', () => {
+            this.addEmojiRow();
+        });

```


**CC-M-001-009** (api/web/admin/README.md)

**Documentation:**

```diff
--- a/api/web/admin/README.md
+++ b/api/web/admin/README.md
@@ -34,9 +34,16 @@ Both auto-included in API requests via `api.js` wrapper

 ### XSS Prevention

 - All user input escaped via `textContent` (never `innerHTML` for user content)
-- `escapeHtml()` function sanitizes server names/URLs before rendering
+- `escapeHtml()` function sanitizes server names before rendering
 - Strict CSP header (see below)

+Server editor extended to edit all Server struct fields (name, port, category)
+rather than just name and non-existent url field (ref: DL-001).
+Category uses dropdown populated from category_order to ensure validity (ref: DL-003).
+Global config fields (server_ip, update_interval, category_order, category_emojis)
+added to enable full config editing via admin UI (ref: DL-002).

 ### CSRF Protection

 - Token fetched from `/api/csrf-token` after successful login

```


## Execution Waves

- W-001: M-001
