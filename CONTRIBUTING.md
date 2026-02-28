# Contributing to AC Discord Bot

Thank you for your interest in contributing! This guide will help you set up local development.

## Prerequisites

- **Go 1.25.5+** - [Install Go](https://golang.org/doc/install)
- **Git** - [Install Git](https://git-scm.com/)
- **Discord Bot Token** - See [Discord Bot Setup](#discord-bot-setup) below
- **Optional**: Docker/Podman for containerized testing

## Discord Bot Setup

1. Go to [Discord Developer Portal](https://discord.com/developers/applications)
2. Click "New Application" and give it a name
3. Navigate to **Bot** → **Add Bot**
4. Under "Privileged Gateway Intents", enable:
   - Message Content Intent
5. Click "Reset Token" to generate a token → save this as `DISCORD_TOKEN`
6. Go to **OAuth2** → **URL Generator**
7. Select "bot" scope and these permissions:
   - Send Messages
   - View Channels
8. Use the generated URL to invite the bot to your test server
9. Enable Developer Mode in Discord (User Settings → Advanced) to right-click copy channel IDs

## Quick Start

### 1. Fork and Clone

```bash
git clone https://github.com/YOUR_USERNAME/absa-ac.git
cd absa-ac
```

### 2. Install Dependencies

```bash
go mod download
```

### 3. Configure Environment

```bash
# Create environment file
cp .env.example .env

# Edit with your values
nano .env
```

Required variables:
- `DISCORD_TOKEN` - Your Discord bot token
- `CHANNEL_ID` - Channel ID for status updates

### 4. Create Config File

```bash
cp config.json.example config.json
nano config.json  # Customize your servers
```

### 5. Run the Bot

```bash
go run main.go
```

## Development

### Running Tests

```bash
# All tests
go test -v ./...

# Specific tests
go test -v -run TestConfigReload
go test -v ./api/...

# With coverage
go test -cover ./...

# Benchmarks
go test -v ./api/ -bench=. -benchmem
```

### Code Style

Format your code before committing:

```bash
gofmt -w .
```

### IDE Setup

**VS Code** recommended extensions:
- Go (`golang.go`)

**GoLand/IntelliJ**:
- Go plugin (pre-installed in GoLand)

### Debugging

Enable verbose output to troubleshoot issues:

```bash
go run main.go -v
```

Common issues:

| Problem | Solution |
|---------|----------|
| "Invalid token" | Verify `DISCORD_TOKEN` is correct and not expired |
| "Unknown channel" | Check `CHANNEL_ID` and ensure bot has access to the channel |
| "Unauthorized" API calls | Confirm `API_BEARER_TOKEN` matches in request header |
| Config not reloading | Ensure config.json is valid JSON (check for trailing commas) |

### Commit Messages

Use conventional commits format:

```
<type>(<scope>): <subject>

Types: feat | fix | docs | style | refactor | test | chore | perf
```

Examples:
- `feat(api): add rate limiting endpoint`
- `fix(config): handle missing categories gracefully`
- `docs: update deployment instructions`

### Branch Naming

- `feature/description` - New features
- `fix/description` - Bug fixes
- `docs/description` - Documentation updates
- `refactor/description` - Code refactoring

## Security

**Never commit secrets:**
- Discord tokens
- API bearer tokens
- Passwords or credentials

Run security checks before submitting:

```bash
# Secret scanning
trufflehog filesystem . --regex --entropy=False

# Vulnerability scanning
govulncheck ./...
```

## API Development

To work on the REST API:

```bash
export API_ENABLED=true
export API_PORT=3001
export API_BEARER_TOKEN="your-secure-token-at-least-32-chars"
export API_CORS_ORIGINS="http://localhost:3000"
```

API tests:
```bash
go test -v ./api/ -run TestBearerAuth
go test -v ./api/ -run TestRateLimit
```

## Project Structure

```
.
├── main.go           # Bot implementation
├── main_test.go      # Unit tests
├── api/              # REST API package
│   ├── server.go     # HTTP server
│   ├── handlers.go   # Request handlers
│   ├── middleware.go # Auth, CORS, rate limiting
│   ├── csrf.go       # CSRF token generation
│   ├── routes.go     # Route registration
│   ├── response.go   # Common response types
│   └── web/admin/    # Admin frontend
├── config.json       # Server configuration (gitignored)
├── config.json.example
├── .env              # Environment variables (gitignored)
└── .env.example
```

## Pull Request Process

1. Fork the repository
2. Create a feature branch
3. Make your changes with tests
4. Ensure all tests pass: `go test -v ./...`
5. Format code: `gofmt -w .`
6. Commit with conventional message
7. Push and create a pull request

**CI/CD:** Automated security scans (trufflehog, govulncheck) and container builds run on version tags (`v*.*.*`). Run the security checks locally before submitting to catch issues early.

## Getting Help

- Check [README.md](README.md) for full documentation
- Open an issue for questions or bugs
- Review [SECURITY.md](SECURITY.md) for security concerns

## License

By contributing, you agree that your contributions will be licensed under the same license as the project.
