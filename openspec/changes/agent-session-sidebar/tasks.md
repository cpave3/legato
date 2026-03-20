## 1. Engine: Tmux spawn with dimensions

- [ ] 1.1 Add `Width` and `Height` parameters to `tmux.Manager.Spawn()` — pass as `-x <width> -y <height>` flags to `tmux new-session`. Default to omitting flags if zero (backward compat). Update tests.
- [ ] 1.2 Update `TmuxManager` interface in `internal/service/agent.go` to include width/height in the spawn signature. Update `mockTmux` in tests.

## 2. Service: Thread dimensions through SpawnAgent

- [ ] 2.1 Add `Width` and `Height` fields to `AgentService.SpawnAgent` (or its options). Pass through to `TmuxManager.Spawn()`. Update tests with mock assertions on dimensions.

## 3. TUI: Sidebar component

- [ ] 3.1 Add sidebar rendering function in `internal/tui/agents/model.go`: fixed 30-char wide panel with right border, renders agent entries as single-line rows (activity icon + task ID + activity label). Include empty state.
- [ ] 3.2 Add sidebar activity indicators: green icon + "WORKING" for working, blue icon + "waiting" for waiting, dim icon + "idle" for running with no activity, red icon + "dead" for dead sessions.
- [ ] 3.3 Add sidebar selection highlighting: selected entry gets highlighted background + bold task ID. Unselected entries use muted styling.
- [ ] 3.4 Add keybinding hints at bottom of sidebar (j/k, s, X, enter, esc). Ensure hints don't overlap agent list — list scrolls if it exceeds available height.

## 4. TUI: Layout refactor

- [ ] 4.1 Replace full-width header bar layout with `lipgloss.JoinHorizontal(sidebar, terminalPanel)`. Sidebar is 30 chars, terminal panel fills remaining width.
- [ ] 4.2 Move header bar content (task ID, command, elapsed time, status) into a terminal panel header — only spans the terminal panel width, not full width.
- [ ] 4.3 Compute `terminalWidth()` as `m.width - sidebarWidth` and use it for terminal output rendering (truncate/pad lines to fit).

## 5. TUI: Spawn with dimensions

- [ ] 5.1 Update `SpawnAgentMsg` (or equivalent) to include terminal panel width and height. Compute from `m.width - sidebarWidth` and `m.height`.
- [ ] 5.2 Wire the dimensions through `app.go` → `AgentService.SpawnAgent()` so new tmux sessions are created at the correct size.

## 6. Testing

- [ ] 6.1 Test sidebar rendering: verify agent entries render with correct icons for each activity state, selection highlighting works, and keybinding hints are present.
- [ ] 6.2 Test layout: verify sidebar is 30 chars wide, terminal panel uses remaining width, and dimensions are passed through to spawn.
- [ ] 6.3 Test edge cases: empty agent list shows spawn hint, long task IDs are truncated with ellipsis, sidebar scrolls when entries exceed height.
