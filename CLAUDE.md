# AC Discord Bot

Discord bot for monitoring Assetto Corsa racing servers with dynamic configuration reloading.

## Files

| File | What | When to read |
| ---- | ---- | ------------ |
| `README.md` | Complete documentation: architecture, deployment, migration guide, troubleshooting, operational procedures | Understanding how the bot works, deploying, debugging issues, learning config reload design |
| `main.go` | Monolithic bot implementation: types, config loading (with dynamic reload), server fetching, Discord integration, update loop | Understanding architecture, modifying behavior, adding features |
| `main_test.go` | Unit tests for config validation, ConfigManager, and reload behavior | Verifying changes, adding tests, debugging reload logic |
| `config.json.example` | Template for server configuration | Setting up new deployment, understanding config schema |
| `.env.example` | Environment variable template (DISCORD_TOKEN, CHANNEL_ID) | Setting up local development, configuring deployment |
| `Containerfile` | Container image definition with Go static binary | Building containers, deployment, understanding runtime |
| `PODMAN.md` | Podman-specific deployment instructions and examples | Deploying with Podman, understanding container setup |
| `go.mod` | Go module dependencies and version pinning | Updating dependencies, checking versions |
| `.gitignore` | Git ignore patterns (binaries, config files, IDE files) | Understanding what's excluded from version control |
| `.github/workflows/docker-publish.yml` | CI/CD pipeline for automated builds | Understanding release process, modifying build workflow |

## Subdirectories

| Directory | What | When to read |
| --------- | ---- | ------------ |
| `.github/` | GitHub Actions workflows for automated container builds | Setting up CI/CD, understanding release automation |
| `plans/` | Working planning documents for executed features | Understanding implementation history, decision rationale for past changes |

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
