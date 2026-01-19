const { test, expect } = require('@playwright/test');
const { spawn } = require('child_process');
const path = require('path');
const os = require('os');
const fs = require('fs');

// Helper function to find free port
async function findFreePort() {
    const net = require('net');
    return new Promise((resolve) => {
        const server = net.createServer();
        server.listen(0, () => {
            const port = server.address().port;
            server.close(() => resolve(port));
        });
    });
}

// Test fixture config
const mockConfig = {
    server_ip: '192.168.1.100',
    update_interval: 30,
    category_order: ['Drift', 'Track'],
    category_emojis: {
        'Drift': '🏎️',
        'Track': '🛤️'
    },
    servers: [
        { name: 'Test Server', port: 8081, category: 'Drift' }
    ]
};

test.describe('Config Frontend Integration Tests', () => {
    let apiServer;
    let testDir;
    let testPort;

    test.beforeAll(async () => {
        // Create isolated temp directory for test files
        testDir = fs.mkdtempSync(path.join(os.tmpdir(), 'ac-bot-test-'));
        fs.writeFileSync(path.join(testDir, 'config.json'), JSON.stringify(mockConfig, null, 2));

        // Find free port to avoid conflicts
        testPort = await findFreePort();

        // Start Go API server for testing with isolated config
        apiServer = spawn('go', ['run', 'main.go', '-c', path.join(testDir, 'config.json')], {
            cwd: testDir,
            env: {
                ...process.env,
                API_ENABLED: 'true',
                API_PORT: String(testPort),
                API_BEARER_TOKEN: 'test-token-123',
                DISCORD_TOKEN: 'fake-discord-token',
                CHANNEL_ID: '123456789'
            }
        });

        // Wait for server with timeout
        const deadline = Date.now() + 10000;

        while (Date.now() < deadline) {
            try {
                await fetch(`http://localhost:${testPort}/health`);
                break;
            } catch {
                await new Promise(r => setTimeout(r, 200));
            }
        }

        if (Date.now() >= deadline) {
            throw new Error('Server failed to start within 10s');
        }
    });

    test.afterAll(async () => {
        if (apiServer) {
            apiServer.kill();
        }
        // Cleanup temp directory
        fs.unlinkSync(path.join(testDir, 'config.json'));
        fs.rmdirSync(testDir);
    });

    test('login flow stores token and shows form', async ({ page }) => {
        await page.goto(`http://localhost:${testPort}`);

        // Verify login modal is visible
        await expect(page.locator('.modal')).toBeVisible();
        await expect(page.locator('h2')).toContainText('Authentication Required');

        // Enter token and login
        await page.fill('input[type="password"]', 'test-token-123');
        await page.click('button');

        // Verify login modal is hidden and form is visible
        await expect(page.locator('.modal')).not.toBeVisible();
        await expect(page.locator('.container')).toBeVisible();
        await expect(page.locator('h1')).toContainText('Server Configuration');
    });

    test('config load displays all fields', async ({ page }) => {
        // Set token before navigating (simulating sessionStorage)
        await page.goto(`http://localhost:${testPort}`);
        await page.evaluate(() => {
            sessionStorage.setItem('apiToken', 'test-token-123');
        });
        await page.reload();

        // Wait for config to load
        const serverIpInput = page.locator('input[type="text"]').first();
        await expect(serverIpInput).toHaveValue(mockConfig.server_ip);

        // Verify servers are displayed
        const serverItems = page.locator('.server-item');
        await expect(serverItems).toHaveCount(mockConfig.servers.length);
    });

    test('save config updates server state', async ({ page }) => {
        await page.goto(`http://localhost:${testPort}`);
        await page.evaluate(() => {
            sessionStorage.setItem('apiToken', 'test-token-123');
        });
        await page.reload();

        // Edit server_ip field
        await page.fill('input[placeholder*="Server IP"]', '10.0.0.1');

        // Click save button
        await page.click('button:has-text("Save Changes")');

        // Verify success message
        await expect(page.locator('.success-message')).toContainText('Saved!');

        // Verify server was updated by fetching config again
        const response = await page.evaluate(async () => {
            const res = await fetch('/api/config', {
                headers: { 'Authorization': 'Bearer test-token-123' }
            });
            return res.json();
        });
        expect(response.data.server_ip).toBe('10.0.0.1');
    });

    test('validation errors display correctly', async ({ page }) => {
        await page.goto(`http://localhost:${testPort}`);
        await page.evaluate(() => {
            sessionStorage.setItem('apiToken', 'test-token-123');
        });
        await page.reload();

        // Set invalid port
        await page.fill('.server-item input[type="number"]', '70000');
        await page.click('button:has-text("Save Changes")');

        // Verify error message
        await expect(page.locator('.error-message')).toContainText('Invalid port');
    });

    test('unauthorized response clears token', async ({ page }) => {
        await page.goto(`http://localhost:${testPort}`);
        await page.evaluate(() => {
            sessionStorage.setItem('apiToken', 'invalid-token');
        });
        await page.reload();

        // Wait for API call to fail and login modal to reappear
        await expect(page.locator('.modal')).toBeVisible({ timeout: 5000 });

        // Verify token was cleared
        const token = await page.evaluate(() => sessionStorage.getItem('apiToken'));
        expect(token).toBeNull();
    });

    test('alpine js initializes correctly', async ({ page }) => {
        await page.goto(`http://localhost:${testPort}`);

        // Verify Alpine.js is loaded and initialized
        await page.waitForFunction(() => typeof window.Alpine !== 'undefined');
        const alpineVersion = await page.evaluate(() => window.Alpine.version);
        expect(alpineVersion).toBe('3.14.0');
    });
});
