## Why

When attached to a legato tmux session, there's no visibility into what other agents are doing — you have to detach back to the agent view to see if something is waiting for input. Since legato already controls tmux session options, it can inject a custom status line that shows a live summary of agent states, keeping the operator informed without leaving their terminal.

## What Changes

- New CLI subcommand `legato agent summary` that outputs a compact, pre-formatted string of agent session counts (e.g., `2 running · 1 waiting · 0 idle`)
- Legato-spawned tmux sessions get a custom status line that shells out to `legato agent summary` on a refresh interval
- New tmux status line styling configuration applied automatically to legato sessions (distinct from user's global tmux config)

## Capabilities

### New Capabilities
- `tmux-statusline`: Custom tmux status bar for legato sessions showing live agent state summary via CLI

### Modified Capabilities

## Impact

- `internal/cli/` — new `AgentSummary` handler
- `cmd/legato/` — new `agent summary` subcommand dispatch
- `internal/service/agent.go` — new method to query aggregate session counts
- `internal/engine/tmux/` — additional `set-option` calls during spawn for status line configuration
- Config: possible new `agents.statusline` config field for customization
