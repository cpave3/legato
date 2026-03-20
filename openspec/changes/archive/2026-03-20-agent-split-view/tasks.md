## 1. Database & Migration

- [x] 1.1 Create migration `004_agent_sessions.sql` adding `agent_sessions` table (id, ticket_id FK, tmux_session UNIQUE, command, status, started_at, ended_at) — validate: migration applies cleanly, `PRAGMA user_version` increments
- [x] 1.2 Add Store CRUD methods for agent sessions: `InsertAgentSession`, `UpdateAgentSessionStatus`, `ListAgentSessions`, `GetAgentSessionByTicketID`, `GetAgentSessionByTmuxName` — validate: round-trip tests with real SQLite in t.TempDir()

## 2. Engine: Tmux Manager (`internal/engine/tmux/`)

- [x] 2.1 Create `tmux.Manager` with `New(opts)` constructor and tmux binary detection (`exec.LookPath`) — validate: test returns error when tmux not on PATH (use test helper to override lookup)
- [x] 2.2 Implement `Spawn(name, workDir string) error` — creates detached tmux session via `tmux new-session -d -s <name> -c <workDir>` — validate: integration test spawns session, `tmux has-session` confirms it exists, cleanup in test teardown
- [x] 2.3 Implement `Kill(name string) error` — runs `tmux kill-session -t <name>`, no error if already dead — validate: kill running session, kill non-existent session (no error)
- [x] 2.4 Implement `Capture(name string) (string, error)` — runs `tmux capture-pane -t <name> -p` — validate: spawn session, send keys, capture contains expected output
- [x] 2.5 Implement `Attach(name string) *exec.Cmd` — returns `exec.Cmd` for `tmux attach-session -t <name>` with escape key configured as detach key — validate: unit test returns correctly formed command
- [x] 2.6 Implement `ListSessions() ([]string, error)` — runs `tmux list-sessions`, filters to `legato-*` prefix, returns session names — validate: spawn two sessions, list returns both
- [x] 2.7 Implement `IsAlive(name string) (bool, error)` — runs `tmux has-session -t <name>` — validate: true for running, false for non-existent

## 3. Service: AgentService (`internal/service/`)

- [x] 3.1 Define `AgentService` interface and concrete struct composing `tmux.Manager` + `Store` — validate: compiles, constructor wires dependencies
- [x] 3.2 Implement `SpawnAgent(ticketID string) error` — creates tmux session + DB row, errors on duplicate or missing tmux — validate: test with mock tmux manager (interface), real SQLite
- [x] 3.3 Implement `KillAgent(ticketID string) error` — kills tmux session + updates DB status to `dead` with `ended_at` — validate: test kill running agent, kill already-dead agent
- [x] 3.4 Implement `ListAgents() ([]AgentSession, error)` — queries DB, returns session metadata with elapsed time — validate: test with multiple sessions in various states
- [x] 3.5 Implement `ReconcileSessions() error` — on startup, check DB `running` sessions against `tmux.ListSessions`, mark dead ones — validate: test with sessions in DB but no tmux process
- [x] 3.6 Implement `CaptureOutput(ticketID string) (string, error)` — proxy to tmux.Capture via session name lookup — validate: test returns captured content
- [x] 3.7 Implement `AttachCmd(ticketID string) (*exec.Cmd, error)` — proxy to tmux.Attach via session name lookup — validate: test returns exec.Cmd

## 4. Config: Escape Key

- [x] 4.1 Add `Agents` section to config struct with `EscapeKey` field (default `ctrl+]`) — validate: parse config with and without agents section, default applies

## 5. TUI: Agent View Model (`internal/tui/agents/`)

- [x] 5.1 Create `agents.Model` struct implementing `tea.Model` with agent list state, selected index, terminal content buffer, and dimensions — validate: compiles, Init/Update/View satisfy interface
- [x] 5.2 Implement sidebar rendering: agent list with ticket ID, summary, status indicator (green/red dot), elapsed time, keybinding help at bottom — validate: test View output contains expected elements for mock data
- [x] 5.3 Implement terminal panel rendering: header bar (ticket ID, summary, command, elapsed, status) + terminal content area showing captured output — validate: test View contains header and content
- [x] 5.4 Implement j/k navigation to move selection in agent list, update terminal panel on selection change — validate: test Update with j/k key messages changes selected index
- [x] 5.5 Implement capture polling: `tea.Tick` at 200ms interval triggers `CaptureOutputMsg` → calls `AgentService.CaptureOutput` → updates terminal content — validate: test tick message triggers capture command
- [x] 5.6 Implement `s` keybinding: emit `SpawnAgentMsg` with current board card context — validate: test s key produces correct message
- [x] 5.7 Implement `X` keybinding: emit `KillAgentMsg` for selected agent — validate: test X key produces correct message, selection adjusts if last agent killed
- [x] 5.8 Implement `enter`/`tab` keybinding: emit `AttachSessionMsg` with tmux session name for selected agent — validate: test enter key produces correct message
- [x] 5.9 Implement `esc` keybinding: emit `ReturnToBoardMsg` — validate: test esc key produces correct message

## 6. TUI: App Integration

- [x] 6.1 Add `viewAgents` to `viewType` enum and `agents.Model` to `App` struct — validate: compiles, no regressions in existing tests
- [x] 6.2 Wire `A` key in board view to switch to `viewAgents`, trigger `ReconcileSessions`, start capture polling — validate: test A key changes active view
- [x] 6.3 Handle `ReturnToBoardMsg` from agent view: switch back to `viewBoard`, stop polling — validate: test message switches view
- [x] 6.4 Handle `AttachSessionMsg`: call `tea.ExecProcess` with the tmux attach command — validate: test message produces exec command
- [x] 6.5 Handle `SpawnAgentMsg`: call `AgentService.SpawnAgent`, refresh agent list — validate: test spawn flow
- [x] 6.6 Handle `KillAgentMsg`: call `AgentService.KillAgent`, refresh agent list — validate: test kill flow
- [x] 6.7 Render agent view in `View()` when `active == viewAgents` — validate: test View output changes based on active view

## 7. TUI: Board Agent Indicator

- [x] 7.1 Add `a` keybinding in board view to spawn agent on selected card (emit `SpawnAgentMsg`) or switch to agent view if already running — validate: test a key emits correct message
- [x] 7.2 Add agent running indicator (`▶` prefix) to card rendering when card has active agent session — validate: test card render includes indicator when agent active

## 8. Wiring & Smoke Test

- [x] 8.1 Wire `AgentService` and `tmux.Manager` in `cmd/legato/main.go`, pass to App constructor — validate: app compiles and starts
- [ ] 8.2 Manual smoke test: spawn agent from board, see it in agent view, attach/detach, kill, restart legato and verify reconnection
