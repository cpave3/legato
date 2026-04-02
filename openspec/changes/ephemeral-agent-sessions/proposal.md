## Why

Users want to spin up managed tmux sessions through Legato's agent view without needing a backing task/ticket — for ad-hoc exploration, debugging, or general terminal work. Currently, every agent session requires a selected board card, which forces users to create throwaway tasks just to get a managed terminal.

## What Changes

- Add an "ephemeral task" concept: lightweight task rows created inline when spawning an agent without a backing ticket. These appear in the agent list but not on the kanban board.
- Allow spawning agents from the agent view (`s` key) with a title prompt — no board card selection required.
- Ephemeral tasks get a title (user-provided) so sessions are distinguishable in the agent list.
- Ephemeral agent sessions participate in the full agent lifecycle (spawn, attach, kill, capture-pane, state tracking) like any other agent.
- Ephemeral tasks are excluded from the kanban board view.

## Capabilities

### New Capabilities
- `ephemeral-agent`: Ephemeral agent session creation — spawning task-free managed tmux sessions with user-provided titles, lifecycle management, and board exclusion logic.

### Modified Capabilities
- `agent-session`: Agent spawning flow gains a new entry point (spawn without pre-existing task) and must handle ephemeral task creation.
- `agent-split-view`: Agent view UI adds spawn trigger (`s` key) with title input overlay.

## Impact

- `internal/engine/store/` — tasks table gains an `ephemeral` flag (or equivalent marker) to distinguish ephemeral tasks from regular ones
- `internal/service/agent.go` — `SpawnAgent` gains a path for creating an ephemeral task row before spawning
- `internal/tui/agents/` — new spawn-from-agent-view flow with title input
- `internal/tui/overlay/` — possible new overlay or reuse of title input overlay for ephemeral naming
- `internal/service/board.go` — board queries filter out ephemeral tasks
- `state_intervals` FK constraint is satisfied because ephemeral sessions have a real task row
