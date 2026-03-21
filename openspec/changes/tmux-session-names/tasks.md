## 1. Engine Layer — Tmux Pane Command Query

- [ ] 1.1 Add `PaneCommands() (map[string]string, error)` to `TmuxManager` interface and implement in the real tmux manager. Runs `tmux list-panes -a -F "#{session_name} #{pane_current_command}" -f "#{m:legato-*,#{session_name}}"`, parses output into session-name→command map. Returns empty map when no sessions match.
- [ ] 1.2 Add tests for `PaneCommands` using `httptest`-style approach (mock exec via `LookPath` injection). Cover: sessions found, no sessions, tmux not installed.

## 2. Service Layer — Dynamic Command Population

- [ ] 2.1 Add `PaneCommands() (map[string]string, error)` to the `TmuxManager` interface in the service layer (or confirm it's already there from 1.1). Update `AgentService.ListAgents()` to call `PaneCommands()` after fetching sessions from the DB, and override `Command` for running sessions with the live value. Fall back to DB value for dead sessions or on error.
- [ ] 2.2 Add tests for `ListAgents` dynamic command population using `mockTmux`. Verify running sessions get live command, dead sessions keep DB value, and tmux query errors fall back gracefully.

## 3. Verification

- [ ] 3.1 Run full test suite (`task test`) to confirm no regressions. Manually verify in TUI that sidebar shows live process names (e.g., `claude`, `bash`) instead of `shell`.
