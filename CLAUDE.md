# Legato

for all work in this project, use the tdd skill, if present.

AI Agent Orchestration TUI — a keyboard-driven kanban board for tracking tasks, built for developers who work with AI coding agents. Supports local tasks and pluggable ticket providers (Jira first, others planned).

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

- `internal/engine/store/` — SQLite store with embedded migrations, task/column mapping/sync log CRUD, task ID generation, state interval tracking (`RecordStateTransition`, `GetStateDurations`, `GetStateDurationsBatch`)
- `internal/engine/events/` — Channel-based event bus with buffered pub/sub (buffer size 64, non-blocking drops on full), error event types (`EventSyncError`, `EventTransitionFailed`, `EventAuthFailed`, `EventRateLimited`) with `ErrorPayload` struct
- `internal/engine/jira/` — Jira REST API v3 client (types, HTTP client with Basic Auth/backoff, ADF-to-Markdown converter), plus `Provider` adapter
- `internal/engine/tmux/` — Tmux session manager: spawn, kill, capture-pane, attach, list/filter legato-prefixed sessions, `PaneCommands()` (batch query live foreground process names via `tmux list-panes -a -f`). LookPath-injectable for testing.
- `internal/service/` — BoardService (columns/cards CRUD + CreateTask + DeleteTask + UpdateTaskDescription + UpdateTaskTitle), SyncService (pull/push with conflict resolution, provider→task conversion, SearchRemote + ImportRemoteTask), AgentService (tmux session lifecycle + SQLite tracking + dynamic pane command population + state duration queries via `GetTaskDurations`), `TicketProvider` interface for pluggable backends
- `internal/setup/` — Setup wizard logic: first-run column seeding, interactive Jira configuration (credential validation, project/status discovery, column mapping heuristics, config writing), `ColumnSeeder` interface for testability
- `internal/tui/` — Root Bubbletea app model, view routing (viewBoard/viewDetail/viewAgents), overlay state management (`overlayType` enum + `activeOverlay tea.Model`), EventBus bridge
- `internal/tui/agents/` — Agent split-view: sidebar with agent list + terminal output panel, j/k navigation, spawn/kill/attach keybindings, capture-pane polling at 200ms
- `internal/tui/board/` — Kanban board model with vim navigation, card/column rendering, agent status line with duration display
- `internal/tui/detail/` — Full-screen task detail view with Glamour markdown, metadata header (provider-specific fields from RemoteMeta), viewport scrolling, `e` to edit description via `$EDITOR` (local tasks), `t` to edit title via overlay (local tasks)
- `internal/tui/clipboard/` — OS-native clipboard (pbcopy/wl-copy/xclip/xsel) and browser open (open/xdg-open)
- `internal/tui/overlay/` — Overlay system: shared `RenderPanel`, move overlay (single-letter shortcuts), search overlay (real-time filtering via `BoardService.SearchCards`), create overlay (inline task creation with title/description/column/priority), delete overlay (confirmation with remote/local distinction), import overlay (remote ticket search + import via `SyncService.SearchRemote`), title edit overlay (pre-filled text input for local tasks), help overlay (keybinding reference)
- `internal/tui/statusbar/` — Status bar with sync state, relative time, key hints, warnings, error messages (auto-clear on sync success)
- `internal/tui/theme/` — Lipgloss color palette, style constants, and icon system (`icons.go`: `NewIcons("unicode"|"nerdfonts")` for provider/agent/warning glyphs)
- `internal/engine/ipc/` — Unix domain socket IPC: `Server` (listens, parses newline-delimited JSON, calls callback), `Send` (client, silent on missing socket), `Broadcast` (sends to all `*.sock` in `SocketDir()`), `SocketPath()` (PID-based, XDG_RUNTIME_DIR with /tmp fallback), `Message` struct (type/task_id/status/content)
- `internal/engine/hooks/` — AI tool hook script generation. `ClaudeCodeAdapter` implements `service.AIToolAdapter`: generates shell scripts for `.claude/hooks/`, merges/removes entries in `.claude/settings.json`
- `internal/cli/` — CLI subcommand handlers: `TaskUpdate` (move task to column, case-insensitive status), `TaskNote` (append timestamped note), `AgentState` (update agent activity). All broadcast IPC to all running instances. Used by `cmd/legato/` subcommand dispatch.
- `internal/server/` — Minimal HTTP server wrapping `BoardService` with `GET /health` endpoint returning board state as JSON
- `config/` — YAML config parser with env var expansion (`os.ExpandEnv`) and XDG path resolution

## Database

- SQLite file location: `cfg.DB.Path` > `$XDG_DATA_HOME/legato/legato.db` > `~/.local/share/legato/legato.db`
- Migrations embedded via `embed.FS`, tracked with `PRAGMA user_version`
- WAL mode enabled, foreign keys ON
- Schema: `tasks`, `column_mappings`, `sync_log`, `agent_sessions`, `state_intervals` tables
- `tasks` table: core fields (id, title, description, description_md, status, priority, sort_order, created_at, updated_at) + nullable provider link (provider, remote_id, remote_meta JSON)
- Local tasks: provider/remote_id/remote_meta are NULL. Synced tasks: provider='jira', remote_id=Jira key, remote_meta=JSON with remote_status, issue_type, assignee, labels, etc.
- Task IDs: 8-char lowercase alphanumeric (crypto/rand) for local tasks, provider IDs (e.g. REX-1234) for synced tasks
- `agent_sessions` and `sync_log` reference `task_id` (not ticket_id)
- Migrations: `001_init.sql` (base), `002_stale_and_move_tracking.sql`, `003_rename_jira_to_remote.sql`, `004_agent_sessions.sql`, `005_tasks.sql` (tickets→tasks migration with remote_meta JSON packing), `006_agent_activity.sql`, `007_state_intervals.sql`

## Config

- Config file location: `$LEGATO_CONFIG` > `$XDG_CONFIG_HOME/legato/config.yaml` > `~/.config/legato/config.yaml`
- Missing config file returns defaults (no error) — app starts without config for initial setup
- Env vars expanded before YAML parsing: `${LEGATO_JIRA_TOKEN}` works in config values
- `icons` field: `"unicode"` (default) or `"nerdfonts"` for Nerd Font glyphs on cards
- `editor` field: optional editor override for description editing (used by `config.ResolveEditor`)

## Provider Architecture

The ticket source is abstracted behind `service.TicketProvider` — Jira is the first implementation, but others (Linear, GitHub Issues, etc.) can be plugged in by implementing the same interface:

- `service.TicketProvider` interface defined in `internal/service/provider.go`
- `service.JiraProviderAdapter` in `internal/service/provider_jira.go` bridges `jira.Provider` → `TicketProvider`
- Sync service (`internal/service/sync.go`) depends only on the interface, never on Jira directly
- ADF-to-Markdown conversion is internal to the Jira provider — the interface returns markdown

## Sync Algorithm

- **Pull**: periodic fetch via provider → convert to `store.Task` with provider/remote_id/remote_meta → upsert into SQLite, status-to-column mapping, stale detection via remote_meta
- **Push**: local SQLite update first (non-blocking), then async remote transition; skipped for local tasks (provider=NULL); failure logs to `sync_log` and preserves local column
- **Conflict resolution**: local wins within 5-minute window of `local_move_at` (stored in remote_meta); after window, remote state accepted on next pull
- **Scheduler**: configurable interval (default 60s), publishes SyncStarted/SyncCompleted/SyncFailed events
- **SearchRemote**: builds JQL (`summary ~ "query" OR key = "query"` scoped to `projectKeys`), min 2-char query, `ORDER BY updated DESC`
- **ImportRemoteTask**: fetches single ticket via `provider.GetTicket`, creates local task with provider metadata, skips if already tracked
- **Wiring**: `main.go` creates Jira provider + sync service when config is present, runs initial sync + periodic scheduler, passes `SyncService` to TUI app

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
- Detail view loads cards synchronously via `GetCard()` in the `OpenDetailMsg` handler (hits local SQLite, not remote API). `e` opens `$EDITOR` for description (local tasks only, via `tea.ExecProcess`), `t` opens title edit overlay (local tasks only). Status bar hints are conditional on `Provider == ""`
- Overlay system: only one overlay active at a time — `overlayType` enum (`overlayNone/Move/Search/Help/Create/Delete/Import/TitleEdit`) + `activeOverlay tea.Model`; `?` opens help from any context (replaces active overlay); `esc` always dismisses
- Move overlay shortcuts: first letter of column name lowercased (`b`=Backlog, `r`=Ready, `d`=Doing, `v`=Review, `x`=Done); falls back to number keys on conflict
- Search overlay: typing appends to query, produces `SearchQueryChangedMsg` → app calls `BoardService.SearchCards` → returns `SearchResultsMsg` → overlay updates results; `enter` closes overlay and calls `board.NavigateTo(cardID)`
- Create overlay: `n` opens from board view; title input + description input with `focusField` cycling (tab: title → column → description); `h`/`l` cycles columns when column focused, `ctrl+j` inserts newline in description, ctrl+p cycles priority (none/low/medium/high); enter submits `CreateTaskMsg` (with description), esc cancels
- Title edit overlay: `t` from detail view (local tasks only); pre-filled text input, enter saves via `BoardService.UpdateTaskTitle`, esc cancels
- Delete overlay: `d` from board, `D` from detail; shows task title, warns if remote-tracking ("removes local reference only"); `y` confirms, `n`/`esc` cancels. Confirm calls `BoardService.DeleteTask`, returns to board from detail view
- Import overlay: `i` from board (no-op without SyncService); fixed-size panel (60% width, 10 result rows + scroll indicator); typing triggers `SyncService.SearchRemote` (min 2 chars); j/k navigates, enter imports via `SyncService.ImportRemoteTask`, esc cancels. Errors shown in red instead of silent "no results"
- Card warning indicators: `CardData.Warning` bool → renders warning icon; sourced from `sync_log` where most recent entry for task is `push_failed`
- Error event classification: sync service classifies errors by message content (auth/rate-limit/offline) and publishes typed events; app converts to `statusbar.ErrorMsg`
- Server stub: `internal/server/` consumes `BoardService` interface only — no TUI imports, independently startable; tests use `httptest.NewRecorder`
- Agent sessions: tmux sessions named `legato-<TASK_ID>`, tracked in `agent_sessions` SQLite table (task_id column). Tmux manager (`internal/engine/tmux/`) uses `exec.LookPath` injection for testability. Integration tests skip when tmux not installed (`skipWithoutTmux`). SpawnAgent verifies tmux session is actually alive before returning "already running" error (prevents stale DB state). `ListAgents()` calls `PaneCommands()` to override `Command` with live foreground process name (e.g., `claude`, `vim`) for running sessions; falls back to DB value for dead sessions or on tmux query error
- Agent service tests use `mockTmux` implementing `TmuxManager` interface + real SQLite — same pattern as sync service with `mockProvider`
- Agent view: `viewAgents` enum value, toggled via `A` key from board. `agents.Model.Update` returns `(Model, tea.Cmd)` (concrete type, same as board — not `tea.Model`). Polling via `agentTickMsg` at 200ms; capture output forwarded as `CaptureOutputMsg`
- Agent attach: `tea.ExecProcess` suspends bubbletea, runs `tmux attach-session` with escape key set to `Ctrl+]` (configurable via `agents.escape_key` in config). On detach, refreshes agent list
- Agent card indicator: `CardData.AgentActive` bool → renders terminal icon on board cards with active agents. Populated by app querying `AgentService.ListAgents()` on `DataLoadedMsg`, not by BoardService
- Data model: provider-agnostic `tasks` table with nullable `provider`/`remote_id`/`remote_meta` for synced tasks. `Card.Title` (not Summary), `CardDetail.RemoteMeta` map for provider-specific fields. `service.Card.IssueType` extracted from remote_meta at read time
- Local task creation: `BoardService.CreateTask(title, description, column, priority)` generates 8-char alphanumeric ID, inserts task with description, publishes refresh event
- Local task editing: `BoardService.UpdateTaskDescription(id, description)` and `BoardService.UpdateTaskTitle(id, title)` — both reject remote tasks (provider != nil), persist changes, publish `EventCardsRefreshed`
- Editor config: `config.ResolveEditor(cfg)` → config `editor` field → `$VISUAL` → `$EDITOR` → `vi`. Used by detail view `e` keybinding to open external editor for description editing via `tea.ExecProcess`
- Task deletion: `BoardService.DeleteTask(id)` verifies existence, deletes from store, publishes `EventCardsRefreshed`. Works for both local and remote-tracking tasks (remote-tracking only removes local ref)
- Provider icons: cards show provider icon before key (◈ Jira, ◉ GitHub, ● local). Configurable via `icons` config field (`"unicode"` default, `"nerdfonts"` for Nerd Font glyphs). `theme.NewIcons(mode)` returns icon set, passed through `NewApp` → `board.New` → `RenderCard`
- Board rendering: colored column header bars (column accent colors), cards with priority-colored left borders, subtle card backgrounds (#252540), gap between columns, margin between cards. Selected cards use dark-on-light colors (`CardSelected` bg `#EEEDFE`) — **any styled text on selected cards must set `.Background(selectedBg)` explicitly**, including spaces between styled spans (render spaces inside the styled span, not as bare `+" "+` concatenation, or they show the terminal's dark default background). Also use dark foreground variants for selected state (e.g. `theme.SyncOK` → `#287828`, `theme.ColReady` → `#285878`)
- First-run setup: `main.go` checks if `column_mappings` is empty, runs `setup.RunWizard()` which seeds default columns and optionally configures Jira interactively. Uses `ColumnSeeder` interface (backed by `StoreAdapter`) for testability
- Jira client: uses `/rest/api/3/search/jql` endpoint (not the removed `/rest/api/3/search`)
- `NewSyncService` takes `projectKeys []string` for scoping remote searches
- `NewApp` takes `SyncService` (nil-safe), `theme.Icons`, and `editor string` (resolved via `config.ResolveEditor`); `board.New` takes `theme.Icons`; `detail.New`/`detail.NewLoading` take `editor string`

## Build Plan

6-phase v0 plan in `openspec/changes/`:

1. ~~Engine Layer~~ (complete)
2. ~~Service Layer~~ (complete)
3. ~~TUI Shell~~ (complete)
4. ~~Jira Integration~~ (complete)
5. ~~Detail View & Clipboard~~ (complete)
6. ~~Polish~~ (complete) — overlays (search/move/help), error handling, server stub

## CLI Subcommands

`legato` binary supports subcommand dispatch alongside the default TUI mode:

- `legato` (no args) — launches TUI (existing behavior)
- `legato task update <task-id> --status <status>` — move task to column (case-insensitive status matching)
- `legato task note <task-id> <message>` — append timestamped note to task description
- `legato agent state <task-id> --activity <working|waiting|"">` — update agent activity state on a card
- `legato hooks install [--tool claude-code]` — install AI tool hooks in current project
- `legato hooks uninstall [--tool claude-code]` — remove installed hooks

CLI subcommands load only config+store+IPC client — no TUI, event bus, tmux, or sync service.

## AI Tool Integration (Claude Code)

Abstract adapter interface (`service.AIToolAdapter`) for pluggable AI tool integrations. Claude Code is the first implementation. **Hooks do NOT perform status/column transitions** — they only update visual activity state on cards.

**Flow**: Legato spawns tmux session → injects `LEGATO_TASK_ID` env var via `tmux new-session -e` → Claude Code hooks fire on lifecycle events → hook scripts check `LEGATO_TASK_ID` → call `legato agent state` CLI → CLI updates `agent_sessions.activity` in SQLite + broadcasts IPC to all running instances → TUI event bus publishes `EventCardUpdated` → board refreshes card indicators

**Agent activity states** (stored in `agent_sessions.activity` column):
- `"working"` — Claude is actively processing (triggered by `UserPromptSubmit` hook)
- `"waiting"` — Claude finished, waiting for user input (triggered by `Stop` hook)
- `""` — no activity / cleared (triggered by `SessionEnd` hook)

**Card indicators**: `AgentState` field on `CardData` drives three visual states on a dedicated agent line: green spinning icon + "RUNNING" with cumulative duration (working), blue diamond + "WAITING" with duration (waiting), dim terminal icon + "IDLE" (agent alive but no activity). Cards with no active agent but duration history show "Xh Ym working · Zm waiting". Rendered in `board/card.go` via `renderAgentLine()`, populated via `board.SetAgentStates()` + `board.SetDurations()` from app.go data loading.

**Hook events mapped**: `UserPromptSubmit` → working, `Stop` → waiting, `SessionEnd` → clear. Scripts generated by `legato hooks install`, written to `.claude/hooks/legato-*.sh` (prompt-submit, stop, session-end), registered in `.claude/settings.json`.

**IPC**: Each TUI instance creates a PID-based Unix domain socket at `$XDG_RUNTIME_DIR/legato/legato-<pid>.sock` (fallback `/tmp/legato-<uid>/legato-<pid>.sock`). Multiple instances coexist — CLI commands `Broadcast()` to all `*.sock` files in the directory. Protocol: newline-delimited JSON. Best-effort — CLI silently skips unreachable sockets. Message types: `task_update`, `task_note`, `agent_state`.

**Adapter registration**: `AdapterRegistry` in service layer. Claude Code adapter in `internal/engine/hooks/claude_code.go`. `AgentServiceOptions` struct passes adapter + socket path to `NewAgentService` for env var injection on spawn.

**Migration**: `006_agent_activity.sql` adds `activity TEXT NOT NULL DEFAULT ''` column to `agent_sessions` table.

**State duration tracking**: `state_intervals` table records timestamped working/waiting intervals per task. `cli.AgentState()` calls both `store.UpdateAgentActivity()` and `store.RecordStateTransition()`. `ReconcileSessions()` closes orphaned intervals for dead agents. Durations computed at query time via SQL aggregation (including open intervals using `datetime('now')`). `Store.DB()` exposes underlying `*sqlx.DB` for advanced queries in tests.

**Duration formatting**: `board.formatDuration(d)` returns `""` (zero), `"<1m"` (under 60s), `"Xm"` (under 1h), `"Xh Ym"` (1h+). `CardData.WorkingDuration`/`WaitingDuration` populated during `DataLoadedMsg` via `AgentService.GetTaskDurations()` batch query.
