## Why

The agent sidebar currently shows "shell" for every session because the `Command` field is hardcoded at spawn time. This makes it impossible to distinguish between sessions at a glance — the second line of each sidebar entry is identical and provides no useful information.

## What Changes

- Query tmux for the current pane command (`pane_current_command` format variable) dynamically instead of storing a static "shell" label
- Update the sidebar rendering to show the live process name (e.g., `claude`, `bash`, `vim`) instead of the hardcoded "shell"
- Add a method to the `TmuxManager` interface for querying pane metadata

## Capabilities

### New Capabilities

### Modified Capabilities
- `agent-session`: Add dynamic pane command querying to tmux manager and surface it through the agent service
- `agent-split-view`: Update sidebar rendering to display live tmux pane command instead of static "shell"

## Impact

- `internal/engine/tmux/` — new method to query `pane_current_command` via `tmux list-panes -t <session> -F "#{pane_current_command}"`
- `internal/service/agent.go` — populate `Command` field dynamically from tmux instead of DB
- `internal/tui/agents/model.go` — rendering already uses `Command` field, so sidebar updates automatically
- No database migration needed — the static `command` column becomes less relevant but doesn't need removal
