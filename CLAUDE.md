# AC Discord Bot

Discord bot for monitoring Assetto Corsa racing servers with dynamic configuration reloading.

## Files

| File | What | When to read |
| ---- | ---- | ------------ |
| `main.go` | Monolithic bot implementation: types, config loading, server fetching, Discord integration, update loop | Understanding architecture, modifying behavior, adding features |
| `main_test.go` | Unit tests for config validation, ConfigManager, and reload behavior | Verifying changes, adding tests, debugging reload logic |
| `config.json.example` | Template for server configuration | Setting up new deployment, understanding config schema |
| `Containerfile` | Container image definition with Go static binary | Building containers, deployment, understanding runtime |
| `go.mod` | Go module dependencies and version pinning | Updating dependencies, checking versions |
| `.github/workflows/docker-publish.yml` | CI/CD pipeline for automated builds | Understanding release process, modifying build workflow |

## Subdirectories

| Directory | What | When to read |
| --------- | ---- | ------------ |
| `.github/` | GitHub Actions workflows and issue templates | Setting up CI/CD, contributing guidelines |

## Build

```bash
go build -o bot .
```

## Test

```bash
go test -v ./...                         # Run all tests
go test -v -run TestConfigReload         # Test config reload specifically
go test -v -run TestConfigManager        # Test ConfigManager behavior
```

## Development

**Environment setup:**
```bash
go mod download                           # Install dependencies
export DISCORD_TOKEN="your_token"
export CHANNEL_ID="your_channel_id"
```

**Running locally:**
```bash
go run main.go                           # Uses ./config.json
go run main.go -c /path/to/config.json   # Uses specified config
```

**Config reload testing:**
```bash
# Terminal 1: Start bot
go run main.go

# Terminal 2: Modify config
vim config.json

# Terminal 1: Watch for "Config reloaded successfully" log
```

**Formatting:**
```bash
gofmt -l .                               # Check formatting
gofmt -w .                               # Format code
```
