// Auth module: login/logout flow with token management.
// Token stored in sessionStorage (cleared on tab close for XSS protection).
// CSRF token fetched following successful auth for state-changing requests (ref: DL-003, DL-004).
//
// Security considerations:
// - sessionStorage limits token exposure to single tab (ref: DL-003)
// - Token format validated locally preceding API call (32+ chars required)
// - CSRF token required for all POST/PATCH/PUT/DELETE requests (ref: DL-004)
//
// Proxy mode: When behind reverse proxy with Basic Auth, proxy handles auth
// and injects Bearer token. No token needed in login form.

const Auth = {
    // Check if running behind proxy (proxy handles auth)
    _proxyMode: null,

    // Check if proxy mode is active (no Bearer token needed)
    async checkProxyMode() {
        if (this._proxyMode !== null) {
            return this._proxyMode;
        }
        try {
            // Try to access API without Bearer token
            // If proxy is active, it will inject the token
            const response = await fetch('/api/config');
            // 200 means proxy mode (proxy injected valid token)
            // 401 means direct access (need Bearer token)
            this._proxyMode = response.ok;
            return this._proxyMode;
        } catch {
            this._proxyMode = false;
            return false;
        }
    },

    // Validate token format locally before API call
    // Bearer tokens must be 32+ chars (per API validation)
    validateTokenFormat(token) {
        if (!token || typeof token !== 'string') {
            return { valid: false, error: 'Token is required' };
        }
        if (token.length < 32) {
            return { valid: false, error: 'Token must be at least 32 characters' };
        }
        return { valid: true };
    },

    // Verify token against protected endpoint (not /health which bypasses auth)
    async verifyToken(token) {
        try {
            const response = await fetch('/api/config', {
                headers: { 'Authorization': `Bearer ${token}` }
            });
            // Any response (even 404) proves auth passed; 401 means invalid token
            return response.status !== 401;
        } catch {
            return false;
        }
    },

    // Store token in sessionStorage
    setToken(token) {
        sessionStorage.setItem('bearerToken', token);
    },

    // Retrieve token from sessionStorage
    // Returns 'proxy' string in proxy mode (signals APIClient to skip Bearer header)
    getToken() {
        if (this._proxyMode) {
            return 'proxy'; // Marker for proxy mode
        }
        return sessionStorage.getItem('bearerToken');
    },

    // Store CSRF token
    setCSRFToken(token) {
        sessionStorage.setItem('csrfToken', token);
    },

    // Retrieve CSRF token
    getCSRFToken() {
        return sessionStorage.getItem('csrfToken');
    },

    // Check if user is authenticated (has valid token in storage or proxy mode)
    isAuthenticated() {
        if (this._proxyMode) {
            return true;
        }
        return !!sessionStorage.getItem('bearerToken');
    },

    // Clear all auth data (logout)
    logout() {
        sessionStorage.removeItem('bearerToken');
        sessionStorage.removeItem('csrfToken');
    },

    // Full login flow: validate format, verify with API, fetch CSRF token
    // In proxy mode, skips token validation (proxy handles auth)
    async login(token) {
        // Proxy mode: proxy handles authentication, just need CSRF token
        if (this._proxyMode) {
            const csrfSuccess = await this.fetchCSRFToken();
            if (!csrfSuccess) {
                return { success: false, error: 'Failed to fetch CSRF token' };
            }
            return { success: true };
        }

        // Direct mode: validate and verify Bearer token
        const formatCheck = this.validateTokenFormat(token);
        if (!formatCheck.valid) {
            return { success: false, error: formatCheck.error };
        }

        const isValid = await this.verifyToken(token);
        if (!isValid) {
            return { success: false, error: 'Invalid token or API unavailable' };
        }

        this.setToken(token);

        // Fetch CSRF token following successful authentication (ref: DL-004)
        // CSRF token required for all state-changing requests
        const csrfSuccess = await this.fetchCSRFToken();
        if (!csrfSuccess) {
            // Rollback: clear token on CSRF failure to maintain consistent state
            this.logout();
            return { success: false, error: 'Failed to fetch CSRF token' };
        }

        return { success: true };
    },

    // Fetch CSRF token from API endpoint.
    // Called following successful bearer token validation (ref: DL-004).
    async fetchCSRFToken() {
        try {
            const response = await APIClient.get('/csrf-token');
            if (response.ok && response.data?.csrf_token) {
                this.setCSRFToken(response.data.csrf_token);
                return true;
            }
            return false;
        } catch {
            return false;
        }
    }
};

// Export for use in other modules
window.Auth = Auth;
