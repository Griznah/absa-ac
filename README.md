# AC Discord Bot

Discord bot for monitoring and displaying status information for Assetto Corsa racing servers.

## Features

- Real-time server status monitoring
- Player count tracking across multiple server categories
- Direct join links via acstuff.club
- Automatic status updates every 30 seconds
- Server categories: Drift, Touge, Track

## Installation

Using [uv](https://github.com/astral-sh/uv):

```bash
uv venv
source .venv/bin/activate
uv sync
```

Or with pip:

```bash
python -m venv .venv
source .venv/bin/activate
pip install -e .
```

## Configuration

**Required:** Create a `.env` file or set environment variables:

```bash
DISCORD_TOKEN=your_bot_token_here
CHANNEL_ID=your_channel_id
SERVER_IP=your.server.ip
```

Or export them directly:

```bash
export DISCORD_TOKEN="your_bot_token_here"
export CHANNEL_ID="your_channel_id"
export SERVER_IP="your.server.ip"
```

## Running

```bash
python bot.py
```

## Development

Install development dependencies:

```bash
uv sync --all-extras
```

Run tests:

```bash
pytest
```

Format code:

```bash
ruff check .
ruff format .
```
