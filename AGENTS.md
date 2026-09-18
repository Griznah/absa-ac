# AC Discord Bot + REST API

Monitors Assetto Corsa servers, posts to Discord. Dynamic config reload. Optional REST API for runtime config management.

## Files

| File | What |
| ---- | ---- |
| `README.md` | Full docs: architecture, deployment, troubleshooting, REST API |
| `main.go` | Bot core: types, config load/reload (default /data/config.json, `-c` override, no-config startup), server fetch, Discord, update loop; wires `api.Server` and `pkg/proxy` |
| `main_test.go` | Config validation, ConfigManager, reload tests |
| `config.json.example` | Config schema |
| `Containerfile` | Container image, Go static binary |
| `.env.example` | Env vars: DISCORD_TOKEN, CHANNEL_ID, API_* |
| `test_*.sh` | Fail-fast startup validation + cleanup scripts |

## Subdirectories

| Directory | What |
| --------- | ---- |
| `.github/workflows/` | CI/CD: container build + publish, security scan |
| `api/` | REST API server, middleware, admin UI — see `api/AGENTS.md` |
| `pkg/` | Shared packages |
| `plans/` | Decision records per feature — see `plans/AGENTS.md` |

## Process
- All work on separate branches
- Before building anything non-trivial, use `pi-plans` to make a plan and review the plan before presenting it to the user
- Use relevant skills to review code before creating PRs
- After opening a PR on GitHub wait for external AI reviewers

## Build

```bash
go build -o bot .
```

## Test

```bash
go test -v ./...                      # all
go test -v ./api/...                  # API package (unit, E2E, benchmarks)
go test -v ./api/ -bench=. -benchmem  # benchmarks
```

## Run

```bash
export DISCORD_TOKEN="..." CHANNEL_ID="..."
go run main.go -c config.json         # default path: /data/config.json
```

## Format

```bash
gofmt -l .   # check
gofmt -w .   # fix
```
