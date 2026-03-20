## Why

Legato spawns AI agents in tmux sessions but has no visibility into what those agents are actually doing. Developers must manually update task status when an agent finishes work, encounters errors, or progresses through subtasks. A deep integration with Claude Code — the first of potentially many AI tool integrations — would let the agent automatically report its lifecycle events back to Legato, keeping the board in sync with reality in real-time.

## What Changes

- **New CLI subcommands** for Legato (`legato hooks install`, `legato task update`, `legato task note`) that allow external tools to interact with the running board
- **Abstract AI tool adapter interface** in the service layer, so Claude Code is the first implementation but Linear-connected agents, Cursor, Aider, etc. can follow the same pattern
- **Claude Code hooks integration**: Legato installs Claude Code hooks (via `.claude/settings.json`) that fire on relevant events (`Stop`, `TaskCompleted`, `PostToolUse`, etc.) and call `legato` CLI to push state updates
- **Environment variable injection** into tmux sessions (`LEGATO_TASK_ID`, `LEGATO_SOCKET`) so hook scripts know which task they're operating on and how to reach the running Legato instance
- **IPC mechanism** (Unix domain socket) so CLI commands from hook scripts can notify the running TUI instance, which reactively updates the board via the existing event bus
- **Hook script generation** that produces the shell scripts Claude Code hooks will execute, with proper `jq`/stdin parsing and `LEGATO_TASK_ID` guard checks

## Capabilities

### New Capabilities
- `ai-tool-adapter`: Abstract interface for AI tool integrations — defines how external AI tools report lifecycle events (started, progress, completed, failed) back to Legato, plus adapter registration and discovery
- `claude-code-hooks`: Claude Code-specific adapter implementation — hook script generation, `legato hooks install` CLI, event mapping from Claude Code hook events to Legato task updates
- `legato-cli`: CLI subcommands (`legato task update`, `legato task note`, `legato hooks install`) for external tools and hook scripts to interact with Legato
- `ipc-socket`: Unix domain socket IPC so CLI invocations from hooks can notify the running TUI instance in real-time, bridging to the existing event bus

### Modified Capabilities
- (none — existing specs unchanged; tmux `Spawn` gains env var injection but that's implementation detail)

## Impact

- **New packages**: `internal/engine/ipc/` (socket server/client), `internal/engine/hooks/` (hook script templates), `internal/service/adapter.go` (AI tool adapter interface)
- **Modified packages**: `internal/engine/tmux/` (env var injection on Spawn), `cmd/legato/` (new CLI subcommands + socket server lifecycle)
- **New binary behavior**: `legato` gains subcommands alongside the default TUI mode
- **External dependency**: Claude Code must be installed for hooks integration; graceful degradation when absent
- **File system**: writes to `.claude/settings.json` (project-level) during `legato hooks install`
- **No breaking changes** to existing functionality
