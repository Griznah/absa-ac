// API Client module: fetch wrapper with auth and CSRF headers.
// Auto-includes Authorization header (Bearer token) and X-CSRF-Token header.
// CSRF token included for state-changing requests (POST/PATCH/PUT/DELETE).
//
// Headers included (ref: DL-003, DL-004):
// - Authorization: Bearer <token> - all requests
// - X-CSRF-Token: <token> - state-changing requests only

const APIClient = {
    // Base URL for API requests
    baseURL: '/api',

    // Build headers with auth and CSRF tokens
    buildHeaders(includeCSRF = false) {
        const headers = {
            'Content-Type': 'application/json'
        };

        const bearerToken = window.Auth?.getToken();
        // In proxy mode, token is 'proxy' - don't send Bearer header
        // Proxy injects the token when forwarding
        if (bearerToken && bearerToken !== 'proxy') {
            headers['Authorization'] = `Bearer ${bearerToken}`;
        }

        // Include CSRF token for state-changing requests
        if (includeCSRF) {
            const csrfToken = window.Auth?.getCSRFToken();
            if (csrfToken) {
                headers['X-CSRF-Token'] = csrfToken;
            }
        }

        return headers;
    },

    // Parse API error responses
    async parseError(response) {
        try {
            const data = await response.json();
            // Include details if available (e.g., validation errors)
            if (data.details) {
                return `${data.error}: ${data.details}`;
            }
            return data.error || data.message || `HTTP ${response.status}`;
        } catch {
            return `HTTP ${response.status}: ${response.statusText}`;
        }
    },

    // Generic request method
    async request(method, path, body = null) {
        const includeCSRF = ['POST', 'PATCH', 'PUT', 'DELETE'].includes(method);
        const options = {
            method,
            headers: this.buildHeaders(includeCSRF)
        };

        if (body) {
            options.body = JSON.stringify(body);
        }

        let response;
        try {
            response = await fetch(`${this.baseURL}${path}`, options);
        } catch (networkError) {
            return { ok: false, status: 0, error: 'Network error: unable to reach server' };
        }

        // Handle 401 - token expired/invalid
        if (response.status === 401) {
            window.Auth?.logout();
            return { ok: false, status: 401, error: 'Authentication required' };
        }

        // Handle 429 - rate limited
        if (response.status === 429) {
            return { ok: false, status: 429, error: 'Rate limit exceeded. Please wait.' };
        }

        // Parse successful responses
        if (response.ok) {
            const text = await response.text();
            try {
                const data = text ? JSON.parse(text) : null;
                return { ok: true, status: response.status, data };
            } catch {
                return { ok: false, status: response.status, error: 'Invalid JSON response from server' };
            }
        }

        // Other errors
        return { ok: false, status: response.status, error: await this.parseError(response) };
    },

    // Convenience methods
    get(path) { return this.request('GET', path); },
    post(path, body) { return this.request('POST', path, body); },
    patch(path, body) { return this.request('PATCH', path, body); },
    put(path, body) { return this.request('PUT', path, body); },
    delete(path) { return this.request('DELETE', path); }
};

window.APIClient = APIClient;
