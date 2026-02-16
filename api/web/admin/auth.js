// Auth module: login/logout flow with token management.
// Token stored in sessionStorage (cleared on tab close for XSS protection).
// CSRF token fetched following successful auth for state-changing requests (ref: DL-003, DL-004).
//
// Security considerations:
// - sessionStorage limits token exposure to single tab (ref: DL-003)
// - Token format validated locally preceding API call (32+ chars required)
// - CSRF token required for all POST/PATCH/PUT/DELETE requests (ref: DL-004)

const Auth = {
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

    // Verify token against API health endpoint
    async verifyToken(token) {
        try {
            const response = await fetch('/health', {
                headers: { 'Authorization': `Bearer ${token}` }
            });
            return response.ok;
        } catch {
            return false;
        }
    },

    // Store token in sessionStorage
    setToken(token) {
        sessionStorage.setItem('bearerToken', token);
    },

    // Retrieve token from sessionStorage
    getToken() {
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

    // Check if user is authenticated (has valid token in storage)
    isAuthenticated() {
        return !!this.getToken();
    },

    // Clear all auth data (logout)
    logout() {
        sessionStorage.removeItem('bearerToken');
        sessionStorage.removeItem('csrfToken');
    },

    // Full login flow: validate format, verify with API, fetch CSRF token
    async login(token) {
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
