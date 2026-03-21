## Why

The agent sidebar currently shows only the task ID (e.g., `abc12345`) for each agent entry. When multiple agents are running, users have to mentally map IDs to tasks. Showing the task title alongside the ID makes it immediately clear what each agent is working on.

## What Changes

- Enrich `service.AgentSession` with a `Title` field populated during `ListAgents()`
- Update agent sidebar entry rendering to show task title on a third line, truncated to fit the card width
- Agent sidebar entries grow from 2 lines to 3 lines (status+ID, command, title)
- Terminal header also displays the task title for the selected agent

## Capabilities

### New Capabilities

_None — this is a presentation enhancement to an existing capability._

### Modified Capabilities

- `agent-split-view`: Agent sidebar entries gain a truncated task title line, making cards taller (3 lines instead of 2)

## Impact

- `internal/service/agent.go` — `AgentSession` struct gains `Title` field; `ListAgents` populates it via store lookup
- `internal/tui/agents/model.go` — sidebar entry rendering adds title line; terminal header shows title
- `internal/service/agent_test.go` — tests updated for title population
- `internal/tui/agents/` tests if any — updated for new rendering
