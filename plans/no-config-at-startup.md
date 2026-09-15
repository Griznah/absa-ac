# No-Config-at-Startup — Decision Record

Bot starts without config.json instead of `log.Fatalf`: returns nil config, waits for file creation or API write, begins normal operation when valid config appears.

Status: implemented. Code and git history are the implementation record; this file preserves design rationale.

## Decisions

| ID | Decision | Reasoning |
|----|----------|-----------|
| DL-001 | Return nil config instead of os.Exit(1) on startup | Current behavior: `loadConfig()` returns error, causing `log.Fatalf` in `main()` -> Container orchestration needs config mounted after bot starts -> Implementation: Return nil from `loadConfig`, log warning, create ConfigManager with nil config -> Hot-reload: Existing polling mechanism continues to work when config appears |
| DL-002 | Update loop logs and skips when config missing | User preference: Log + Skip on each cycle -> Provides visibility into bot state -> Simpler than state machine for tracking 'already logged' |
| DL-003 | GetConfig returns nil pointer, all callers must check | `atomic.Value` stores `*Config` pointer -> Type assertion on nil panics -> Store nil pointer explicitly -> `GetConfig` returns nil -> Callers must check `cfg != nil` before field access |

## Rejected Alternatives

| ID | Alternative | Reason Rejected |
|----|-------------|-----------------|
| RA-001 | Create minimal config with hardcoded defaults | Hardcoded defaults don't match flexible runtime use case. Users expect containerized deployments where config may be mounted at runtime via secrets or init containers. |
| RA-002 | Require config file to exist, return error and recreate placeholder config | Requiring file to exist breaks the hot-reload pattern. API can still write config via `WriteConfig`/`UpdateConfig`. Adds complexity. |

## Invariants

- `ConfigManager.GetConfig()` always returns valid config (or nil if no config loaded)
- Missing config: bot stays running, waiting for config
- Invalid config: never replaces valid config
