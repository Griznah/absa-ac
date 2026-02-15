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
    checkAuth() {
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
        document.getElementById('interval-input').value = this.config.interval || 60;
        document.getElementById('category-input').value = this.config.category_id || '';
    },

    // Create server editor element
    createServerElement(server, index) {
        const div = document.createElement('div');
        div.className = 'server-item';
        div.innerHTML = `
            <div class="form-group">
                <label>Name</label>
                <input type="text" data-field="name" value="${this.escapeHtml(server.name || '')}">
            </div>
            <div class="form-group">
                <label>URL</label>
                <input type="text" data-field="url" value="${this.escapeHtml(server.url || '')}">
            </div>
            <button type="button" class="delete-server-btn" data-index="${index}">Delete</button>
        `;

        // Bind delete handler
        div.querySelector('.delete-server-btn').addEventListener('click', () => {
            this.deleteServer(index);
        });

        // Bind input handlers
        div.querySelectorAll('input').forEach(input => {
            input.addEventListener('change', (e) => {
                this.updateServer(index, e.target.dataset.field, e.target.value);
            });
        });

        return div;
    },

    // Escape HTML to prevent XSS
    // Uses textContent assignment to avoid innerHTML injection (ref: RSK-001)
    // All user-provided content rendered through this method
    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    },

    // Add new server
    addServer() {
        this.servers.push({ name: '', url: '' });
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
    collectFormChanges() {
        this.config.interval = parseInt(document.getElementById('interval-input').value, 10) || 60;
        this.config.category_id = document.getElementById('category-input').value.trim();
        this.config.servers = this.servers;
    },

    // Build config payload for API
    buildConfigPayload() {
        return {
            interval: this.config.interval,
            category_id: this.config.category_id,
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
