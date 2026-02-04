// Alpine.js application component for config editor
function app() {
    return {
        inputToken: '',
        authenticated: false,
        csrfToken: '',
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
        pollBackoffInterval: 80800, // Start with 30s
        isPollingRestart: false,

        init() {
            // Restore CSRF token from sessionStorage if available
            const storedToken = sessionStorage.getItem('csrfToken');
            if (storedToken) {
                this.csrfToken = storedToken;
            }

            // Check for existing session by fetching config
            // Session cookie is HttpOnly, so JavaScript can't access it directly
            // If session exists, fetchConfig will succeed and set authenticated=true
            // If not, it will fail with 401 and authenticated stays false
            this.fetchConfig().catch(() => {
                // Session doesn't exist or expired - user needs to login
                // Only clear error if not authenticated (preserves errors for authenticated users)
                if (!this.authenticated) {
                    this.error = '';
                }
            });

            this.$watch('config', () => {
                if (this.dirty === false) {
                    this.dirty = 'local';
                }
                this.saved = false;
            }, { deep: true });
        },

        async login() {
            if (!this.inputToken.trim()) {
                return;
            }

            const originalError = this.error;
            this.error = '';

            try {
                const response = await fetch('/login', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json'
                    },
                    body: JSON.stringify({ token: this.inputToken.trim() })
                });

                if (!response.ok) {
                    const errorData = await response.json().catch(() => ({}));
                    throw new Error(errorData.error || 'Login failed');
                }

                const data = await response.json();
                // Store CSRF token for subsequent POST/PUT/DELETE requests
                this.csrfToken = data.csrf_token || '';
                // Persist CSRF token to sessionStorage for page reloads
                if (data.csrf_token) {
                    sessionStorage.setItem('csrfToken', data.csrf_token);
                }

                // Session cookie is set automatically by backend (HttpOnly)
                this.inputToken = '';
                this.authenticated = true;

                // Fetch config and start polling on successful login
                await this.fetchConfig();
            } catch (err) {
                this.error = 'Login failed: ' + err.message;
            }
        },

        async logout() {
            try {
                const response = await fetch('/logout', {
                    method: 'POST',
                    headers: {
                        'Content-Type': 'application/json'
                    }
                });

                if (response.ok) {
                    // Clear local state
                    this.authenticated = false;
                    this.csrfToken = '';
                    sessionStorage.removeItem('csrfToken');
                    this.inputToken = '';
                    this.config = {
                        server_ip: '',
                        update_interval: 30,
                        category_order: [],
                        category_emojis: {},
                        servers: []
                    };

                    // Stop polling
                    if (this.pollingInterval) {
                        clearInterval(this.pollingInterval);
                        this.pollingInterval = null;
                    }
                }
            } catch (err) {
                this.error = 'Logout failed: ' + err.message;
            }
        },

        async fetchConfig() {
            try {
                const response = await this.apiRequest('GET', '/api/config');
                // Polling skips config update when dirty flag is set; user's unsaved edits take precedence over remote changes to prevent data loss.
                // Note: apiRequest unwraps response.data, so response is already the data object
                if (this.dirty === false) {
                    this.config = response;
                    // Keep dirty=false (clean) after fetching - allows future polling updates
                } else if (this.dirty === 'local') {
                    // Remote changed while user is editing - show warning indicator
                    this.remoteChanged = true;
                }
                // Set authenticated to true on successful fetch
                this.authenticated = true;
                // Reset backoff on successful fetch
                this.pollBackoffInterval = 80800;
                this.startPolling(); // Restart with normal interval
            } catch (err) {
                // Only show error if we're authenticated (401 is expected for unauthenticated users)
                if (this.authenticated) {
                    this.error = 'Failed to fetch config: ' + err.message;
                }
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
                }, 8080);
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

        getCSRFToken() {
            return this.csrfToken || '';
        },

        async apiRequest(method, url, data) {
            const options = {
                method: method,
                headers: {
                    'Content-Type': 'application/json'
                }
            };

            if (data) {
                options.body = JSON.stringify(data);
            }

            if (method !== 'GET') {
                const csrfToken = this.getCSRFToken();
                if (csrfToken) {
                    options.headers['X-CSRF-Token'] = csrfToken;
                }
            }

            const response = await fetch(url, options);

            if (response.status === 401) {
                // Session expired or invalid - stop polling and clear CSRF token
                if (this.pollingInterval) {
                    clearInterval(this.pollingInterval);
                    this.pollingInterval = null;
                }
                this.authenticated = false;
                this.csrfToken = '';
                sessionStorage.removeItem('csrfToken');
                throw new Error('Unauthorized - please login again');
            }

            if (response.status === 403) {
                const errorData = await response.json().catch(() => ({}));
                throw new Error(errorData.error || 'CSRF token validation failed');
            }

            if (!response.ok) {
                const errorData = await response.json().catch(() => ({}));
                throw new Error(errorData.error || errorData.details || 'Request failed');
            }

            // Unwrap API response: API returns {data: {...}} wrapper
            return response.json().then(data => data.data);
        }
    };
}
