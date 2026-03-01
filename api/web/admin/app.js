// Main app module: config editor with CRUD operations.
// Vanilla JS SPA (no framework per DL-002).
//
// XSS prevention: all user input escaped via textContent (ref: RSK-001)
// No innerHTML used for user-provided content

const App = {
    config: null,
    servers: [],

    // Initialize app on page load
    init() {
        this.bindEvents();
        this.checkAuth();
    },

    // Bind all event handlers
    bindEvents() {
        // Login form
        document.getElementById('login-form').addEventListener('submit', (e) => {
            e.preventDefault();
            this.handleLogin();
        });

        // Logout button
        document.getElementById('logout-btn').addEventListener('click', () => {
            this.handleLogout();
        });

        // Add server button
        document.getElementById('add-server-btn').addEventListener('click', () => {
            this.addServer();
        });

        // Add emoji button
        document.getElementById('add-emoji-btn').addEventListener('click', () => {
            this.addEmojiRow();
        });

        // Validate button
        document.getElementById('validate-btn').addEventListener('click', () => {
            this.validateConfig();
        });

        // Save button
        document.getElementById('save-btn').addEventListener('click', () => {
            this.saveConfig();
        });
    },

    // Check auth state and show appropriate screen
    async checkAuth() {
        // Check if behind proxy (proxy handles authentication)
        const proxyMode = await window.Auth.checkProxyMode();
        if (proxyMode) {
            // Proxy mode: auto-authenticate
            await this.showConfigScreen();
            return;
        }

        if (window.Auth.isAuthenticated()) {
            this.showConfigScreen();
        } else {
            this.showLoginScreen();
        }
    },

    // Handle login form submission
    async handleLogin() {
        const tokenInput = document.getElementById('token-input');
        const errorEl = document.getElementById('login-error');
        const token = tokenInput.value.trim();

        errorEl.classList.add('hidden');

        const result = await window.Auth.login(token);
        if (result.success) {
            this.showConfigScreen();
        } else {
            errorEl.textContent = result.error;
            errorEl.classList.remove('hidden');
        }
    },

    // Handle logout
    handleLogout() {
        window.Auth.logout();
        this.showLoginScreen();
    },

    // Show login screen
    showLoginScreen() {
        document.getElementById('login-screen').classList.remove('hidden');
        document.getElementById('config-screen').classList.add('hidden');
        document.getElementById('token-input').value = '';
    },

    // Show config screen and load data
    async showConfigScreen() {
        document.getElementById('login-screen').classList.add('hidden');
        document.getElementById('config-screen').classList.remove('hidden');
        await this.loadConfig();
    },

    // Load config from API
    async loadConfig() {
        const response = await window.APIClient.get('/config');
        if (response.ok) {
            this.config = response.data;
            this.servers = response.data.servers || [];
            this.renderConfig();
        } else {
            this.showMessage('Failed to load config: ' + response.error, 'error');
        }
    },

    // Render config to UI
    // Populates server list and settings fields: server_ip, update_interval,
    // category_order, category_emojis (ref: DL-002).
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
        document.getElementById('server-ip-input').value = this.config.server_ip || '';
        document.getElementById('update-interval-input').value = this.config.update_interval || 30;
        document.getElementById('category-order-input').value = (this.config.category_order || []).join(', ');
        this.renderCategoryEmojis();
    },

    // Populate category dropdown with options from category_order
    populateCategoryDropdown(select, selectedCategory) {
        select.innerHTML = '';
        const categories = this.config.category_order || [];
        categories.forEach(cat => {
            const option = document.createElement('option');
            option.value = cat;
            option.textContent = cat;
            if (cat === selectedCategory) {
                option.selected = true;
            }
            select.appendChild(option);
        });
    },

    // Render category emojis editor
    renderCategoryEmojis() {
        const container = document.getElementById('category-emojis-list');
        container.innerHTML = '';
        const emojis = this.config.category_emojis || {};
        Object.entries(emojis).forEach(([category, emoji]) => {
            this.addEmojiRow(category, emoji);
        });
    },

    // Add a row to the emoji editor
    addEmojiRow(category = '', emoji = '') {
        const container = document.getElementById('category-emojis-list');
        const row = document.createElement('div');
        row.className = 'emoji-row';

        const catInput = document.createElement('input');
        catInput.type = 'text';
        catInput.className = 'emoji-category-input';
        catInput.placeholder = 'Category';
        catInput.value = category;

        const emojiInput = document.createElement('input');
        emojiInput.type = 'text';
        emojiInput.className = 'emoji-value-input';
        emojiInput.placeholder = 'Emoji';
        emojiInput.value = emoji;

        const deleteBtn = document.createElement('button');
        deleteBtn.type = 'button';
        deleteBtn.className = 'delete-emoji-btn';
        deleteBtn.textContent = 'X';
        deleteBtn.addEventListener('click', () => row.remove());

        row.appendChild(catInput);
        row.appendChild(emojiInput);
        row.appendChild(deleteBtn);
        container.appendChild(row);
    },

    // Create server editor element
    // Uses DOM APIs instead of innerHTML for XSS prevention (ref: DL-004).
    // Fields: name (text), port (number 1-65535), category (dropdown).
    // Category dropdown populated from category_order to ensure valid values (ref: DL-003).
    createServerElement(server, index) {
        const div = document.createElement('div');
        div.className = 'server-item';

        const nameGroup = document.createElement('div');
        nameGroup.className = 'form-group';
        const nameLabel = document.createElement('label');
        nameLabel.textContent = 'Name';
        const nameInput = document.createElement('input');
        nameInput.type = 'text';
        nameInput.dataset.field = 'name';
        nameInput.value = server.name || '';
        nameGroup.appendChild(nameLabel);
        nameGroup.appendChild(nameInput);

        const portGroup = document.createElement('div');
        portGroup.className = 'form-group';
        const portLabel = document.createElement('label');
        portLabel.textContent = 'Port';
        const portInput = document.createElement('input');
        portInput.type = 'number';
        portInput.min = '1';
        portInput.max = '65535';
        portInput.dataset.field = 'port';
        portInput.value = server.port || '';
        portGroup.appendChild(portLabel);
        portGroup.appendChild(portInput);

        const categoryGroup = document.createElement('div');
        categoryGroup.className = 'form-group';
        const categoryLabel = document.createElement('label');
        categoryLabel.textContent = 'Category';
        const categorySelect = document.createElement('select');
        categorySelect.dataset.field = 'category';
        this.populateCategoryDropdown(categorySelect, server.category);
        categoryGroup.appendChild(categoryLabel);
        categoryGroup.appendChild(categorySelect);

        const deleteBtn = document.createElement('button');
        deleteBtn.type = 'button';
        deleteBtn.className = 'delete-server-btn';
        deleteBtn.textContent = 'Delete';

        div.appendChild(nameGroup);
        div.appendChild(portGroup);
        div.appendChild(categoryGroup);
        div.appendChild(deleteBtn);

        // Bind delete handler
        deleteBtn.addEventListener('click', () => {
            this.deleteServer(index);
        });

        // Bind input handlers
        nameInput.addEventListener('change', (e) => {
            this.updateServer(index, 'name', e.target.value);
        });
        portInput.addEventListener('change', (e) => {
            this.updateServer(index, 'port', parseInt(e.target.value, 10) || 0);
        });
        categorySelect.addEventListener('change', (e) => {
            this.updateServer(index, 'category', e.target.value);
        });

        return div;
    },

    // Escape HTML to prevent XSS
    // Uses textContent/innerHTML round-trip for <, >, & escaping,
    // plus manual quote escaping for attribute context safety
    // All user-provided content rendered through this method
    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML
            .replace(/"/g, '&quot;')
            .replace(/'/g, '&#39;');
    },

    // Add new server
    // Creates server with default values matching Server struct (ref: DL-001).
    addServer() {
        this.servers.push({ name: '', port: 0, category: '' });
        this.renderConfig();
    },

    // Delete server
    deleteServer(index) {
        this.servers.splice(index, 1);
        this.renderConfig();
    },

    // Update server field
    updateServer(index, field, value) {
        if (this.servers[index]) {
            this.servers[index][field] = value;
        }
    },

    // Validate config via API
    async validateConfig() {
        this.collectFormChanges();
        const response = await window.APIClient.post('/config/validate', this.buildConfigPayload());
        if (response.ok) {
            this.showMessage('Configuration is valid', 'success');
        } else {
            this.showMessage('Validation failed: ' + response.error, 'error');
        }
    },

    // Save config via API
    async saveConfig() {
        this.collectFormChanges();
        const response = await window.APIClient.put('/config', this.buildConfigPayload());
        if (response.ok) {
            this.showMessage('Configuration saved', 'success');
            await this.loadConfig(); // Refresh from server
        } else {
            this.showMessage('Failed to save: ' + response.error, 'error');
        }
    },

    // Collect form changes into config object
    // Gathers all config fields: server_ip, update_interval, category_order,
    // category_emojis, and servers array (ref: DL-002).
    collectFormChanges() {
        this.config.server_ip = document.getElementById('server-ip-input').value.trim();
        this.config.update_interval = parseInt(document.getElementById('update-interval-input').value, 10) || 30;

        const orderValue = document.getElementById('category-order-input').value.trim();
        this.config.category_order = orderValue
            ? orderValue.split(',').map(s => s.trim()).filter(s => s)
            : [];

        this.config.category_emojis = this.collectCategoryEmojis();
        this.config.servers = this.servers;
    },

    // Collect category emojis from the editor
    collectCategoryEmojis() {
        const emojis = {};
        const rows = document.querySelectorAll('#category-emojis-list .emoji-row');
        rows.forEach(row => {
            const cat = row.querySelector('.emoji-category-input').value.trim();
            const emoji = row.querySelector('.emoji-value-input').value.trim();
            if (cat && emoji) {
                emojis[cat] = emoji;
            }
        });
        return emojis;
    },

    // Build config payload for API
    buildConfigPayload() {
        return {
            server_ip: this.config.server_ip,
            update_interval: this.config.update_interval,
            category_order: this.config.category_order,
            category_emojis: this.config.category_emojis,
            servers: this.servers
        };
    },

    // Show status message
    showMessage(text, type) {
        const el = document.getElementById('status-message');
        el.textContent = text;
        el.className = type; // 'success' or 'error'
        setTimeout(() => el.classList.add('hidden'), 5000);
    }
};

// Initialize on DOM ready
document.addEventListener('DOMContentLoaded', () => App.init());
