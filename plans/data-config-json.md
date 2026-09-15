# Simplify Config Path to /data/config.json Only — Decision Record

Removed `./config.json` fallback from `loadConfig` and `getConfigPath`. Default path is `/data/config.json` only; `-c`/`--config` flag behavior unchanged.

Status: implemented. Code and git history are the implementation record; this file preserves design rationale.

## Decisions

| ID | Decision | Reasoning |
|----|----------|-----------|
| DL-001 | Remove ./config.json fallback, use only /data/config.json as default | Container deployments use /data/ mount point -> ./config.json fallback adds complexity for non-container use -> Users outside containers can use -c flag -> Simplifies logic and reduces cognitive load |
| DL-002 | Keep getConfigPath function synchronized with loadConfig | getConfigPath determines which file to watch for hot-reload -> Must match loadConfig fallback logic exactly -> Removing ./config.json fallback from both maintains consistency |

## Rejected Alternatives

| ID | Alternative | Reason Rejected |
|----|-------------|-----------------|
| RA-001 | Add constant for default path /data/config.json | Overkill for single usage - the path is only referenced in two functions and adding a constant adds indirection without clarity benefit |
| RA-002 | Keep fallback but log deprecation warning | Adds complexity for transitional period that is not needed - this is an internal tool with controlled deployments |

## Invariants

- `ConfigManager.GetConfig()` returns nil when no config loaded (no-config-at-startup)
- Missing config file returns nil, not error (graceful handling)
- Hot-reload polling continues to work when config appears

## Tradeoff

Chose simplicity over flexibility: single default path; non-container users must use `-c` flag. Acceptable for container-first design.
