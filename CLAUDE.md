# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

This is a Discord bot for monitoring Assetto Corsa racing servers. It polls multiple servers every 30 seconds and displays their status (player counts, current maps, online/offline status) in a Discord channel using rich embeds. The bot is intentionally designed as a single-file application for simplicity.

**Architecture:** Monolithic Python bot (~245 lines in `bot.py`) with async/await patterns, Docker deployment, and GitHub Actions CI/CD.

## Development Commands

### Environment Setup

Using `uv` (recommended, much faster than pip):
```bash
uv venv
source .venv/bin/activate
uv sync --all-extras  # Install dev dependencies too
```

Or with pip:
```bash
python -m venv .venv
source .venv/bin/activate
pip install -e .
```

### Running the Bot

Locally:
```bash
python bot.py
```

With Podman (Local Development):
```bash
podman build -t ac-discordbot .
podman run -d \
  --name ac-discordbot \
  -e DISCORD_TOKEN="your_token" \
  -e CHANNEL_ID="your_channel_id" \
  -e SERVER_IP="your.server.ip" \
  --restart unless-stopped \
  ac-discordbot
```

### Testing and Quality

```bash
pytest                           # Run tests (pytest with async support)
ruff check .                     # Lint
ruff format .                    # Format code
```

## Code Architecture

### Single-File Structure

The entire bot lives in `bot.py` with clear section headers:
- **CONFIG** - Environment variables and server configuration
- **BOT SETUP** - Discord bot initialization with intents
- **SERVER FETCH** - Async server info fetching with error handling
- **EMBED BUILDER** - Discord embed construction from server data
- **TASK LOOP** - Periodic update task (30-second intervals)
- **EVENTS** - Bot lifecycle events
- **RUN** - Bot startup and validation

### Key Patterns

**Async Parallel Fetching:** Uses `asyncio.gather()` to query all servers concurrently:
```python
server_infos = await asyncio.gather(*(get_server_info(session, s) for s in SERVERS))
```

**Global State Management:** Single global variable `server_message` tracks the Discord message to edit (rather than spamming new messages).

**Graceful Degradation:** Server fetch failures return offline status instead of crashing:
```python
try:
    # fetch server info
except:
    return {..."map": "Offline", "num_players": -1}
```

**Message Recovery:** If the status message is deleted, the bot automatically creates a new one.

### Configuration System

**Command-Line Flags:**
- `-c, --config`: Path to config.json file (optional)

**Config File Loading Priority:**
1. Explicit path via `-c`/`--config` flag (if provided)
2. `/data/config.json` (for containerized deployments)
3. `./config.json` (for local development)

The `loadConfig()` function in `main.go` implements this fallback logic with comprehensive error reporting if all paths fail.

**Environment Variables** (required):
- `DISCORD_TOKEN` - Bot authentication token
- `CHANNEL_ID` - Target channel for status messages (as integer)

**JSON Configuration:** Loaded from `config.json` with the following structure:
- `server_ip` - Base IP for Assetto Corsa servers
- `update_interval` - Seconds between status updates
- `category_order` - Array defining display order
- `category_emojis` - Map of category to emoji
- `servers` - Array of server objects with name, port, category

**Server Configuration:** Each server in the `servers` array has:
- `name` - Display name
- `port` - HTTP query port (different from game port)
- `category` - One of: "Drift", "Touge", "Track"

### Discord Integration Details

**No Command System:** This bot doesn't use commands or slash commands. It only does automatic status updates via a background task loop.

**Embed Structure:**
- Thumbnail: Norwegian flag (ABSA branding)
- Title: "ABSA Official Servers"
- Description: Total player count
- Fields: Grouped by category (Drift, Touge, Track)
  - Category headers with emoji and total players
  - Individual servers with status emoji, map, player count, join link
- Footer: Update interval
- Image: Logo from `http://{SERVER_IP}/images/logo.png`

**Join Links:** Uses `acstuff.club` service for direct server joining:
```
https://acstuff.club/s/q:race/online/join?ip={ip}&httpPort={port}
```

### Server Query Protocol

The bot queries Assetto Corsa servers via HTTP:
- Endpoint: `http://{ip}:{port}/info`
- Response JSON contains: `clients`, `maxclients`, `track`
- Timeout: 2 seconds per server
- Track name uses `os.path.basename()` to extract just the filename

## Python Version Quirk

**Critical:** This project requires Python 3.12.12 (pinned in `pyproject.toml`). The bot includes a workaround for Python 3.12+ asyncio changes:
```python
asyncio.set_event_loop(asyncio.new_event_loop())
```

## Deployment

### Docker

Uses `uv` for fast multi-stage builds. The Dockerfile:
- Base: `python:3.12-slim`
- Copies `uv` from official image for dependency installation
- Creates virtual environment and syncs dependencies
- Copies only `bot.py` (minimal attack surface)
- Runs with `.venv/bin/python bot.py`

### CI/CD

GitHub Actions workflow (`.github/workflows/docker-publish.yml`):
- Triggers on version tags (`v*.*.*`)
- Builds and pushes to GitHub Container Registry (GHCR)
- Creates multiple tags: version, major.minor, latest
- Uses BuildKit cache for faster builds

Images available at: `ghcr.io/{owner}/ac-discordbot`

## Code Style

**Ruff Configuration:**
- Line length: 100 characters
- Target: Python 3.12
- Rules: E (errors), F (pyflakes), W (warnings), I (import sorting), N (naming)
- Ignores: E501 (line length conflicts with formatter)

## Common Modification Patterns

**Adding a Server:** Add to `SERVERS` list in `bot.py`:
```python
{
    "name": "Server Name",
    "ip": SERVER_IP,
    "port": 8091,
    "category": "Drift",  # or "Touge", "Track"
}
```

**Changing Update Interval:** Modify `UPDATE_INTERVAL` in `bot.py` (in seconds).

**Adding a Category:** Add to `CATEGORY_ORDER` and `CATEGORY_EMOJIS` dicts.

**Modifying Embed Layout:** Edit the `build_embed()` function, which constructs the Discord embed field by field.
