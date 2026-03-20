## Why

Legato tracks tickets but doesn't help execute on them. Developers using AI coding agents (Claude Code, aider, OpenCode) must manually manage terminal sessions alongside the board. Adding in-TUI agent management lets users spawn, monitor, and steer persistent agent sessions directly from their kanban cards — closing the loop between planning and execution.

## What Changes

- New split-view screen showing active agents (left sidebar) and a live terminal panel (right) for the selected agent
- Spawn a persistent tmux session from any card on the board — session survives legato exit
- Track agent sessions in SQLite so legato can reassociate with running tmux sessions on restart
- Embed the tmux session output in the TUI with the ability to drop in and type commands
- Configurable escape key (default Ctrl+]) to return focus from the terminal back to legato
- New keybindings: spawn agent, kill agent, navigate agent list, toggle between board and agent view
- v1 spawns a plain shell; architecture is modular to support command presets and worktree integration later

## Capabilities

### New Capabilities
- `agent-session`: Tmux session lifecycle management — spawn, track, kill, and reconnect persistent terminal sessions tied to cards. SQLite persistence for session metadata.
- `agent-split-view`: TUI split-view mode with agent list sidebar and embedded terminal panel. Navigation, focus management, and escape-key handling.

### Modified Capabilities
- `tui-app-shell`: New view mode for agent split view, keybinding to toggle between board and agent view
- `kanban-board`: New keybinding to spawn an agent from the currently selected card

## Impact

- **New packages**: `internal/engine/tmux/` (tmux process management), `internal/tui/agents/` (agent view model)
- **Modified packages**: `internal/tui/` (view routing, new mode), `internal/tui/board/` (spawn keybinding), `internal/engine/store/` (new migration for agent sessions table)
- **New dependency**: tmux must be installed on the host system
- **Database**: New `agent_sessions` table tracking tmux session name, card ID, status, spawn time
- **Config**: Optional `agents.escape_key` config field (default `ctrl+]`)
