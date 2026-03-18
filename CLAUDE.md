# Legato

for all work in this project, use the tdd skill, if present.

AI Agent Orchestration TUI — a keyboard-driven kanban board for Jira tickets, built for developers who work with AI coding agents.

## Tech Stack

- **Language**: Go 1.23+
- **TUI**: Bubbletea, Lipgloss (styling), Glamour (markdown rendering)
- **Database**: SQLite via `modernc.org/sqlite` (pure Go, no CGO), `sqlx` for queries
- **Config**: YAML via `gopkg.in/yaml.v3`, XDG-compliant paths
- **Module path**: `github.com/cpave3/legato`

## Architecture

3-layer architecture with strict import rules:

```
cmd/legato/     → wires everything (imports all layers)
internal/tui/   → presentation (imports service/ via interfaces, never engine/)
internal/service/ → business logic (imports engine/, never tui/)
internal/engine/  → infrastructure (imports only stdlib + 3rd-party)
config/           → configuration (standalone, no internal imports)
```

**Import rules are enforced by convention, not tooling.** engine/ must never import service/ or tui/. service/ must never import tui/.

## Commands

```bash
go test ./...              # run all tests
go test -race ./...        # run with race detector (use for event bus changes)
go run ./cmd/legato        # run the app (currently a smoke test)
go build ./...             # build all packages
go vet ./...               # static analysis
```

## Key Packages

- `internal/engine/store/` — SQLite store with embedded migrations, ticket/column mapping/sync log CRUD
- `internal/engine/events/` — Channel-based event bus with buffered pub/sub (buffer size 64, non-blocking drops on full)
- `config/` — YAML config parser with env var expansion (`os.ExpandEnv`) and XDG path resolution

## Database

- SQLite file location: `cfg.DB.Path` > `$XDG_DATA_HOME/legato/legato.db` > `~/.local/share/legato/legato.db`
- Migrations embedded via `embed.FS`, tracked with `PRAGMA user_version`
- WAL mode enabled, foreign keys ON
- Schema: `tickets`, `column_mappings`, `sync_log` tables (see `internal/engine/store/migrations/001_init.sql`)

## Config

- Config file location: `$LEGATO_CONFIG` > `$XDG_CONFIG_HOME/legato/config.yaml` > `~/.config/legato/config.yaml`
- Missing config file returns defaults (no error) — app starts without config for initial setup
- Env vars expanded before YAML parsing: `${LEGATO_JIRA_TOKEN}` works in config values

## Development Notes

- Tests use real SQLite databases in `t.TempDir()` — no mocks for storage
- Event bus tests use real channels — no mocks
- Config tests use `t.Setenv()` for env var isolation
- `sync_log` uses `datetime('now')` which has second precision — queries use `id DESC` as tiebreaker for ordering

## Build Plan

6-phase v0 plan in `openspec/changes/`:

1. ~~Engine Layer~~ (complete)
2. Service Layer
3. TUI Shell
4. Jira Integration
5. Detail View & Clipboard
6. Polish (overlays, error handling, server stub)
