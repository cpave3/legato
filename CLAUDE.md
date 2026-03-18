# Legato

for all work in this project, use the tdd skill, if present.

AI Agent Orchestration TUI — a keyboard-driven kanban board for tracking tickets, built for developers who work with AI coding agents. Supports pluggable ticket providers (Jira first, others planned).

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
- `internal/engine/events/` — Channel-based event bus with buffered pub/sub (buffer size 64, non-blocking drops on full), error event types (`EventSyncError`, `EventTransitionFailed`, `EventAuthFailed`, `EventRateLimited`) with `ErrorPayload` struct
- `internal/engine/jira/` — Jira REST API v3 client (types, HTTP client with Basic Auth/backoff, ADF-to-Markdown converter), plus `Provider` adapter
- `internal/engine/tmux/` — Tmux session manager: spawn, kill, capture-pane, attach, list/filter legato-prefixed sessions. LookPath-injectable for testing.
- `internal/service/` — BoardService (columns/cards CRUD), SyncService (pull/push with conflict resolution), AgentService (tmux session lifecycle + SQLite tracking), `TicketProvider` interface for pluggable backends
- `internal/setup/` — Setup wizard logic: first-run detection, credential validation, project/status discovery, column mapping heuristics, config writing
- `internal/tui/` — Root Bubbletea app model, view routing (viewBoard/viewDetail/viewAgents), overlay state management (`overlayType` enum + `activeOverlay tea.Model`), EventBus bridge
- `internal/tui/agents/` — Agent split-view: sidebar with agent list + terminal output panel, j/k navigation, spawn/kill/attach keybindings, capture-pane polling at 200ms
- `internal/tui/board/` — Kanban board model with vim navigation, card/column rendering, agent indicator (`▶` prefix)
- `internal/tui/detail/` — Full-screen ticket detail view with Glamour markdown, metadata header, viewport scrolling
- `internal/tui/clipboard/` — OS-native clipboard (pbcopy/wl-copy/xclip/xsel) and browser open (open/xdg-open)
- `internal/tui/overlay/` — Overlay system: shared `RenderPanel`, move overlay (single-letter shortcuts), search overlay (real-time filtering via `BoardService.SearchCards`), help overlay (keybinding reference)
- `internal/tui/statusbar/` — Status bar with sync state, relative time, key hints, warnings, error messages (auto-clear on sync success)
- `internal/tui/theme/` — Lipgloss color palette and style constants
- `internal/server/` — Minimal HTTP server wrapping `BoardService` with `GET /health` endpoint returning board state as JSON
- `config/` — YAML config parser with env var expansion (`os.ExpandEnv`) and XDG path resolution

## Database

- SQLite file location: `cfg.DB.Path` > `$XDG_DATA_HOME/legato/legato.db` > `~/.local/share/legato/legato.db`
- Migrations embedded via `embed.FS`, tracked with `PRAGMA user_version`
- WAL mode enabled, foreign keys ON
- Schema: `tickets`, `column_mappings`, `sync_log`, `agent_sessions` tables
- DB columns use provider-agnostic names: `remote_status`, `remote_updated_at`, `remote_statuses`, `remote_transition` (not Jira-specific)
- Migrations: `001_init.sql` (base), `002_stale_and_move_tracking.sql` (stale_at, local_move_at), `003_rename_jira_to_remote.sql` (agnostic naming), `004_agent_sessions.sql` (agent session tracking)

## Config

- Config file location: `$LEGATO_CONFIG` > `$XDG_CONFIG_HOME/legato/config.yaml` > `~/.config/legato/config.yaml`
- Missing config file returns defaults (no error) — app starts without config for initial setup
- Env vars expanded before YAML parsing: `${LEGATO_JIRA_TOKEN}` works in config values

## Provider Architecture

The ticket source is abstracted behind `service.TicketProvider` — Jira is the first implementation, but others (Linear, GitHub Issues, etc.) can be plugged in by implementing the same interface:

- `service.TicketProvider` interface defined in `internal/service/provider.go`
- `service.JiraProviderAdapter` in `internal/service/provider_jira.go` bridges `jira.Provider` → `TicketProvider`
- Sync service (`internal/service/sync.go`) depends only on the interface, never on Jira directly
- ADF-to-Markdown conversion is internal to the Jira provider — the interface returns markdown

## Sync Algorithm

- **Pull**: periodic fetch via provider → upsert into SQLite, status-to-column mapping, stale detection (7-day retention)
- **Push**: local SQLite update first (non-blocking), then async remote transition; failure logs to `sync_log` and preserves local column
- **Conflict resolution**: local wins within 5-minute window of `local_move_at`; after window, remote state accepted on next pull
- **Scheduler**: configurable interval (default 60s), publishes SyncStarted/SyncCompleted/SyncFailed events

## Development Notes

- Tests use real SQLite databases in `t.TempDir()` — no mocks for storage
- Event bus tests use real channels — no mocks
- Config tests use `t.Setenv()` for env var isolation
- Jira client tests use `net/http/httptest` servers — no real API calls
- Sync service tests use a `mockProvider` implementing `TicketProvider` — not the real Jira client
- `sync_log` uses `datetime('now')` which has second precision — queries use `id DESC` as tiebreaker for ordering
- TUI tests: test model state via `Update()`, not rendered ANSI output — lipgloss strips styles in non-TTY test environments
- Bubbletea async data loading: never replace the entire model in a `DataLoadedMsg` — only copy data fields, or dimensions set by `WindowSizeMsg` get wiped
- `tea.Model` interface requires `Update` to return `(tea.Model, tea.Cmd)` — concrete types need type assertions in tests
- `cmd/validate/service-smoke/` — standalone service layer smoke test (renamed from phase2)
- Glamour: must use `glamour.WithStyles(styles.DarkStyleConfig)`, NOT `WithAutoStyle()` — auto-style probes terminal background via stdin/stdout which deadlocks in bubbletea's alt-screen mode
- Clipboard: `Copy()` uses `cmd.Start()` + `StdinPipe()`, NOT `cmd.Run()` — `wl-copy` on Wayland stays alive to serve paste requests, so `Run()` blocks forever
- Detail view loads cards synchronously via `GetCard()` in the `OpenDetailMsg` handler (hits local SQLite, not remote API)
- Overlay system: only one overlay active at a time — `overlayType` enum (`overlayNone/Move/Search/Help`) + `activeOverlay tea.Model`; `?` opens help from any context (replaces active overlay); `esc` always dismisses
- Move overlay shortcuts: first letter of column name lowercased (`b`=Backlog, `r`=Ready, `d`=Doing, `v`=Review, `x`=Done); falls back to number keys on conflict
- Search overlay: typing appends to query, produces `SearchQueryChangedMsg` → app calls `BoardService.SearchCards` → returns `SearchResultsMsg` → overlay updates results; `enter` closes overlay and calls `board.NavigateTo(cardID)`
- Card warning indicators: `CardData.Warning` bool → renders `!` prefix; sourced from `sync_log` where most recent entry for ticket is `push_failed`
- Error event classification: sync service classifies errors by message content (auth/rate-limit/offline) and publishes typed events; app converts to `statusbar.ErrorMsg`
- Server stub: `internal/server/` consumes `BoardService` interface only — no TUI imports, independently startable; tests use `httptest.NewRecorder`
- Agent sessions: tmux sessions named `legato-<TICKET_ID>`, tracked in `agent_sessions` SQLite table. Tmux manager (`internal/engine/tmux/`) uses `exec.LookPath` injection for testability. Integration tests skip when tmux not installed (`skipWithoutTmux`)
- Agent service tests use `mockTmux` implementing `TmuxManager` interface + real SQLite — same pattern as sync service with `mockProvider`
- Agent view: `viewAgents` enum value, toggled via `A` key from board. `agents.Model.Update` returns `(Model, tea.Cmd)` (concrete type, same as board — not `tea.Model`). Polling via `agentTickMsg` at 200ms; capture output forwarded as `CaptureOutputMsg`
- Agent attach: `tea.ExecProcess` suspends bubbletea, runs `tmux attach-session` with escape key set to `Ctrl+]` (configurable via `agents.escape_key` in config). On detach, refreshes agent list
- Agent card indicator: `CardData.AgentActive` bool → renders `▶` prefix on board cards with active agents

## Build Plan

6-phase v0 plan in `openspec/changes/`:

1. ~~Engine Layer~~ (complete)
2. ~~Service Layer~~ (complete)
3. ~~TUI Shell~~ (complete)
4. ~~Jira Integration~~ (complete)
5. ~~Detail View & Clipboard~~ (complete)
6. ~~Polish~~ (complete) — overlays (search/move/help), error handling, server stub
