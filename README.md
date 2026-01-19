# AC Discord Bot

Discord bot for monitoring and displaying status information for Assetto Corsa racing servers.

**Architecture:** Single-file Go application with concurrent goroutines for parallel server monitoring.

## Features

- Real-time server status monitoring
- Player count tracking across multiple server categories
- Direct join links via acstuff.club
- Automatic status updates every 30 seconds
- Server categories: Drift, Touge, Track
- Message cleanup on startup to remove old bot messages
- Graceful error handling with automatic message recovery
- **REST API** for dynamic configuration management (optional)

## Prerequisites

- **Go 1.25.5** or later
- Discord bot token ([Discord Developer Portal](https://discord.com/developers/applications))
- config.json file (see Configuration section below)

## Running Locally

1. Create config.json from the example:

```bash
cp config.json.example config.json
# Edit config.json with your server details
nano config.json
```

2. Set required environment variables:

```bash
export DISCORD_TOKEN="your_bot_token_here"
export CHANNEL_ID="your_channel_id"
```

Or create a `.env` file and source it.

3. Install dependencies:

```bash
go mod download
```

4. Run the bot:

```bash
# Use default config paths (tries /data/config.json, then ./config.json)
go run main.go

# Or specify a custom config file
go run main.go -c /path/to/config.json
go run main.go --config /path/to/config.json
```

Or build and run:

```bash
go build -o bot .

# Use default config paths
./bot

# Specify custom config file
./bot -c /path/to/config.json
```

## Usage

The bot supports command-line flags for specifying the config file location:

### Command-Line Flags

| Flag | Description |
|------|-------------|
| `-c, --config` | Path to config.json file (optional) |

### Config File Loading Order

The bot loads configuration in the following priority order:

1. **Command-line flag** (if provided): `-c` or `--config` - uses only this path
2. **Container path**: `/data/config.json` - checked when no flag is provided
3. **Local path**: `./config.json` - checked when no flag is provided (fallback for local development)

### Examples

```bash
# Local development - uses ./config.json
./bot

# Container deployment - automatically finds /data/config.json
podman run -v $(pwd)/config.json:/data/config.json:ro ac-discordbot

# Custom config file
./bot -c /etc/bot/production-config.json

# Test with alternate configuration
./bot --config ./test-config.json
```

## Configuration

The bot uses a hybrid configuration model: sensitive secrets in environment variables, display configuration in JSON.

### Environment Variables (Secrets)

Required environment variables:

- `DISCORD_TOKEN` - Bot authentication token from Discord Developer Portal
- `CHANNEL_ID` - Target Discord channel ID for status messages (integer)

### JSON Configuration

Create `config.json` in the working directory with the following structure:

```json
{
  "server_ip": "your.server.ip",
  "update_interval": 30,
  "category_order": ["Drift", "Touge", "Track"],
  "category_emojis": {
    "Drift": "🏎️",
    "Touge": "⛰️",
    "Track": "🛤️"
  },
  "servers": [
    {
      "name": "Server Name",
      "port": 8091,
      "category": "Drift"
    }
  ]
}
```

**JSON Schema:**

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `server_ip` | string | Yes | Non-empty IP address or hostname |
| `update_interval` | integer | Yes | Minimum 1 second (recommended: 30) |
| `category_order` | array | Yes | Non-empty array of category names |
| `category_emojis` | object | Yes | Must contain all categories from `category_order` as keys |
| `servers` | array | Yes | Array of server objects (see below) |

**Server Object Schema:**

| Field | Type | Required | Constraints |
|-------|------|----------|-------------|
| `name` | string | Yes | Non-empty display name |
| `port` | integer | Yes | Valid port: 1-65535 (HTTP query port, not game port) |
| `category` | string | Yes | Must exist in `category_order` array |

**Validation Rules:**

- Every category in `category_order` must have a corresponding emoji in `category_emojis`
- Every server's `category` field must match one of the categories in `category_order`
- Port numbers must be within valid range (1-65535)
- The `server_ip` is automatically prepended to each server's address for HTTP queries

## REST API (Optional)

The bot includes an optional REST API for dynamic configuration management. When enabled, the API runs alongside the Discord bot, allowing you to update `config.json` via HTTP requests without restarting the bot.

### Enabling the API

Set the following environment variables:

```bash
# Enable the REST API server
API_ENABLED=true

# API server port (default: 8080)
API_PORT=8080

# Bearer token for authentication (required if API_ENABLED=true)
API_BEARER_TOKEN=your-secure-token-here

# Optional: Comma-separated list of allowed CORS origins
# Leave empty for no CORS, use "*" for all origins
API_CORS_ORIGINS=https://example.com,https://app.com
```

### API Endpoints

All endpoints (except `/health`) require Bearer token authentication:

```bash
# Set your token
export API_TOKEN="your-secure-token-here"

# Health check (no auth required)
curl http://localhost:8080/health

# Get current configuration
curl -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/config

# Get servers only
curl -H "Authorization: Bearer $API_TOKEN" \
  http://localhost:8080/api/config/servers

# Partial update (PATCH) - merges with existing config
curl -X PATCH \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"update_interval": 120}' \
  http://localhost:8080/api/config

# Full replacement (PUT) - replaces entire config
curl -X PUT \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d @config.json \
  http://localhost:8080/api/config

# Validate without applying
curl -X POST \
  -H "Authorization: Bearer $API_TOKEN" \
  -H "Content-Type: application/json" \
  -d @config.json \
  http://localhost:8080/api/config/validate
```

### API Features

- **Atomic writes**: Config updates use temp-file-then-rename pattern to prevent corruption
- **Backup rotation**: Every write creates 4 backup files (`config.json.backup`, `.backup.1`, `.backup.2`, `.backup.3`) for rollback
- **Automatic reload**: Changes trigger the existing 30-second polling cycle to reload config
- **Bearer token auth**: RFC 6750 compliant authentication
- **Rate limiting**: 10 req/sec per IP with 20 request burst
- **CORS support**: Configurable cross-origin requests for web applications
- **Security headers**: X-Content-Type-Options, X-Frame-Options, CSP included

### Response Format

Success responses:
```json
{
  "data": {
    "server_ip": "192.168.1.100",
    "update_interval": 120,
    "category_order": ["Drift", "Track"],
    ...
  }
}
```

Error responses:
```json
{
  "error": "Config validation failed",
  "details": "server_ip cannot be empty"
}
```

## Session-Based Proxy (Optional)

The bot includes an optional session-based authentication layer that proxies API requests, eliminating Bearer token exposure in browser JavaScript. When enabled, the web UI authenticates once via Bearer token, receives an HTTP-only session cookie, and all subsequent API requests use the session cookie instead of the Bearer token. The backend validates sessions and proxies requests to the existing API, adding the Bearer token server-side.

### Enabling the Proxy

Set the following environment variables:

```bash
# Enable the session-based proxy server
PROXY_ENABLED=true

# Proxy server port (default: 3000)
PROXY_PORT=3000

# Bearer token for proxy validation (uses same token as bot API)
# Must match API_BEARER_TOKEN if both are enabled
API_BEARER_TOKEN=your-secure-token-here
```

### Proxy Endpoints

```bash
# Login (exchange Bearer token for session cookie)
curl -X POST \
  -H "Content-Type: application/json" \
  -d '{"token": "Bearer your-token-here"}' \
  -c cookies.txt \
  http://localhost:3000/proxy/login

# Access API using session cookie
curl -b cookies.txt \
  http://localhost:3000/proxy/api/config

# Logout (invalidate session)
curl -X POST \
  -b cookies.txt \
  http://localhost:3000/proxy/logout
```

### Proxy Features

- **HTTP-only session cookies**: Prevents XSS access to session tokens
- **SameSite=Strict**: CSRF protection by default
- **File-based session storage**: Persists across restarts (in `./sessions/` directory)
- **4-hour session timeout**: Balances security and UX for admin tool
- **Automatic session cleanup**: Background goroutine removes expired sessions every 5 minutes
- **10-second upstream timeout**: Fast error reporting for unresponsive bot API

### Proxy vs. Direct API Access

| Feature | Direct API (Bearer Token) | Session Proxy (Cookie) |
|---------|--------------------------|------------------------|
| Authentication | Bearer token in Authorization header | HTTP-only session cookie |
| Token storage | Frontend (sessionStorage/localStorage) | Server-side only |
| XSS exposure | Token accessible via JavaScript | Token inaccessible (HTTP-only) |
| Use case | API clients, scripts | Web UI, browser-based tools |
| Request path | Client -> Bot API | Client -> Proxy -> Bot API |

### Deployment with Proxy

When enabling the proxy, expose both ports in your container configuration:

```bash
podman run -d \
  --name ac-discordbot \
  -e DISCORD_TOKEN="your_token" \
  -e CHANNEL_ID="your_channel_id" \
  -e API_BEARER_TOKEN="your_api_token" \
  -e API_ENABLED=true \
  -e PROXY_ENABLED=true \
  -p 8080:8080 \
  -p 3000:3000 \
  -v /opt/ac-discordbot/config.json:/data/config.json:ro \
  --restart unless-stopped \
  ac-discordbot
```

The web UI (`static/`) connects to the proxy at port 3000 for secure session-based access, while direct API clients can still connect to port 8080 with Bearer token authentication.

## Deployment

### Podman (Recommended)

```bash
podman build -t ac-discordbot .

# Create config file
mkdir -p /opt/ac-discordbot
cp config.json.example /opt/ac-discordbot/config.json
nano /opt/ac-discordbot/config.json

# Fix permissions for non-root container user (UID 1001)
sudo chown 1001:1001 /opt/ac-discordbot/config.json
sudo chmod 644 /opt/ac-discordbot/config.json

# Run container with volume mount (bot will find /data/config.json automatically)
podman run -d \
  --name ac-discordbot \
  -e DISCORD_TOKEN="your_token" \
  -e CHANNEL_ID="your_channel_id" \
  -v /opt/ac-discordbot/config.json:/data/config.json:ro \
  --restart unless-stopped \
  ac-discordbot
```

### Docker

The `Containerfile` is compatible with Docker:

```bash
docker build -t ac-discordbot .

# Create config file
mkdir -p /opt/ac-discordbot
cp config.json.example /opt/ac-discordbot/config.json
nano /opt/ac-discordbot/config.json

# Fix permissions for non-root container user (UID 1001)
sudo chown 1001:1001 /opt/ac-discordbot/config.json
sudo chmod 644 /opt/ac-discordbot/config.json

# Run container with volume mount (bot will find /data/config.json automatically)
docker run -d \
  --name ac-discordbot \
  -e DISCORD_TOKEN="your_token" \
  -e CHANNEL_ID="your_channel_id" \
  -v /opt/ac-discordbot/config.json:/data/config.json:ro \
  --restart unless-stopped \
  ac-discordbot
```

### CI/CD

The bot uses GitHub Actions to automatically build and push Docker images to GitHub Container Registry (GHCR) on version tags (`v*.*.*`).

Available images: `ghcr.io/{owner}/ac-discordbot:latest`

## Migration Guide

**Breaking Change:** The bot now uses `config.json` for server configuration. The `SERVER_IP` environment variable is no longer used.

### For Existing Deployments

1. **Create config.json from example:**

   ```bash
   cp config.json.example config.json
   ```

2. **Copy your server list:**

   Edit `config.json` and add your servers. The server format has changed from environment variables to JSON:

   ```json
   {
     "server_ip": "your.server.ip",
     "update_interval": 30,
     "category_order": ["Drift", "Touge", "Track"],
     "category_emojis": {
       "Drift": "🏎️",
       "Touge": "⛰️",
       "Track": "🛤️"
     },
     "servers": [
       {
         "name": "Your Server Name",
         "port": 8091,
         "category": "Drift"
       }
     ]
   }
   ```

3. **Update container run command:**

   Add volume mount for config.json and remove `SERVER_IP` environment variable:

   ```bash
   # Old command (DO NOT USE)
   podman run -d \
     --name ac-discordbot \
     -e DISCORD_TOKEN="your_token" \
     -e CHANNEL_ID="your_channel_id" \
     -e SERVER_IP="your.server.ip" \
     --restart unless-stopped \
     ac-discordbot

   # New command (USE THIS)
   podman run -d \
     --name ac-discordbot \
     -e DISCORD_TOKEN="your_token" \
     -e CHANNEL_ID="your_channel_id" \
     -v /opt/ac-discordbot/config.json:/data/config.json:ro \
     --restart unless-stopped \
     ac-discordbot
   ```

4. **Test configuration locally:**

   ```bash
   # Validate JSON syntax
   jq . config.json

   # Run bot locally to verify
   go run main.go
   ```

5. **Deploy updated container:**

   ```bash
   # Stop and remove old container
   podman stop ac-discordbot
   podman rm ac-discordbot

   # Pull latest image
   podman pull ghcr.io/{owner}/ac-discordbot:latest

   # Run with new configuration
   podman run -d \
     --name ac-discordbot \
     -e DISCORD_TOKEN="your_token" \
     -e CHANNEL_ID="your_channel_id" \
     -v /opt/ac-discordbot/config.json:/data/config.json:ro \
     --restart unless-stopped \
     ghcr.io/{owner}/ac-discordbot:latest
   ```

6. **Verify deployment:**

   ```bash
   # Check logs for successful config load
   podman logs ac-discordbot | grep "Loading config"
   podman logs ac-discordbot | grep "Configuration validated"
   ```

## Troubleshooting

### config.json Not Found

**Error:** `failed to load config from any location`

**Solutions:**
- The bot tries multiple paths: command-line flag → `/data/config.json` → `./config.json`
- For containers: verify volume mount path is `/data/config.json`
- For local development: verify config.json exists in working directory
- Use absolute host paths in volume mounts (not relative paths)
- Verify mount syntax: `-v /absolute/host/path:/data/config.json:ro`
- Or use the `-c` flag to specify the exact path: `./bot -c /path/to/config.json`

**Debug commands:**
```bash
# Check what the container sees
podman exec ac-discordbot ls -la /data/

# Check logs for exact path being searched
podman logs ac-discordbot | grep "Attempting to load config"

# List all attempted paths on failure
podman logs ac-discordbot | grep "failed to load config"
```

### Permission Denied

**Error:** `failed to read config.json: permission denied`

**Cause:** Container runs as non-root user (UID 1001) and cannot read the config file.

**Solution:**
```bash
# Fix file ownership
sudo chown 1001:1001 /path/to/config/config.json

# Fix permissions (readable by owner)
sudo chmod 644 /path/to/config/config.json

# For directory mounts, fix directory too
sudo chown -R 1001:1001 /path/to/config/
sudo chmod 755 /path/to/config/
```

**Verify:**
```bash
# Check container user ID
podman exec ac-discordbot id
# Output: uid=1001(botuser) gid=1001(botuser)

# Test file access
podman exec ac-discordbot cat /data/config.json
```

### Invalid JSON Syntax

**Error:** `failed to parse config.json: invalid character...`

**Solutions:**
- Validate JSON syntax using `jq`: `jq . config.json`
- Use online JSON validator: https://jsonlint.com/
- Common mistakes:
  - Trailing commas: `{"servers": [],}` (remove trailing comma)
  - Unquoted strings: `{name: "Server"}` (keys must be quoted)
  - Single quotes: `{'name': 'Server'}` (use double quotes)
  - Comments: JSON does not support `//` or `/* */` comments

**Example of valid vs invalid:**
```json
// INVALID - trailing comma
{"servers": [{"name": "Server"}],}

// VALID
{"servers": [{"name": "Server"}]}
```

### Port Out of Range

**Error:** `server 'ServerName' has invalid port: 70000 (valid range: 1-65535)`

**Solution:** Use a valid port number between 1 and 65535.

**Note:** This is the HTTP query port, not the game port. For Assetto Corsa servers, the HTTP query port is typically the game port + 100 (e.g., game port 8091 → HTTP query port 8191).

### Category Not Found

**Error:** `server 'ServerName' has category 'Drift' which is not defined in category_order`

**Cause:** Every server's category must exist in the `category_order` array.

**Solution:** Add the missing category to `category_order` and `category_emojis`:

```json
{
  "category_order": ["Drift", "Touge", "Track", "NewCategory"],
  "category_emojis": {
    "Drift": "🏎️",
    "Touge": "⛰️",
    "Track": "🛤️",
    "NewCategory": "🆕"
  },
  "servers": [
    {
      "name": "Server",
      "port": 8091,
      "category": "NewCategory"
    }
  ]
}
```

### Missing Category Emoji

**Error:** `category 'Drift' is in category_order but missing from category_emojis`

**Cause:** Every category in `category_order` must have a corresponding emoji in `category_emojis`.

**Solution:** Add the missing emoji mapping:

```json
{
  "category_order": ["Drift", "Touge"],
  "category_emojis": {
    "Drift": "🏎️",
    "Touge": "⛰️"
  }
}
```

### Update Interval Too Low

**Error:** `update_interval must be at least 1 second (got: 0)`

**Solution:** Set `update_interval` to at least 1 second. Recommended: 30 seconds for production to avoid rate limiting.

### Empty Required Fields

**Error:** `Configuration error: server_ip cannot be empty` or `server at index 0 has empty name`

**Solution:** Ensure all required fields are populated:
- `server_ip` must be a non-empty string
- All servers must have non-empty `name` and `category` fields
- `category_order` must be a non-empty array

### Bot Not Updating Messages

**Symptoms:** Container is running but Discord channel shows no updates.

**Debug steps:**
```bash
# Check container is running
podman ps | grep ac-discordbot

# Check logs for errors
podman logs -f ac-discordbot

# Verify config loaded successfully
podman logs ac-discordbot | grep "Configuration validated"

# Check bot has permissions in Discord
# - Bot must have "Read Messages" and "Send Messages" permissions
# - CHANNEL_ID must be correct (right-click channel → Copy ID)
```

## Code Architecture

### Single-File Structure

The entire bot lives in `main.go` with clear section headers:

- **CONFIG** - Environment variables and server configuration
- **TYPES** - ServerInfo and Bot structs
- **HTTP CLIENT** - Server info fetching with concurrent goroutines
- **DISCORD INTEGRATION** - Embed building and message updates
- **EVENT HANDLERS** - Bot lifecycle and cleanup
- **UPDATE LOOP** - Periodic server status updates
- **BOT CONSTRUCTION** - Bot initialization and startup
- **MAIN** - Configuration validation and entry point

### Key Patterns

**Concurrent Fetching:** Uses goroutines and `sync.WaitGroup` to query all servers in parallel:

```go
for i, server := range servers {
    wg.Add(1)
    go func(idx int, s Server) {
        defer wg.Done()
        info := fetchServerInfo(s)
        mu.Lock()
        infos[idx] = info
        mu.Unlock()
    }(i, server)
}
```

**Thread-Safe State Management:** Uses `sync.RWMutex` to protect the global message state across goroutines.

**Graceful Degradation:** Server fetch failures return offline status instead of crashing the bot.

**Message Recovery:** If the status message is deleted, the bot automatically creates a new one.

### Adding Servers

Edit `config.json` and add a new server object to the `servers` array:

```json
{
  "servers": [
    {
      "name": "Server Name",
      "port": 8091,
      "category": "Drift"
    },
    {
      "name": "Another Server",
      "port": 8092,
      "category": "Touge"
    }
  ]
}
```

**Notes:**
- The `server_ip` from the top level is automatically prepended to each server's address
- The `category` must exist in the `category_order` array
- The `port` is the HTTP query port, not the game port (typically game port + 100)

## ConfigManager Architecture

### Overview

The bot uses a thread-safe ConfigManager wrapper to enable dynamic configuration reloading without restart. Config changes are detected via file modification time checks during each status update cycle, allowing near-real-time updates without external dependencies or complex event handling.

### Update Cycle Integration

```
Every 30 seconds:
  1. Check config file mtime (main.go:checkAndReloadIfNeeded)
  2. If changed: Load & Validate -> Atomic swap
  3. Fetch server info with current config
  4. Update Discord embed
```

Config reload checked at the start of each update cycle, before server polling.

### Reload Flow

```
Config file modified
  -> checkAndReloadIfNeeded() detects mtime change
  -> scheduleReload() starts 100ms debounce timer
  -> performReload() loads and validates new config
  -> atomic.Value.Store() swaps config atomically
  -> Next update cycle uses new config
```

**Debouncing:** Text editors create multiple write events during save. The 100ms debounce timer batches these writes into a single reload attempt, preventing CPU waste and potential race conditions. Still provides near-instant updates from admin perspective.

### Thread-Safety Strategy

**Read-heavy workload:** Config accessed on every server query (every 30 seconds for all servers).
**Write-light workload:** Config changes rarely (manual admin edits).

- **atomic.Value** stores config pointer for lock-free reads in hot path
- **sync.RWMutex** protects reload operations (serialized writes)
- Multiple goroutines can call GetConfig() simultaneously during server polling
- atomic.Value ensures atomic swap between old and new config (no partial state)

### Validation Failure Recovery

Invalid config never replaces valid config. Bot continues operating with last known good config.

**Failure path:**
```
Config file modified
  -> checkAndReloadIfNeeded() loads file
  -> validateConfigStructSafeRuntime() returns error
  -> Error logged: "config validation failed: <details>"
  -> Old config remains active
  -> Admin fixes config file
  -> Next update cycle retries reload
```

**Validation rules** (from validateConfigStructSafeRuntime in main.go):
- `server_ip` must be non-empty
- `update_interval` must be >= 1 second
- `category_order` must be non-empty array
- All categories in `category_order` must have emoji in `category_emojis`
- All servers must have non-empty name, valid port (1-65535), and valid category
- Server category must exist in `category_order`

### ConfigManager Structure

```go
type ConfigManager struct {
    config        atomic.Value // stores *Config (lock-free reads)
    configPath    string
    lastModTime   time.Time
    mu            sync.RWMutex
    debounceTimer *time.Timer  // Debounces rapid file writes
}
```

**Key methods:**
- `GetConfig() *Config` - Lock-free read via atomic.Value.Load()
- `checkAndReloadIfNeeded() error` - Called every update cycle, checks mtime
- `scheduleReload()` - Starts 100ms debounce timer on file change
- `performReload() error` - Loads, validates, and atomically swaps config
- `Cleanup()` - Stops debounce timer during shutdown (called from Bot.WaitForShutdown)

### Invariants

**Config consistency:**
- All config reads see a complete, valid config (never partial state)
- atomic.Value ensures atomic swap between old and new config
- Validation runs before swap, never after

**File watching bounds:**
- Config mtime checked every 30 seconds (update_interval)
- No more than one reload attempt per 30-second window (per update cycle)
- Failed reloads don't affect running bot

**Error recovery:**
- Invalid config never replaces valid config
- Bot continues operating on last known good config
- All errors logged but never crash the bot

## Operational Procedures

### Detecting Config Reload Failures

When a config file change is not applied, check the bot logs for these patterns:

```
config validation failed: <error details>
failed to reload config: <error reason>
```

**Verification steps:**
1. Check log for "Config reloaded successfully" after file modification
2. Verify bot behavior reflects new config (servers appear/disappear in embed)
3. If no success log within 30 seconds, check for error messages above

**Common failures:**
- **JSON syntax error:** "failed to parse config" - Use `jq . config.json` to validate syntax
- **Missing field:** "server_ip cannot be empty" - Ensure all required fields present
- **Invalid port:** "invalid port: 70000 (valid range: 1-65535)"
- **Unknown category:** "category 'X' which is not defined in category_order"

**Recovery procedure:**
1. Fix the config file error (use `jq . config.json` to validate JSON syntax)
2. Wait for next update cycle (max 30 seconds)
3. Verify log shows "Config reloaded successfully"
4. Check Discord embed reflects new configuration

### Troubleshooting Config Reload

**Config changes not applied:**
- Symptom: Config file modified but Discord embed doesn't update
- Diagnosis: `tail -f /var/log/ac-discordbot.log | grep -E "(Config reloaded|config validation)"`
- Expected logs: "Config reloaded successfully" or "config validation failed: <error>"
- Common causes: JSON syntax error, invalid port, missing required field, unknown category

**Config reload loop:**
- Symptom: Continuous "Config reloaded successfully" messages
- Cause: File system modification time issues (network mounts, time sync)
- Resolution: Check `stat config.json` stability, consider local file instead of network mount

**No health check endpoint:**
- Bot does not expose HTTP endpoints for config health checks
- Monitor logs to verify config reload status
- Use `grep` or log aggregation to detect reload failures

**Log monitoring example:**
```bash
# Alert on validation failures
tail -f bot.log | grep --line-buffered "config validation failed" | while read line; do
  echo "ALERT: $line"
  # Send alert (email, Slack, etc.)
done
```

### Testing Config Reload

**Manual testing:**
```bash
# Terminal 1: Start bot
go run main.go

# Terminal 2: Modify config (triggers reload within 30s)
vim config.json

# Terminal 1: Watch for "Config reloaded successfully" log
# Verify new servers appear in Discord embed
```

**Validation failure recovery:**
```bash
# Terminal 1: Start bot
go run main.go

# Terminal 2: Break config with invalid JSON
echo '{"invalid": json}' > config.json

# Terminal 1: Watch for "config validation failed" error
# Bot continues running with old config

# Terminal 2: Fix config
vim config.json  # Fix the JSON

# Terminal 1: Watch for "Config reloaded successfully"
```

## Design Decisions

### Polling vs Event-Driven Config Watching

**Decision:** Use file modification time polling (every 30 seconds) instead of fsnotify-based event watching.

**Rationale:**
- Bot already has 30-second update loop → adding file mtime check leverages existing cycle
- fsnotify adds external dependency and ~150 LOC of event handling code
- Polling keeps single-file architecture simple
- 30-second latency acceptable for admin-triggered config changes (not time-critical)
- Event-driven approach would require separate goroutine and coordination complexity

**Tradeoff:** Max 30-second delay before detecting config changes, but eliminates external dependency and reduces code complexity.

### Read-Only Config Access

**Decision:** Bot detects config changes but never writes config file.

**Rationale:**
- User requirement confirmed: read-only access
- Simpler implementation (no file locking or write coordination)
- Architecture supports adding write capability later without breaking changes
- Eliminates risk of bot corrupting config file

**Tradeoff:** Cannot persist config changes from runtime, but meets current requirements and maintains simplicity.

### Atomic Config Swap vs Partial Updates

**Decision:** Atomic config swap (full config replacement) instead of field-by-field hot reload.

**Rationale:**
- Full config duplicated in memory during reload (old + new)
- Atomic swap requires separate config instances
- Config is small (~1KB), memory cost negligible
- Avoids complex partial update logic and inconsistent state risk
- atomic.Value provides lock-free reads during swap

**Tradeoff:** Memory overhead of full config copy, but eliminates merge logic complexity and ensures consistency.

### Validation Behavior: Startup vs Runtime

**Decision:** Two validation functions with different failure modes.

**Startup behavior** (validateConfigStruct in main.go):
- Uses `log.Fatalf` for ALL validation failures
- Terminates bot immediately on invalid config
- Fail-fast: prevents bot from starting with bad config

**Runtime behavior** (validateConfigStructSafeRuntime in main.go):
- Returns error instead of calling `log.Fatalf`
- Safe for runtime validation during config reload
- Bot continues operating with old config on validation failure
- Admin can fix config and retry without bot downtime

**Rationale:** Startup validation should be strict (no bad config allowed), but runtime reload should be forgiving (bot must continue operating). This separation prevents a single config typo from breaking production deployment.

## Dependencies

- `github.com/bwmarrin/discordgo` - Discord API bindings
- Standard library packages: `net/http`, `context`, `encoding/json`, `time`, `sync`, `os`, `atomic`
