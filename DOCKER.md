# Docker

This Dockerfile uses [uv](https://github.com/astral-sh/uv) for fast Python package management.

## Build

```bash
docker build -t ac-discordbot .
```

## Run

```bash
docker run -d \
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
docker-compose up -d
```

## Logs

```bash
docker logs -f ac-discordbot
```

## Stop

```bash
docker stop ac-discordbot
docker rm ac-discordbot
```
