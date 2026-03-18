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
task test                  # run all tests
task test:race             # run with race detector (use for event bus changes)
task test:cover            # run tests with coverage
task build                 # build the legato binary
task run                   # run the app
task vet                   # static analysis
task lint                  # golangci-lint
task check                 # build + test + vet + lint
```

## Key Packages

- `internal/engine/store/` — SQLite store with embedded migrations, ticket/column mapping/sync log CRUD
- `internal/engine/events/` — Channel-based event bus with buffered pub/sub (buffer size 64, non-blocking drops on full)
- `internal/service/` — BoardService (columns/cards CRUD) and SyncService (stub with fake data)
- `internal/tui/` — Root Bubbletea app model, view routing, EventBus bridge
- `internal/tui/board/` — Kanban board model with vim navigation, card/column rendering
- `internal/tui/statusbar/` — Status bar with sync state, relative time, key hints
- `internal/tui/theme/` — Lipgloss color palette and style constants
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
- TUI tests: test model state via `Update()`, not rendered ANSI output — lipgloss strips styles in non-TTY test environments
- Bubbletea async data loading: never replace the entire model in a `DataLoadedMsg` — only copy data fields, or dimensions set by `WindowSizeMsg` get wiped
- `tea.Model` interface requires `Update` to return `(tea.Model, tea.Cmd)` — concrete types need type assertions in tests
- `cmd/validate/service-smoke/` — standalone service layer smoke test (renamed from phase2)

## Build Plan

6-phase v0 plan in `openspec/changes/`:

1. ~~Engine Layer~~ (complete)
2. ~~Service Layer~~ (complete)
3. ~~TUI Shell~~ (complete)
4. Jira Integration
5. Detail View & Clipboard
6. Polish (overlays, error handling, server stub)
