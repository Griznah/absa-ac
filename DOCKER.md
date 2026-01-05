# Docker / Podman

This Dockerfile uses [uv](https://github.com/astral-sh/uv) for fast Python package management.

**Note:** We prefer `podman` for local development. All commands below use `podman`, but you can use `docker` as an alias if needed. The CI/CD pipeline uses Docker for production builds.

## Build

```bash
podman build -t ac-discordbot .
```

## Security

This container runs as a non-root user (UID 1001) following container security best practices.

### User Details
- **Username**: botuser
- **UID/GID**: 1001/1001
- **Shell**: /sbin/nologin (no login access)
- **Permissions**: Read-only access to application code

The bot requires no file write permissions as it only reads environment variables and makes HTTP requests.

### Verify Non-Root Execution

```bash
podman exec ac-discordbot whoami
# Should output: botuser
```

## Run

```bash
podman run -d \
  --name ac-discordbot \
  -e DISCORD_TOKEN="your_bot_token_here" \
  -e CHANNEL_ID="your_channel_id" \
  -e SERVER_IP="your.server.ip" \
  --restart unless-stopped \
  ac-discordbot
```

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
      - SERVER_IP=${SERVER_IP}
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
