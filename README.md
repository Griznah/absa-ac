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

## Running Locally

1. Set required environment variables:

```bash
export DISCORD_TOKEN="your_bot_token_here"
export CHANNEL_ID="your_channel_id"
export SERVER_IP="your.server.ip"
```

Or create a `.env` file and source it.

2. Install dependencies:

```bash
go mod download
```

3. Run the bot:

```bash
go run main.go
```

Or build and run:

```bash
go build -o bot .
./bot
```

## Configuration

**Required environment variables:**

- `DISCORD_TOKEN` - Your Discord bot token
- `CHANNEL_ID` - The Discord channel ID where status messages will be posted
- `SERVER_IP` - Base IP address for Assetto Corsa servers

## Deployment

### Podman (Recommended)

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

### Docker

The `Containerfile` is compatible with Docker:

```bash
docker build -t ac-discordbot .
docker run -d \
  --name ac-discordbot \
  -e DISCORD_TOKEN="your_token" \
  -e CHANNEL_ID="your_channel_id" \
  -e SERVER_IP="your.server.ip" \
  --restart unless-stopped \
  ac-discordbot
```

### CI/CD

The bot uses GitHub Actions to automatically build and push Docker images to GitHub Container Registry (GHCR) on version tags (`v*.*.*`).

Available images: `ghcr.io/{owner}/ac-discordbot:latest`

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

Edit the `servers` slice in `main.go`:

```go
{
    Name:     "Server Name",
    Port:     8091,
    Category: "Drift",  // or "Touge", "Track"
},
```

The `IP` field is automatically set from the `SERVER_IP` environment variable.

## Dependencies

- `github.com/bwmarrin/discordgo` - Discord API bindings
- Standard library packages: `net/http`, `context`, `encoding/json`, `time`, `sync`
