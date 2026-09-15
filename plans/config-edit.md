# Admin UI Config Editor — Decision Record

Admin UI originally edited `name`/`url` fields, but `url` does not exist in the Server struct (name, port, category). Aligned frontend fields with the actual struct and added global config editing (server_ip, update_interval, category_order, category_emojis).

Status: implemented. Code and git history are the implementation record; this file preserves design rationale.

## Decisions

| ID | Decision | Reasoning Chain |
|---|---|---|
| DL-001 | Align UI fields with actual Server struct: name, port, category (remove url) | Current UI has url field that does not exist in Server struct -> Server has Name, Port, Category per main.go:L172-L177 -> UI must match struct to send valid payloads |
| DL-002 | Add global config fields: server_ip, update_interval, category_order, category_emojis | config.json.example shows these top-level fields -> Backend validates them in validateConfigStructSafeRuntime -> UI should allow editing all config fields for completeness |
| DL-003 | Category dropdown populated from category_order, stored as category string | Server.category must exist in category_order per validation -> Dropdown prevents invalid categories -> Simpler UX than free-text input |
| DL-004 | Use existing XSS prevention pattern (escapeHtml via textContent) | RSK-001 established escapeHtml pattern -> Continue using textContent/innerHTML round-trip for all user content |

## Invariants

- All user content rendered through escapeHtml function
- Server struct fields: Name, IP, Port, Category (IP set globally from server_ip)
- Config validation requires category in category_order and emoji in category_emojis

## Tradeoffs

- Category dropdown limits to existing categories (cannot add new category inline)
- No client-side port range validation (relies on backend validation)
