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

## Dependencies

- `github.com/bwmarrin/discordgo` - Discord API bindings
- Standard library packages: `net/http`, `context`, `encoding/json`, `time`, `sync`
