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
- `internal/engine/github/` — GitHub PR status client via `gh` CLI: `FetchPRStatus` (single branch), `BatchFetchPRStatus` (concurrent fan-out with semaphore(5)), `DetectRepo`/`DetectBranch` (git remote/branch detection), `FetchCommentCount` (via `gh api`), `deriveCheckStatus` (CI rollup aggregation). LookPath + ExecCommand injectable for testing.
- `internal/engine/tmux/` — Tmux session manager: spawn, kill, capture-pane, attach, set-option, list/filter legato-prefixed sessions, `PaneCommands()` (batch query live foreground process names via `tmux list-panes -a -f`). LookPath-injectable for testing.
- `internal/service/` — BoardService (columns/cards CRUD + CreateTask + DeleteTask + UpdateTaskDescription + UpdateTaskTitle + UpdateTaskWorkspace + ListCardsByWorkspace + ListWorkspaces + ArchiveDoneCards + ArchiveTask + CountDoneCards), SyncService (pull/push with conflict resolution, provider→task conversion, SearchRemote + ImportRemoteTask), AgentService (tmux session lifecycle + SQLite tracking + dynamic pane command population + task title enrichment + state duration queries via `GetTaskDurations`), PRTrackingService (branch-to-task linking, periodic PR status polling via `gh` CLI, auto-link on agent spawn), `TicketProvider` interface for pluggable backends, `SeedWorkspaces` (config→DB workspace seeding)
- `internal/setup/` — Setup wizard logic: first-run column seeding, interactive Jira configuration (credential validation, project/status discovery, column mapping heuristics, config writing), `ColumnSeeder` interface for testability
- `internal/tui/` — Root Bubbletea app model, view routing (viewBoard/viewDetail/viewAgents), overlay state management (`overlayType` enum + `activeOverlay tea.Model`), EventBus bridge
- `internal/tui/agents/` — Agent split-view: sidebar with agent list (status, task ID, title, command per entry) + terminal output panel, j/k navigation, spawn/kill/attach keybindings, capture-pane polling at 200ms
- `internal/tui/board/` — Kanban board model with vim navigation, card/column rendering, agent status line with duration display
- `internal/tui/detail/` — Full-screen task detail view with Glamour markdown, metadata header (provider-specific fields from RemoteMeta), viewport scrolling, `e` to edit description via `$EDITOR` (local tasks), `t` to edit title via overlay (local tasks)
- `internal/tui/clipboard/` — OS-native clipboard (pbcopy/wl-copy/xclip/xsel) and browser open (open/xdg-open)
- `internal/tui/overlay/` — Overlay system: shared `RenderPanel`, move overlay (single-letter shortcuts, `w` transitions to workspace assignment), search overlay (real-time filtering via `BoardService.SearchCards`), create overlay (inline task creation with title/description/column/priority/workspace), delete overlay (confirmation with remote/local distinction), import overlay (remote ticket search + import via `SyncService.SearchRemote`), title edit overlay (pre-filled text input for local tasks), workspace overlay (board view filter), move-workspace overlay (task workspace assignment), archive overlay (bulk archive done cards with count + confirmation), link PR overlay (two-phase: repo+number input → preview → confirm), help overlay (keybinding reference)
- `internal/tui/statusbar/` — Status bar with sync state, relative time, key hints, warnings, error messages (auto-clear on sync success)
- `internal/tui/theme/` — Lipgloss color palette, style constants, and icon system (`icons.go`: `NewIcons("unicode"|"nerdfonts")` for provider/agent/warning glyphs)
- `internal/engine/ipc/` — Unix domain socket IPC: `Server` (listens, parses newline-delimited JSON, calls callback), `Send` (client, silent on missing socket), `Broadcast` (sends to all `*.sock` in `SocketDir()`), `SocketPath()` (PID-based, XDG_RUNTIME_DIR with /tmp fallback), `Message` struct (type/task_id/status/content)
- `internal/engine/hooks/` — AI tool hook script generation. `ClaudeCodeAdapter` implements `service.AIToolAdapter`: generates shell scripts for `.claude/hooks/`, merges/removes entries in `.claude/settings.json`. `StaccatoAdapter`: generates `post-pr-create` hook at `~/.config/staccato/hooks/post-pr-create/legato-pr-link.sh` (directory-based, no config file)
- `internal/cli/` — CLI subcommand handlers: `TaskUpdate` (move task to column, case-insensitive status), `TaskNote` (append timestamped note), `AgentState` (update agent activity), `AgentSummary` (tmux-formatted agent activity counts with `--exclude` flag), `TaskLink` (link branch/repo to task for PR tracking), `TaskUnlink` (remove PR association). All broadcast IPC to all running instances. Used by `cmd/legato/` subcommand dispatch.
- `internal/server/` — HTTP + WebSocket server for web UI. `GET /health` (board state JSON), `GET /api/agents`, `GET /api/tasks`, `POST /api/agents/spawn` (ephemeral agent creation), `POST /api/agents/kill` (agent termination), `/ws` (WebSocket). `streamManager` fans out `pipe-pane` output per agent with deferred pipe start after first resize. Prompt detection via `prompt.Detect` (500ms debounce after output settles + on-demand `detect_prompt` message). `resizePane` computes min of web client sizes + attached tmux terminal clients. `send_keys` supports literal text (`\n`-terminated) and space-separated named key sequences (e.g. `"Down Enter"`). `refresh_pane` recaptures pane with ANSI escapes and resets web xterm. Escape sequence buffering (`findIncompleteEscape`) prevents split ANSI sequences from corrupting xterm.js state. `Server.Serve(ln)` accepts pre-bound listener for auto-start use. `onStreamEnd` callback triggers `ReconcileSessions` + `agents_changed` broadcast when pipe-pane EOF detected (shell exit)
- `config/` — YAML config parser with env var expansion (`os.ExpandEnv`) and XDG path resolution

## Database

- SQLite file location: `cfg.DB.Path` > `$XDG_DATA_HOME/legato/legato.db` > `~/.local/share/legato/legato.db`
- Migrations embedded via `embed.FS`, tracked with `PRAGMA user_version`
- WAL mode enabled, foreign keys ON
- Schema: `tasks`, `column_mappings`, `sync_log`, `agent_sessions`, `state_intervals`, `workspaces` tables
- `tasks` table: core fields (id, title, description, description_md, status, priority, sort_order, workspace_id, archived_at, created_at, updated_at) + nullable provider link (provider, remote_id, remote_meta JSON) + nullable `pr_meta` TEXT (JSON with branch, pr_number, pr_url, state, is_draft, review_decision, check_status, comment_count, updated_at). `archived_at` is nullable DATETIME — NULL means active, non-NULL means archived (hidden from board but retained in DB)
- `workspaces` table: id (INTEGER PRIMARY KEY), name (TEXT UNIQUE), color (TEXT), sort_order (INTEGER)
- Local tasks: provider/remote_id/remote_meta are NULL. Synced tasks: provider='jira', remote_id=Jira key, remote_meta=JSON with remote_status, issue_type, assignee, labels, etc.
- Task IDs: 8-char lowercase alphanumeric (crypto/rand) for local tasks, provider IDs (e.g. REX-1234) for synced tasks
- `agent_sessions` and `sync_log` reference `task_id` (not ticket_id)
- Migrations: `001_init.sql` (base), `002_stale_and_move_tracking.sql`, `003_rename_jira_to_remote.sql`, `004_agent_sessions.sql`, `005_tasks.sql` (tickets→tasks migration with remote_meta JSON packing), `006_agent_activity.sql`, `007_state_intervals.sql`, `008_workspaces.sql` (workspaces table + tasks.workspace_id FK), `009_archive.sql` (archived_at column on tasks), `010_pr_meta.sql` (pr_meta TEXT column on tasks)

## Config

- Config file location: `$LEGATO_CONFIG` > `$XDG_CONFIG_HOME/legato/config.yaml` > `~/.config/legato/config.yaml`
- Missing config file returns defaults (no error) — app starts without config for initial setup
- Env vars expanded before YAML parsing: `${LEGATO_JIRA_TOKEN}` works in config values
- `icons` field: `"unicode"` (default) or `"nerdfonts"` for Nerd Font glyphs on cards
- `editor` field: optional editor override for description editing (used by `config.ResolveEditor`)
- `agents.tmux_options` field: map of tmux option key→value pairs applied to each spawned agent session via `tmux set-option` (e.g., `mouse: "on"`, `history-limit: "50000"`)
- `workspaces` field: list of `{name, color}` objects defining workspaces. Seeded to DB on startup via `service.SeedWorkspaces`. Color is hex string (e.g. `"#4A9EEF"`)
- `web.enabled` field: bool, auto-start web server alongside TUI. On startup, probes the port — if free, starts server in background goroutine sharing the same services. If port in use (another instance), skips silently. Server shuts down cleanly when TUI exits. Status bar shows `Web :<port>` indicator. `web.port` field: string (default `"3080"`)
- `github.poll_interval_seconds` field: polling interval for unresolved PRs — branch-only, no PR number yet (default 600s / 10 min). `github.resolved_poll_interval_seconds`: polling interval for resolved PRs — already have PR number (default 600s / 10 min). Both require `gh` CLI installed and authenticated. Manual refresh via `r` key bypasses intervals and polls all PRs immediately.

## Provider Architecture

The ticket source is abstracted behind `service.TicketProvider` — Jira is the first implementation, but others (Linear, GitHub Issues, etc.) can be plugged in by implementing the same interface:

- `service.TicketProvider` interface defined in `internal/service/provider.go`
- `service.JiraProviderAdapter` in `internal/service/provider_jira.go` bridges `jira.Provider` → `TicketProvider`
- Sync service (`internal/service/sync.go`) depends only on the interface, never on Jira directly
- ADF-to-Markdown conversion is internal to the Jira provider — the interface returns markdown

## Sync Algorithm

- **Pull**: periodic fetch via provider → **update existing tracked tasks only** (new tickets must be imported manually via `i` overlay or `ImportRemoteTask`). Status-to-column mapping, stale detection via remote_meta. Archived tasks skipped. Untracked remote tickets are silently ignored — pull never auto-imports
- **Push**: local SQLite update first (non-blocking), then async remote transition; skipped for local tasks (provider=NULL); failure logs to `sync_log` and preserves local column
- **Conflict resolution**: local wins within 5-minute window of `local_move_at` (stored in remote_meta); after window, remote state accepted on next pull
- **Scheduler**: configurable interval (default 60s), publishes SyncStarted/SyncCompleted/SyncFailed events
- **SearchRemote**: builds JQL (`summary ~ "query" OR key = "query"` scoped to `projectKeys`), min 2-char query, `ORDER BY updated DESC`
- **ImportRemoteTask**: `ImportRemoteTask(ctx, ticketID, workspaceID)` fetches single ticket via `provider.GetTicket`, creates local task with provider metadata and optional workspace, skips if already tracked
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
- Overlay system: only one overlay active at a time — `overlayType` enum (`overlayNone/Move/Search/Help/Create/Delete/Import/TitleEdit/Workspace/MoveWorkspace/Archive/LinkPR/OpenURL`) + `activeOverlay tea.Model`; `?` opens help from any context (replaces active overlay); `esc` always dismisses
- Move overlay shortcuts: first letter of column name lowercased (`b`=Backlog, `r`=Ready, `d`=Doing, `v`=Review, `x`=Done); falls back to number keys on conflict. `w` opens workspace assignment overlay (`MoveWorkspaceOverlay`) for the same task — lists "None" + workspaces, marks current, emits `WorkspaceAssignedMsg{TaskID, WorkspaceID}`. App calls `UpdateTaskWorkspace` on selection
- Search overlay: typing appends to query, produces `SearchQueryChangedMsg` → app calls `BoardService.SearchCards` → returns `SearchResultsMsg` → overlay updates results; `enter` closes overlay and calls `board.NavigateTo(cardID)`
- Create overlay: `n` opens from board view; title input + description input with `focusField` cycling (tab: title → column → workspace → description); `h`/`l` cycles columns/workspaces when focused, `ctrl+j` inserts newline in description, ctrl+p cycles priority (none/low/medium/high); workspace pre-filled with active workspace (None if All/Unassigned view); enter submits `CreateTaskMsg` (with description + workspaceID), esc cancels
- Title edit overlay: `t` from detail view (local tasks only); pre-filled text input, enter saves via `BoardService.UpdateTaskTitle`, esc cancels
- Delete overlay: `d` from board, `D` from detail; shows task title, warns if remote-tracking ("removes local reference only"); `y` confirms, `n`/`esc` cancels. Confirm calls `BoardService.DeleteTask`, returns to board from detail view
- Archive overlay: `X` (shift-x) from board opens archive confirmation if Done column has cards; shows "Archive N done cards?", `y` confirms → `ArchiveConfirmedMsg` → `BoardService.ArchiveDoneCards()` → board refresh, `n`/`esc` cancels. No-op if Done column is empty. Done column identified by name (case-insensitive match on "Done"), not by position — boards may have columns after Done (e.g. Unmapped)
- Card archiving: `store.ArchiveTask(id)` sets `archived_at = datetime('now')`, `store.ArchiveTasksByStatus(status)` bulk archives. All listing queries (`ListTasksByStatus`, `ListTasksByStatusAndWorkspace`) filter `AND archived_at IS NULL`. `BoardService.ArchiveTask(id)` validates task is in Done column before archiving. No unarchive in v1 — data preserved in DB for future restore feature
- Import overlay: `i` from board (no-op without SyncService); fixed-size panel (60% width, 10 result rows + scroll indicator); typing triggers `SyncService.SearchRemote` (min 2 chars); j/k navigates, enter imports via `SyncService.ImportRemoteTask` (with active workspace ID if viewing a specific workspace), esc cancels. Errors shown in red instead of silent "no results"
- Workspace overlay: `w` from board opens workspace switcher listing "All", "Unassigned", and each configured workspace (with color dot). j/k navigates, single-letter shortcuts, enter selects, esc dismisses. Returns `WorkspaceSelectedMsg` with `store.WorkspaceView`. Board filters cards via `ListCardsByWorkspace`. Workspace tags shown on cards in "All" view only. Status bar shows active workspace name in configured color. Create overlay includes workspace picker (h/l cycling, pre-filled with active workspace)
- Workspace data flow: `workspaces` config → `SeedWorkspaces` at startup → `workspaces` SQLite table → `tasks.workspace_id` FK. `store.WorkspaceView` enum: `ViewAll`/`ViewUnassigned`/`ViewWorkspace(id)`. `ListTasksByStatusAndWorkspace` handles SQL filtering. Board model holds `workspaceView` and `workspaces` state. `NewApp` takes `[]service.Workspace` param. Workspace assignment works for both local and remote tasks (workspaces are a local organizational concept)
- Card warning indicators: `CardData.Warning` bool → renders warning icon; sourced from `sync_log` where most recent entry for task is `push_failed`
- Error event classification: sync service classifies errors by message content (auth/rate-limit/offline) and publishes typed events; app converts to `statusbar.ErrorMsg`
- Server: `internal/server/` consumes `BoardService`, `AgentService`, and `TmuxManager` interfaces + `engine/prompt` for detection — no TUI imports, independently startable; tests use `httptest.NewRecorder`, `mockTmuxManager`, and `mockBoardService`. Stream lifecycle tests cover subscribe/unsubscribe, pipe reset on failure, cancel abort. `findIncompleteEscape` has table-driven tests for CSI/OSC/bare-ESC edge cases. `log.SetOutput(io.Discard)` before bubbletea alt-screen to prevent server log writes from corrupting TUI
- Agent sessions: tmux sessions named `legato-<TASK_ID>`, tracked in `agent_sessions` SQLite table (task_id column). Tmux manager (`internal/engine/tmux/`) uses `exec.LookPath` injection for testability. Integration tests skip when tmux not installed (`skipWithoutTmux`). SpawnAgent verifies tmux session is actually alive before returning "already running" error (prevents stale DB state). `ListAgents()` calls `PaneCommands()` to override `Command` with live foreground process name (e.g., `claude`, `vim`) for running sessions; falls back to DB value for dead sessions or on tmux query error. `AgentSession.Title` populated via `store.GetTask()` lookup per session (gracefully empty if task deleted)
- Agent service tests use `mockTmux` implementing `TmuxManager` interface + real SQLite — same pattern as sync service with `mockProvider`
- Agent view: `viewAgents` enum value, toggled via `A` key from board. `agents.Model.Update` returns `(Model, tea.Cmd)` (concrete type, same as board — not `tea.Model`). Polling via `agentTickMsg` at 200ms; capture output forwarded as `CaptureOutputMsg`
- Agent attach: `tea.ExecProcess` suspends bubbletea, runs `tmux attach-session` with escape key set to `Ctrl+]` (configurable via `agents.escape_key` in config). On detach, refreshes agent list
- Agent card indicator: `CardData.AgentActive` bool → renders terminal icon on board cards with active agents. Populated by app querying `AgentService.ListAgents()` on `DataLoadedMsg`, not by BoardService
- Data model: provider-agnostic `tasks` table with nullable `provider`/`remote_id`/`remote_meta` for synced tasks, nullable `workspace_id` FK to `workspaces`. `Card.Title` (not Summary), `Card.WorkspaceName`/`WorkspaceColor` populated in All view. `CardDetail.RemoteMeta` map for provider-specific fields, `CardDetail.WorkspaceID *int`. `service.Card.IssueType` extracted from remote_meta at read time
- Local task creation: `BoardService.CreateTask(title, description, column, priority, workspaceID)` generates 8-char alphanumeric ID, inserts task with description and optional workspace, publishes refresh event
- Local task editing: `BoardService.UpdateTaskDescription(id, description)` and `BoardService.UpdateTaskTitle(id, title)` — both reject remote tasks (provider != nil), persist changes, publish `EventCardsRefreshed`
- Editor config: `config.ResolveEditor(cfg)` → config `editor` field → `$VISUAL` → `$EDITOR` → `vi`. Used by detail view `e` keybinding to open external editor for description editing via `tea.ExecProcess`
- Task deletion: `BoardService.DeleteTask(id)` verifies existence, deletes from store, publishes `EventCardsRefreshed`. Works for both local and remote-tracking tasks (remote-tracking only removes local ref)
- Provider icons: cards show provider icon before key (◈ Jira, ◉ GitHub, ● local). Configurable via `icons` config field (`"unicode"` default, `"nerdfonts"` for Nerd Font glyphs). `theme.NewIcons(mode)` returns icon set, passed through `NewApp` → `board.New` → `RenderCard`
- Board rendering: colored column header bars (column accent colors), cards with priority-colored left borders, subtle card backgrounds (#252540), gap between columns, margin between cards. Selected cards use dark-on-light colors (`CardSelected` bg `#EEEDFE`) — **any styled text on selected cards must set `.Background(selectedBg)` explicitly**, including spaces between styled spans (render spaces inside the styled span, not as bare `+" "+` concatenation, or they show the terminal's dark default background). Also use dark foreground variants for selected state (e.g. `theme.SyncOK` → `#287828`, `theme.ColReady` → `#285878`)
- First-run setup: `main.go` checks if `column_mappings` is empty, runs `setup.RunWizard()` which seeds default columns and optionally configures Jira interactively. Uses `ColumnSeeder` interface (backed by `StoreAdapter`) for testability
- Jira client: uses `/rest/api/3/search/jql` endpoint (not the removed `/rest/api/3/search`)
- `NewSyncService` takes `projectKeys []string` for scoping remote searches
- `NewApp` takes `SyncService` (nil-safe), `PRTrackingService` (nil-safe), `theme.Icons`, `editor string` (resolved via `config.ResolveEditor`), and `[]service.Workspace` (loaded from DB); `board.New` takes `theme.Icons`; `detail.New`/`detail.NewLoading` take `editor string`

## GitHub PR Tracking

Read-only PR enrichment orthogonal to ticket providers — any task (local or Jira-synced) can track a PR. Uses `gh` CLI (no token management, leverages existing auth). Graceful degradation if `gh` not installed.

- **PR metadata**: stored as JSON in `tasks.pr_meta` column (separate from `remote_meta`). `store.PRMeta` struct with `MarshalPRMeta`/`ParsePRMeta` helpers. Fields: repo (owner/repo), branch, pr_number, pr_url, state, is_draft, review_decision, check_status, comment_count, updated_at
- **GitHub client**: `internal/engine/github/` — `FetchPRStatus(branch, repo...)` shells out to `gh pr list --head <branch> [--repo owner/repo] --json ...`. `FetchPRByNumber(owner, repo, number)` fetches single PR via `gh api repos/{owner}/{repo}/pulls/{n}`. `BatchFetchPRStatusWithRepo(queries)` fans out with semaphore(5) and per-branch repo scoping. `Options` struct with injectable `LookPath` and `ExecCommand` for testing. `deriveCheckStatus` aggregates CI rollup (pass/fail/pending/""). `PRStatus.HeadBranch` populated by `FetchPRByNumber`
- **PR tracking service**: `internal/service/pr_tracking.go` — `PRTrackingService` interface with `LinkBranch`, `LinkPR`, `UnlinkBranch`, `PollOnce`, `StartPolling`, `GetPRStatus`, `DetectRepo`, `FetchPRByNumber`. `LinkBranch(ctx, taskID, branch, repo)` stores branch+repo in `pr_meta` + triggers immediate fetch. `LinkPR(ctx, taskID, owner, repo, prNumber)` fetches full PR data by number and stores complete PRMeta (used by link overlay). `PollOnce` uses stored `PRMeta.Repo` for repo-scoped queries — **skips branches with empty repo** to avoid `gh` failures from non-git directories. `StartPolling` returns stop func (same pattern as `SyncService.StartScheduler`). `AutoLinkBranch` detects both git branch and repo on agent spawn — skips if either detection fails (prevents linking wrong repo context, e.g. dotfiles from `~`)
- **CLI**: `legato task link <id> [--branch <branch>] [--repo <owner/repo>]` and `legato task unlink <id>`. Auto-detects branch via `git rev-parse --abbrev-ref HEAD`. `--repo` stores owner/repo for repo-scoped polling. Broadcasts `pr_linked` IPC message
- **Link PR overlay**: `p` from board opens two-phase overlay — input phase (repo + PR number fields, tab to cycle) → fetch → confirm phase (shows title/branch/state, `y` confirms, `n` cancels). Repo pre-filled from git remote if detectable. Calls `PRTrackingService.LinkPR` on confirm
- **Board cards**: `CardData` includes `PRCheckStatus`, `PRReviewDecision`, `PRCommentCount`, `PRIsDraft`, `PRNumber`. `renderPRLine()` shows compact CI/review/comment indicators. `SetPRStates()` populates from `pr_meta` during `DataLoadedMsg`
- **Detail view**: PR section in header shows PR number, review decision, CI status, comment count. "Branch: <name> — No PR found" when linked but no PR. `o` keybinding: if both provider URL and PR URL exist, opens URL picker overlay (`j` = Jira, `g` = GitHub PR, `esc` = cancel); if only one URL exists, opens it directly
- **Polling cadence**: `PollOnce` (periodic) skips resolved PRs unless `resolvedInterval` has elapsed — unresolved PRs poll faster to discover new PRs. `PollAll` (startup + manual `r`) fetches everything regardless of cadence. `lastResolvedPoll` timestamp tracks when resolved PRs were last checked
- **Manual refresh**: `r` from board triggers `manualRefresh()` — runs Jira sync + `PollAll` in one goroutine, shows "syncing..." status bar, clears on completion via `manualRefreshDoneMsg`
- **Events**: `EventPRStatusUpdated` published after poll detects changes. TUI subscribes to all sync event types (`EventSyncStarted/Completed/Failed` + error events) via per-type channels. `EventBusMsg.ch` carries source channel for re-subscribing. IPC `pr_linked` message also triggers refresh
- **Auto-link**: `AgentService.SpawnAgent` calls `PRTrackingService.AutoLinkBranch` (best-effort, skips if already linked, not in git repo, or repo detection fails)

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
- `legato agent summary [--exclude <task-id>]` — output tmux-formatted agent session counts (working/waiting/idle) for use in tmux status bar `#()` expansion
- `legato task link <task-id> [--branch <branch>] [--repo <owner/repo>]` — link a git branch to a task for PR tracking (auto-detects branch if `--branch` omitted, `--repo` enables repo-scoped polling)
- `legato task unlink <task-id>` — remove branch/PR association from a task
- `legato hooks install [--tool claude-code|staccato]` — install AI tool hooks (claude-code: `.claude/hooks/`, staccato: `~/.config/staccato/hooks/`)
- `legato hooks uninstall [--tool claude-code|staccato]` — remove installed hooks

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

**IPC**: Each TUI instance creates a PID-based Unix domain socket at `$XDG_RUNTIME_DIR/legato/legato-<pid>.sock` (fallback `/tmp/legato-<uid>/legato-<pid>.sock`). Multiple instances coexist — CLI commands `Broadcast()` to all `*.sock` files in the directory. Protocol: newline-delimited JSON. Best-effort — CLI silently skips unreachable sockets. Message types: `task_update`, `task_note`, `agent_state`, `pr_linked`.

**Adapter registration**: `AdapterRegistry` in service layer. Claude Code adapter in `internal/engine/hooks/claude_code.go`. `AgentServiceOptions` struct passes adapter, socket path, and `TmuxOptions` to `NewAgentService` for env var injection and session configuration on spawn.

**Migration**: `006_agent_activity.sql` adds `activity TEXT NOT NULL DEFAULT ''` column to `agent_sessions` table.

**State duration tracking**: `state_intervals` table records timestamped working/waiting intervals per task. `cli.AgentState()` calls both `store.UpdateAgentActivity()` and `store.RecordStateTransition()`. `ReconcileSessions()` closes orphaned intervals for dead agents. Durations computed at query time via SQL aggregation (including open intervals using `datetime('now')`). `Store.DB()` exposes underlying `*sqlx.DB` for advanced queries in tests.

**Duration formatting**: `board.formatDuration(d)` returns `""` (zero), `"<1m"` (under 60s), `"Xm"` (under 1h), `"Xh Ym"` (1h+). `CardData.WorkingDuration`/`WaitingDuration` populated during `DataLoadedMsg` via `AgentService.GetTaskDurations()` batch query.

## Tmux Status Line

Legato-spawned tmux sessions get a custom status bar showing a live summary of other agent sessions. Implemented via tmux `#()` shell expansion calling `legato agent summary`.

- **Injection**: `SpawnAgent` sets `status-right`, `status-interval` (5s), `status-style`, `status-left` on the session *before* user `tmux_options` — user config can override
- **Binary path**: Resolved once at startup via `os.Executable()`, passed to `AgentServiceOptions.BinaryPath`, embedded as absolute path in `status-right` command
- **Exclude self**: Each session's `status-right` includes `--exclude <taskID>` so the operator sees counts for *other* sessions only
- **Output format**: `legato agent summary` outputs tmux-native style markup (`#[fg=green]2 working #[fg=colour240]· #[fg=yellow]1 waiting #[fg=colour240]· #[fg=colour245]0 idle`). Zero-count working/waiting states omitted; idle always shown
- **Performance**: Opens SQLite, runs single `GROUP BY` aggregate query, exits. Sub-10ms typical execution

## Staccato Integration

`StaccatoAdapter` in `internal/engine/hooks/staccato.go` implements `AIToolAdapter`. Installs a `post-pr-create` hook at `~/.config/staccato/hooks/post-pr-create/legato-pr-link.sh` (staccato uses directory-based hooks, not config files).

**Flow**: Staccato `post-pr-create` fires when browser opens to PR creation page (PR may not exist yet) → hook reads `LEGATO_TASK_ID` (injected by legato's tmux session) + `ST_REPO_PATH` + `ST_BRANCH` → detects owner/repo from git remote → calls `legato task link $LEGATO_TASK_ID --branch $ST_BRANCH --repo owner/repo` → IPC broadcast triggers immediate poll → background polling discovers PR when it actually exists.

**Key detail**: staccato's `post-pr-create` fires on PR page open, not on actual PR creation. So the initial link only stores repo+branch (no PR number). The PR tracking service polls `gh pr list --head <branch> --repo <owner/repo>` until the PR materializes. This is why `PRMeta.Repo` is needed — legato may not be running from the same repo directory.

## Web UI

Remote web interface for monitoring and interacting with agent sessions from any device (mobile, tablet, second screen). Built with React 19 + Vite + xterm.js + TailwindCSS. Compiled to `internal/server/static/dist/` and embedded in the Go binary via `embed.FS`.

**Starting**: `legato serve [--port 3080]` for standalone, or `web.enabled: true` in config to auto-start alongside TUI. Auto-start probes the port first — skips if another instance already has it. `Server.Serve(ln)` accepts pre-bound listener to avoid close-rebind race. Server shuts down when TUI exits via `webSrv.Stop()`.

**Terminal streaming**: `pipe-pane` fans output to all subscribed web clients via WebSocket. Pipe start is deferred until the first `resize` message so the tmux pane is correctly sized before output flows. On subscribe, the server waits 150ms for SIGWINCH redraw, then sends a `CaptureWithEscapes` snapshot as backfill (`\n` → `\r\n` for xterm.js). Escape sequence buffering (`findIncompleteEscape`) holds back incomplete CSI/OSC sequences at chunk boundaries to prevent xterm.js state corruption. Read buffer is 32KB.

**Resize protocol**: Web clients send `resize` messages (initial + on container resize + 5s heartbeat for TTL). Server computes min of all web client sizes + attached tmux terminal clients (`tmux list-clients -t <session>`). Only resizes if dimensions actually changed (dedup via `appliedCols`/`appliedRows`). Stale clients expire after 10s TTL. On last client disconnect, resets `window-size` to `latest` so tmux auto-sizes from remaining terminal clients.

**Prompt detection**: `prompt.Detect` runs on captured pane output after 500ms debounce following output. Classifies as `tool_approval` (Yes/Always/No buttons), `plan_approval` (Accept/Reject), or `free_text` (text input). Actions use arrow-key navigation: `Enter` (confirm default), `Down Enter` (second option), `Down Down Enter` (third option), `Escape` (dismiss). On-demand detection via `detect_prompt` WebSocket message (Detect button in overflow menu). Buttons auto-dismiss after clicking an action.

**Key sending**: `send_keys` WebSocket message. Text ending in `\n` → literal text via `tmux send-keys --` then named `Enter`. No `\n` → space-separated named keys sent in sequence (e.g. `"Down Enter"` → two `SendKey` calls). Used by prompt action buttons, Mode (BTab), Stop (Escape), and free text input.

**Agent management**: `POST /api/agents/spawn` creates ephemeral agent (title input, defaults to "Ephemeral session"). `POST /api/agents/kill` terminates agent tmux session. Both broadcast `agents_changed`. Dead agent detection: `onStreamEnd` callback fires on pipe-pane EOF → `ReconcileSessions` updates DB → `agents_changed` broadcast → web sidebar filters to `status === "running"` only. Auto-disconnect from selected agent if it dies.

**Mobile support**: Custom touch-to-scroll handler intercepts `touchstart`/`touchmove` on terminal container, calculates swipe delta, calls `term.scrollLines()` directly (xterm.js built-in touch doesn't work on mobile browsers). `touchmove` uses `preventDefault` (non-passive) to block pull-to-refresh. `overscroll-behavior: none` on body as fallback. Floating scroll-to-bottom button appears when scrolled up from bottom (tracks `buffer.active.viewportY < baseY`).

**UI components**:
- `TerminalPanel` — xterm.js terminal with resize reporting, touch scroll, scroll-to-bottom FAB, `[connected]` message on agent switch
- `PromptBar` — Primary buttons (Mode, Stop when working) + overflow menu (Refresh terminal, Re-detect prompt, Disconnect, Kill agent). Tool/plan approval buttons when detected. Free text input with arrow key passthrough when empty (Up/Down/Enter/Escape/Tab sent to tmux)
- `AgentSidebar` — Agent list with activity badges (working/waiting/idle), spawn button (`+`). Desktop sidebar + mobile dropdown with spawn button
- `WebSocketProvider` — Connection management with exponential backoff reconnect

**Frontend source**: `web/src/` — `pages/Agents.tsx` (main orchestrator), `components/TerminalPanel.tsx`, `components/PromptBar.tsx`, `components/AgentSidebar.tsx`, `hooks/useWebSocket.ts`. Build: `cd web && npm run build` → outputs to `../internal/server/static/dist/`. Dist files gitignored except `.gitkeep`.
