# Docker / Podman

This Dockerfile uses [uv](https://github.com/astral-sh/uv) for fast Python package management.

**Note:** We prefer `podman` for local development. All commands below use `podman`, but you can use `docker` as an alias if needed. The CI/CD pipeline uses Docker for production builds.

## Build

```bash
podman build -t ac-discordbot .
```

## Security

This container runs as a non-root user (UID 1001) following container security best practices. At runtime, the application will immediately refuse to start if run as root (UID 0). It does not enforce config file/directory permissions at runtime. Make sure the absabot user (UID 1001) can read config.json; see troubleshooting below if startup fails on file access.

### User Details
- **Username**: absabot
- **UID/GID**: 1001/1001
- **Shell**: /sbin/nologin (no login access)
- **Permissions**: Read-only access to application code

The bot requires no file write permissions as it only reads environment variables and makes HTTP requests.

### Verify Non-Root Execution

```bash
podman exec ac-discordbot whoami
# Should output: absabot
```

## Run

### Configuration via Volume Mount

The bot loads `config.json` from the data directory (`/data/`) at startup.
Host can edit server configuration via volume mount without container rebuild.

**Option 1: Mount single config file (recommended - simpler)**
```bash
# Create config file on host
mkdir -p /path/to/config
cp config.json.example /path/to/config/config.json
nano /path/to/config/config.json

# Ensure non-root container user (UID 1001) can read
sudo chown 1001:1001 /path/to/config/config.json
sudo chmod 644 /path/to/config/config.json

# Run with file mount
podman run -d \
  --name ac-discordbot \
  -e DISCORD_TOKEN="your_bot_token_here" \
  -e CHANNEL_ID="your_channel_id" \
  -v /path/to/config/config.json:/data/config.json:ro \
  --restart unless-stopped \
  ac-discordbot
```

**Option 2: Mount working directory (for multiple config files)**
```bash
# Create directory with config file inside
mkdir -p /path/to/config
cp config.json.example /path/to/config/config.json
nano /path/to/config/config.json
sudo chown -R 1001:1001 /path/to/config
sudo chmod 755 /path/to/config

# Run with directory mount
podman run -d \
  --name ac-discordbot \
  -e DISCORD_TOKEN="your_bot_token_here" \
  -e CHANNEL_ID="your_channel_id" \
  -v /path/to/config:/data:ro \
  --restart unless-stopped \
  ac-discordbot
```

The `:ro` flag makes the mount read-only for additional security. To edit configuration:
1. Edit config file on host
2. Restart the container: `podman restart ac-discordbot`
3. Bot loads the new configuration on startup

## Using Docker Compose

Create `docker-compose.yml`:

```yaml
services:
  bot:
    build: .
    container_name: ac-discordbot
    environment:
      - DISCORD_TOKEN=${DISCORD_TOKEN}
      - CHANNEL_ID=${CHANNEL_ID}
    volumes:
      - ./config.json:/data/config.json:ro
    restart: unless-stopped
```

Then create a `.env` file with your credentials and run:

```bash
podman-compose up -d
```

## Logs

```bash
podman logs -f ac-discordbot
```

## Stop

```bash
podman stop ac-discordbot
podman rm ac-discordbot
```

## Troubleshooting

### Permission Denied Errors

If the bot cannot read config.json:

```bash
# Check container user ID
podman exec ac-discordbot id
# Output: uid=1001(absabot) gid=1001(absabot)

# Fix host file permissions (for single file mount)
sudo chown 1001:1001 /path/to/config/config.json
# OR for directory mount:
sudo chown -R 1001:1001 /path/to/config
```

### Config File Not Found

Check container logs for the absolute path being searched:

```bash
podman logs ac-discordbot | grep "Loading config"
```

Common issues:
- Volume mount path must match: `/data/config.json` (file) or `/data/` (directory)
- Host path must be absolute (not relative to current directory)
- Config file name must be exactly `config.json` (case-sensitive)
