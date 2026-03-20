## Why

The agent view currently uses a full-width header bar to show which agent is selected, with the terminal output below. When multiple agents are running, there's no way to see all their statuses at a glance — you have to cycle through them with j/k. This makes it easy to miss that an agent is waiting for input. A persistent sidebar showing all sessions with their activity state (working/waiting/idle) would let users monitor everything at once, similar to how the board view shows agent indicators on cards.

The previous attempt at a sidebar changed the terminal content width dynamically, causing capture-pane output to reflow. The fix: render the tmux session with the sidebar always present, so the terminal panel width is constant from the moment the session spawns.

## What Changes

- Replace the full-width header bar in the agent view with a left sidebar listing all agent sessions
- Each sidebar entry shows: task ID, activity state indicator (working/waiting/idle), command name, elapsed time
- Selected agent is highlighted; terminal panel on the right shows that agent's capture-pane output
- Sidebar is always rendered (even with one agent), keeping terminal panel width constant
- Tmux sessions are spawned at a width matching the terminal panel (total width minus sidebar width), so capture-pane output never reflows when navigating between agents
- Activity state indicators in sidebar match the board's visual language (green for working, blue for waiting, dim for idle)

## Capabilities

### New Capabilities
- `agent-sidebar`: Persistent sidebar component listing all agent sessions with real-time status indicators and selection state

### Modified Capabilities
- `agent-split-view`: Layout changes from header-bar + terminal to sidebar + terminal; tmux spawn width adjusted to match terminal panel width
- `agent-session`: Tmux capture and spawn must accept explicit width parameters to keep output consistent with sidebar layout

## Impact

- `internal/tui/agents/model.go` — Major refactor: replace header bar layout with sidebar + terminal panel split
- `internal/engine/tmux/tmux.go` — Add width parameter to `Spawn()` and potentially `Capture()`
- `internal/service/agent.go` — `SpawnAgent` needs to pass terminal panel width to tmux
- `internal/tui/app.go` — May need to pass available width to agent view for sidebar width calculation
