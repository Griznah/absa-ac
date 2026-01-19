// Alpine.js application component for config editor
function app() {
    return {
        token: null,
        inputToken: '',
        csrfToken: null,
        config: {
            server_ip: '',
            update_interval: 30,
            category_order: [],
            category_emojis: {},
            servers: []
        },
        dirty: false,
        saved: false,
        error: '',
        remoteChanged: false,
        pollingInterval: null,
        pollBackoffInterval: 30000, // Start with 30s
        isPollingRestart: false,

        init() {
            const storedToken = sessionStorage.getItem('apiToken');
            if (storedToken) {
                this.token = storedToken;
                this.fetchCSRFToken().then(() => {
                    this.fetchConfig();
                    this.startPolling();
                });
            }

            this.$watch('config', () => {
                if (this.dirty === false || this.dirty === 'remote') {
                    this.dirty = 'local';
                }
                this.saved = false;
            }, { deep: true });
        },

        login() {
            if (this.inputToken.trim()) {
                this.token = this.inputToken.trim();
                sessionStorage.setItem('apiToken', this.token);
                this.inputToken = '';
                this.fetchCSRFToken().then(() => {
                    this.fetchConfig();
                    this.startPolling();
                });
            }
        },

        async fetchCSRFToken() {
            try {
                const response = await this.apiRequest('GET', '/api/csrf-token');
                this.csrfToken = response.csrf_token;
            } catch (err) {
                this.error = 'Failed to fetch CSRF token: ' + err.message;
                throw err;
            }
        },

        async fetchConfig() {
            try {
                const response = await this.apiRequest('GET', '/api/config');
                // Polling skips config update when dirty flag is set; user's unsaved edits take precedence over remote changes to prevent data loss.
                // Note: apiRequest unwraps response.data, so response is already the data object
                if (this.dirty === false) {
                    this.config = response;
                    this.dirty = 'remote';
                } else if (this.dirty === 'local') {
                    // Remote changed while user is editing - show warning indicator
                    this.remoteChanged = true;
                }
                // Reset backoff on successful fetch
                this.pollBackoffInterval = 30000;
                this.startPolling(); // Restart with normal interval
            } catch (err) {
                this.error = 'Failed to fetch config: ' + err.message;
                // Exponential backoff: double interval up to max 300s (5 minutes) with jitter
                this.pollBackoffInterval = Math.min(this.pollBackoffInterval * 2, 300000) + Math.random() * 5000;
                this.startPolling(); // Restart with backoff interval
            }
        },

        async save() {
            this.error = '';

            for (const server of this.config.servers) {
                if (!server.name.trim()) {
                    this.error = 'Server name cannot be empty';
                    return;
                }
                if (server.port < 1 || server.port > 65535) {
                    this.error = `Invalid port: ${server.port} (valid range: 1-65535)`;
                    return;
                }
                if (!this.config.category_order.includes(server.category)) {
                    this.error = `Invalid category: ${server.category}`;
                    return;
                }
            }

            if (!this.config.server_ip.trim()) {
                this.error = 'Server IP cannot be empty';
                return;
            }

            if (this.config.update_interval < 1) {
                this.error = 'Update interval must be at least 1 second';
                return;
            }

            try {
                await this.apiRequest('PATCH', '/api/config', this.config);
                this.dirty = false;
                this.remoteChanged = false;
                this.saved = true;
                setTimeout(() => {
                    this.saved = false;
                }, 3000);
                // Refetch config after save to ensure UI matches server state
                this.fetchConfig();
            } catch (err) {
                this.error = err.message;
            }
        },

        addServer() {
            this.config.servers.push({
                name: '',
                port: 8081,
                category: this.config.category_order[0] || ''
            });
        },

        removeServer(index) {
            this.config.servers.splice(index, 1);
        },

        startPolling() {
            // Guard: prevent concurrent polling restart operations
            if (this.isPollingRestart) {
                return;
            }
            this.isPollingRestart = true;

            if (this.pollingInterval) {
                clearInterval(this.pollingInterval);
            }
            // Use pollBackoffInterval (starts at 30s, increases on errors)
            this.pollingInterval = setInterval(() => {
                this.fetchConfig();
            }, this.pollBackoffInterval);

            // Clear guard after interval is established
            setTimeout(() => {
                this.isPollingRestart = false;
            }, 100);
        },

        async apiRequest(method, url, data) {
            const options = {
                method: method,
                headers: {
                    'Content-Type': 'application/json',
                    'Authorization': `Bearer ${this.token}`
                }
            };

            // Add CSRF token for state-changing methods (PATCH, PUT, POST, DELETE)
            const stateChangingMethods = ['PATCH', 'PUT', 'POST', 'DELETE'];
            if (stateChangingMethods.includes(method)) {
                if (!this.csrfToken) {
                    throw new Error('CSRF token not loaded. Refresh the page.');
                }
                options.headers['X-CSRF-Token'] = this.csrfToken;
            }

            if (data) {
                options.body = JSON.stringify(data);
            }

            const response = await fetch(url, options);

            if (response.status === 401) {
                this.token = null;
                sessionStorage.removeItem('apiToken');
                if (this.pollingInterval) {
                    clearInterval(this.pollingInterval);
                    this.pollingInterval = null;
                }
                throw new Error('Unauthorized - please login again');
            }

            // Handle 403 Forbidden (could be CSRF validation failure)
            if (response.status === 403) {
                const errorData = await response.json().catch(() => ({}));
                if (errorData.error && errorData.error.includes('CSRF')) {
                    // CSRF token invalid - fetch new token and retry
                    await this.fetchCSRFToken();
                    // Retry the original request with new CSRF token
                    options.headers['X-CSRF-Token'] = this.csrfToken;
                    const retryResponse = await fetch(url, options);
                    if (!retryResponse.ok) {
                        const retryErrorData = await retryResponse.json().catch(() => ({}));
                        throw new Error(retryErrorData.error || retryErrorData.details || 'Request failed after retry');
                    }
                    return retryResponse.json().then(data => data.data);
                }
                throw new Error(errorData.error || errorData.details || 'Request failed');
            }

            if (!response.ok) {
                const errorData = await response.json();
                throw new Error(errorData.error || errorData.details || 'Request failed');
            }

            // Unwrap API response: API returns {data: {...}} wrapper
            return response.json().then(data => data.data);
        }
    };
}
