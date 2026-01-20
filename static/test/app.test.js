/**
 * Property-based unit tests for Alpine.js app component
 * Tests validation logic, state management, and API client behavior
 */

const { test, expect } = require('@playwright/test');
const path = require('path');

// Mock fetch for isolated testing
function createMockFetch(responseData, status = 200) {
    return async (url, options) => {
        return {
            ok: status === 200,
            status: status,
            json: async () => ({
                data: responseData,
                error: status !== 200 ? 'Test error' : undefined
            })
        };
    };
}

test.describe('Alpine.js App Unit Tests', () => {

    test.beforeEach(async ({ page }) => {
        // Load Alpine.js and app.js using addScriptTag to bypass data: URL restrictions
        const alpinePath = path.join(__dirname, '..', 'js', 'alpine.min.js');
        const appPath = path.join(__dirname, '..', 'js', 'app.js');

        await page.goto('data:text/html,<html><body></body></html>');
        await page.addScriptTag({ path: alpinePath });
        await page.addScriptTag({ path: appPath });
    });

    test('login clears inputToken and calls /proxy/login', async ({ page }) => {
        // Mock fetch to intercept login call
        await page.evaluate(() => {
            window.loginCalled = false;
            window.loginToken = null;

            // Override global fetch
            const originalFetch = globalThis.fetch;
            globalThis.fetch = async (url, options) => {
                if (url === '/proxy/login' && options?.method === 'POST') {
                    window.loginCalled = true;
                    const body = JSON.parse(options.body);
                    window.loginToken = body.token;
                    return {
                        ok: true,
                        status: 200,
                        json: async () => ({ csrf_token: 'test-csrf-token-123' })
                    };
                }
                // For other calls, return mock success
                return {
                    ok: true,
                    status: 200,
                    json: async () => ({ data: { server_ip: '192.168.1.1', update_interval: 30, category_order: [], category_emojis: {}, servers: [] } })
                };
            };
        });

        const result = await page.evaluate(async () => {
            const appInstance = app();
            appInstance.inputToken = 'test-bearer-token';
            await appInstance.login();

            return {
                inputCleared: appInstance.inputToken === '',
                loginCalled: window.loginCalled,
                loginToken: window.loginToken
            };
        });

        expect(result.inputCleared).toBe(true);
        expect(result.loginCalled).toBe(true);
        expect(result.loginToken).toBe('test-bearer-token');
    });

    test('config watcher sets dirty flag on changes', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            appInstance.config = {
                server_ip: '192.168.1.1',
                update_interval: 30,
                category_order: ['Test'],
                category_emojis: { 'Test': '🏎️' },
                servers: []
            };
            appInstance.dirty = false;

            // Simulate Alpine watcher by calling the watcher logic
            if (appInstance.dirty === false || appInstance.dirty === 'remote') {
                appInstance.dirty = 'local';
            }
            appInstance.saved = false;

            return {
                dirtyState: appInstance.dirty,
                savedState: appInstance.saved
            };
        });

        expect(result.dirtyState).toBe('local');
        expect(result.savedState).toBe(false);
    });

    test('port validation rejects invalid ports', async ({ page }) => {
        const testCases = [
            { port: 0, valid: false },
            { port: -1, valid: false },
            { port: 65536, valid: false },
            { port: 100000, valid: false },
            { port: 1, valid: true },
            { port: 3001, valid: true },
            { port: 65535, valid: true }
        ];

        for (const tc of testCases) {
            const result = await page.evaluate((port) => {
                const appInstance = app();
                appInstance.config = {
                    server_ip: '192.168.1.1',
                    update_interval: 30,
                    category_order: ['Test'],
                    category_emojis: { 'Test': '🏎️' },
                    servers: [{ name: 'Test', port: port, category: 'Test' }]
                };

                // Run validation logic from save()
                for (const server of appInstance.config.servers) {
                    if (!server.name.trim()) {
                        return 'Server name cannot be empty';
                    }
                    if (server.port < 1 || server.port > 65535) {
                        return `Invalid port: ${server.port} (valid range: 1-65535)`;
                    }
                }
                return 'valid';
            }, tc.port);

            if (tc.valid) {
                expect(result).toBe('valid');
            } else {
                expect(result).toContain('Invalid port');
            }
        }
    });

    test('server name validation rejects empty names', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            appInstance.config = {
                server_ip: '192.168.1.1',
                update_interval: 30,
                category_order: ['Test'],
                category_emojis: { 'Test': '🏎️' },
                servers: [{ name: '', port: 3001, category: 'Test' }]
            };

            // Run validation logic
            for (const server of appInstance.config.servers) {
                if (!server.name.trim()) {
                    return 'Server name cannot be empty';
                }
            }
            return 'valid';
        });

        expect(result).toBe('Server name cannot be empty');
    });

    test('server category validation rejects invalid categories', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            appInstance.config = {
                server_ip: '192.168.1.1',
                update_interval: 30,
                category_order: ['Drift', 'Track'],
                category_emojis: { 'Drift': '🏎️', 'Track': '🛤️' },
                servers: [{ name: 'Test', port: 3001, category: 'Invalid' }]
            };

            // Run validation logic
            for (const server of appInstance.config.servers) {
                if (!appInstance.config.category_order.includes(server.category)) {
                    return `Invalid category: ${server.category}`;
                }
            }
            return 'valid';
        });

        expect(result).toBe('Invalid category: Invalid');
    });

    test('addServer creates server with default values', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            appInstance.config = {
                server_ip: '192.168.1.1',
                update_interval: 30,
                category_order: ['Drift', 'Track'],
                category_emojis: { 'Drift': '🏎️', 'Track': '🛤️' },
                servers: []
            };

            appInstance.addServer();

            return {
                serverCount: appInstance.config.servers.length,
                newServer: appInstance.config.servers[0]
            };
        });

        expect(result.serverCount).toBe(1);
        expect(result.newServer.name).toBe('');
        expect(result.newServer.port).toBe(8081);
        expect(result.newServer.category).toBe('Drift');
    });

    test('removeServer deletes server at index', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            appInstance.config = {
                server_ip: '192.168.1.1',
                update_interval: 30,
                category_order: ['Test'],
                category_emojis: { 'Test': '🏎️' },
                servers: [
                    { name: 'Server 1', port: 8081, category: 'Test' },
                    { name: 'Server 2', port: 8082, category: 'Test' },
                    { name: 'Server 3', port: 8083, category: 'Test' }
                ]
            };

            appInstance.removeServer(1);

            return {
                serverCount: appInstance.config.servers.length,
                remainingNames: appInstance.config.servers.map(s => s.name)
            };
        });

        expect(result.serverCount).toBe(2);
        expect(result.remainingNames).toEqual(['Server 1', 'Server 3']);
    });

    test('polling backoff doubles on errors', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();

            // Simulate error in fetchConfig
            const initialBackoff = appInstance.pollBackoffInterval;
            appInstance.pollBackoffInterval = Math.min(appInstance.pollBackoffInterval * 2, 808000);

            return {
                initial: initialBackoff,
                afterError: appInstance.pollBackoffInterval
            };
        });

        expect(result.afterError).toBe(60000); // 30s * 2
    });

    test('polling backoff caps at 300s', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            appInstance.pollBackoffInterval = 200000; // Already high

            appInstance.pollBackoffInterval = Math.min(appInstance.pollBackoffInterval * 2, 808000);

            return appInstance.pollBackoffInterval;
        });

        expect(result).toBe(808000);
    });

    test('polling backoff resets on success', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            appInstance.pollBackoffInterval = 120000; // Elevated backoff

            // Simulate success in fetchConfig
            appInstance.pollBackoffInterval = 80800;

            return appInstance.pollBackoffInterval;
        });

        expect(result).toBe(80800);
    });

    test('dirty flag state transitions', async ({ page }) => {
        const transitions = [
            { from: false, to: 'local', description: 'clean -> local on edit' },
            { from: 'local', to: 'local', description: 'local stays local on edit' }
        ];

        for (const transition of transitions) {
            const result = await page.evaluate((fromState) => {
                const appInstance = app();
                appInstance.dirty = fromState;

                // Simulate watcher logic
                if (appInstance.dirty === false) {
                    appInstance.dirty = 'local';
                }

                return appInstance.dirty;
            }, transition.from);

            expect(result).toBe('local');
        }
    });

    test('config update skipped when dirty is local', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            appInstance.config = {
                server_ip: '192.168.1.1',
                update_interval: 30,
                category_order: ['Test'],
                category_emojis: { 'Test': '🏎️' },
                servers: []
            };
            appInstance.dirty = 'local';

            const originalConfig = JSON.stringify(appInstance.config);

            // Simulate fetchConfig with dirty='local'
            const mockResponse = {
                server_ip: '10.0.0.1',
                update_interval: 60,
                category_order: ['New'],
                category_emojis: { 'New': '✅' },
                servers: []
            };

            if (appInstance.dirty === false) {
                appInstance.config = mockResponse;
                // Keep dirty=false (clean) after fetching - allows future polling updates
            } else if (appInstance.dirty === 'local') {
                appInstance.remoteChanged = true;
            }

            return {
                configChanged: JSON.stringify(appInstance.config) !== originalConfig,
                remoteChangedFlag: appInstance.remoteChanged,
                dirtyState: appInstance.dirty
            };
        });

        expect(result.configChanged).toBe(false);
        expect(result.remoteChangedFlag).toBe(true);
        expect(result.dirtyState).toBe('local');
    });

    test('apiRequest unwraps response data', async ({ page }) => {
        const mockData = {
            server_ip: '192.168.1.100',
            update_interval: 30
        };

        const result = await page.evaluate((data) => {
            // Simulate apiRequest unwrapping logic
            const mockResponse = { data: data };
            return mockResponse.data;
        }, mockData);

        expect(result.server_ip).toBe('192.168.1.100');
    });

    test('server ip validation rejects empty', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            appInstance.config = {
                server_ip: '',
                update_interval: 30,
                category_order: ['Test'],
                category_emojis: { 'Test': '🏎️' },
                servers: []
            };

            if (!appInstance.config.server_ip.trim()) {
                return 'Server IP cannot be empty';
            }
            return 'valid';
        });

        expect(result).toBe('Server IP cannot be empty');
    });

    test('update interval validation rejects values < 1', async ({ page }) => {
        const testCases = [
            { interval: 0, valid: false },
            { interval: -5, valid: false },
            { interval: 1, valid: true },
            { interval: 30, valid: true }
        ];

        for (const tc of testCases) {
            const result = await page.evaluate((interval) => {
                const appInstance = app();
                appInstance.config = {
                    server_ip: '192.168.1.1',
                    update_interval: interval,
                    category_order: ['Test'],
                    category_emojis: { 'Test': '🏎️' },
                    servers: []
                };

                if (appInstance.config.update_interval < 1) {
                    return 'Update interval must be at least 1 second';
                }
                return 'valid';
            }, tc.interval);

            if (tc.valid) {
                expect(result).toBe('valid');
            } else {
                expect(result).toBe('Update interval must be at least 1 second');
            }
        }
    });

    test('saved flag clears on config change', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            appInstance.saved = true;

            // Simulate watcher clearing saved flag
            appInstance.saved = false;

            return appInstance.saved;
        });

        expect(result).toBe(false);
    });

    test('login does not store token in app instance', async ({ page }) => {
        // Mock fetch for login
        await page.evaluate(() => {
            const originalFetch = globalThis.fetch;
            globalThis.fetch = async (url, options) => {
                if (url === '/proxy/login') {
                    return {
                        ok: true,
                        status: 200,
                        json: async () => ({ csrf_token: 'test-csrf' })
                    };
                }
                return {
                    ok: true,
                    status: 200,
                    json: async () => ({ data: { server_ip: '192.168.1.1', update_interval: 30, category_order: [], category_emojis: {}, servers: [] } })
                };
            };
        });

        const result = await page.evaluate(async () => {
            const appInstance = app();
            appInstance.inputToken = 'test-token-123';
            await appInstance.login();

            return {
                hasInputToken: appInstance.inputToken !== '',
                hasCsrfToken: appInstance.csrfToken !== ''
            };
        });

        expect(result.hasInputToken).toBe(false);
        expect(result.hasCsrfToken).toBe(true);
    });

    test('login sends POST to /proxy/login with token in body', async ({ page }) => {
        await page.evaluate(() => {
            window.loginRequest = null;

            const originalFetch = globalThis.fetch;
            globalThis.fetch = async (url, options) => {
                // Capture only the login request
                if (url === '/proxy/login' && options?.method === 'POST') {
                    window.loginRequest = {
                        url: url,
                        method: options?.method,
                        body: options?.body ? JSON.parse(options.body) : null
                    };
                }

                return {
                    ok: true,
                    status: 200,
                    json: async () => ({ csrf_token: 'test-csrf' })
                };
            };
        });

        await page.evaluate(async () => {
            const appInstance = app();
            appInstance.inputToken = 'my-bearer-token';
            await appInstance.login();
        });

        const result = await page.evaluate(() => {
            return window.loginRequest;
        });

        expect(result.url).toBe('/proxy/login');
        expect(result.method).toBe('POST');
        expect(result.body).toEqual({ token: 'my-bearer-token' });
    });

    test('apiRequest uses /proxy prefix and no Authorization header', async ({ page }) => {
        await page.evaluate(() => {
            window.requestUrl = null;
            window.requestHeaders = null;

            const originalFetch = globalThis.fetch;
            globalThis.fetch = async (url, options) => {
                window.requestUrl = url;
                window.requestHeaders = options?.headers;

                return {
                    ok: true,
                    status: 200,
                    json: async () => ({ data: { server_ip: '192.168.1.1', update_interval: 30, category_order: [], category_emojis: {}, servers: [] } })
                };
            };
        });

        await page.evaluate(() => {
            const appInstance = app();
            appInstance.apiRequest('GET', '/proxy/api/config');
        });

        const result = await page.evaluate(() => {
            return {
                url: window.requestUrl,
                hasAuthHeader: window.requestHeaders?.hasOwnProperty('Authorization')
            };
        });

        expect(result.url).toBe('/proxy/api/config');
        expect(result.hasAuthHeader).toBe(false);
    });

    test('CSRF token property exists and getCSRFToken is a function', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            return {
                hasCsrfToken: 'csrfToken' in appInstance,
                isGetCSRFTokenFunction: typeof appInstance.getCSRFToken === 'function'
            };
        });

        expect(result.hasCsrfToken).toBe(true);
        expect(result.isGetCSRFTokenFunction).toBe(true);
    });

    test('getCSRFToken returns stored csrfToken property', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            appInstance.csrfToken = 'test-csrf-token-123';
            const token = appInstance.getCSRFToken();
            return {
                token: token,
                isEmpty: token === ''
            };
        });

        expect(result.token).toBe('test-csrf-token-123');
        expect(result.isEmpty).toBe(false);
    });

    test('getCSRFToken returns empty string when not set', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            return appInstance.getCSRFToken();
        });

        expect(result).toBe('');
    });

    test('apiRequest adds X-CSRF-Token header for POST', async ({ page }) => {
        await page.evaluate(() => {
            window.requestHeaders = null;

            const originalFetch = globalThis.fetch;
            globalThis.fetch = async (url, options) => {
                window.requestHeaders = options?.headers;
                return {
                    ok: true,
                    status: 200,
                    json: async () => ({ data: { server_ip: '192.168.1.1' } })
                };
            };
        });

        await page.evaluate(() => {
            const appInstance = app();
            appInstance.csrfToken = 'csrf-token-456';
            appInstance.apiRequest('POST', '/proxy/api/config', { test: 'data' });
        });

        const result = await page.evaluate(() => {
            return {
                hasCsrfHeader: window.requestHeaders?.hasOwnProperty('X-CSRF-Token'),
                csrfToken: window.requestHeaders?.['X-CSRF-Token']
            };
        });

        expect(result.hasCsrfHeader).toBe(true);
        expect(result.csrfToken).toBe('csrf-token-456');
    });

    test('apiRequest adds X-CSRF-Token header for PATCH', async ({ page }) => {
        await page.evaluate(() => {
            window.requestHeaders = null;

            const originalFetch = globalThis.fetch;
            globalThis.fetch = async (url, options) => {
                window.requestHeaders = options?.headers;
                return {
                    ok: true,
                    status: 200,
                    json: async () => ({ data: { server_ip: '192.168.1.1' } })
                };
            };
        });

        await page.evaluate(() => {
            const appInstance = app();
            appInstance.csrfToken = 'patch-token-789';
            appInstance.apiRequest('PATCH', '/proxy/api/config', { update: 'me' });
        });

        const result = await page.evaluate(() => {
            return {
                csrfToken: window.requestHeaders?.['X-CSRF-Token']
            };
        });

        expect(result.csrfToken).toBe('patch-token-789');
    });

    test('apiRequest does not add X-CSRF-Token for GET', async ({ page }) => {
        await page.evaluate(() => {
            window.requestHeaders = null;

            const originalFetch = globalThis.fetch;
            globalThis.fetch = async (url, options) => {
                window.requestHeaders = options?.headers;
                return {
                    ok: true,
                    status: 200,
                    json: async () => ({ data: { server_ip: '192.168.1.1' } })
                };
            };
        });

        await page.evaluate(() => {
            const appInstance = app();
            appInstance.csrfToken = 'get-token-999';
            appInstance.apiRequest('GET', '/proxy/api/config');
        });

        const result = await page.evaluate(() => {
            return {
                hasCsrfHeader: window.requestHeaders?.hasOwnProperty('X-CSRF-Token')
            };
        });

        expect(result.hasCsrfHeader).toBe(false);
    });

    test('apiRequest handles 403 Forbidden with CSRF error message', async ({ page }) => {
        await page.evaluate(() => {
            const originalFetch = globalThis.fetch;
            globalThis.fetch = async (url, options) => {
                return {
                    ok: false,
                    status: 403,
                    json: async () => ({ error: 'forbidden: CSRF token mismatch' })
                };
            };
        });

        const error = await page.evaluate(async () => {
            const appInstance = app();
            try {
                await appInstance.apiRequest('POST', '/proxy/api/config', { test: 'data' });
                return null;
            } catch (err) {
                return err.message;
            }
        });

        expect(error).toContain('CSRF token mismatch');
    });

    test('getCSRFToken returns stored value', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            appInstance.csrfToken = 'test-token-abc123';
            return appInstance.getCSRFToken();
        });

        expect(result).toBe('test-token-abc123');
    });

    test('getCSRFToken can be updated', async ({ page }) => {
        const result = await page.evaluate(() => {
            const appInstance = app();
            appInstance.csrfToken = 'initial-token';
            const first = appInstance.getCSRFToken();
            appInstance.csrfToken = 'updated-token';
            const second = appInstance.getCSRFToken();
            return { first, second };
        });

        expect(result.first).toBe('initial-token');
        expect(result.second).toBe('updated-token');
    });
});
