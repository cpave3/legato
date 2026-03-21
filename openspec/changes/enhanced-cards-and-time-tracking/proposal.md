## Why

Board cards are currently 3 lines tall and show minimal information (key, title, priority/type), leaving unused space in columns. Meanwhile, agent activity state transitions (working/waiting/idle) are tracked in real-time but not persisted historically — there's no way to see how long a task spent in each state. Taller, richer cards and persisted state duration tracking together give developers better at-a-glance context and retrospective insight into where time goes.

## What Changes

- **Taller card rendering**: Expand cards from ~3 lines to ~5-6 lines, adding room for:
  - Agent activity indicator on its own line (currently crammed into the key line)
  - Status/column name display
  - Time-in-state duration (e.g. "working 12m", "waiting 3h")
  - Warning/sync indicators with more breathing room
- **State duration tracking**: New `state_intervals` table recording timestamped intervals for each task's agent activity transitions (working → waiting → idle). Each row captures `task_id`, `state`, `started_at`, `ended_at`. Transitions close the previous interval and open a new one.
- **Duration display on cards**: Aggregate time-in-state shown on cards — e.g. total working time, total waiting time — computed from the intervals table.
- **Service layer support**: `AgentService` gains methods to record state transitions and query aggregated durations per task.

## Capabilities

### New Capabilities
- `state-duration-tracking`: Persistence and querying of agent activity state intervals (working/waiting/idle) per task, including the migration, store CRUD, and service layer aggregation.

### Modified Capabilities
- `kanban-board`: Card rendering changes — taller cards with additional data rows (agent state line, status line, duration display).

## Impact

- **Database**: New `state_intervals` migration (007), new store methods for interval CRUD and aggregation queries.
- **Store layer** (`internal/engine/store/`): New `StateInterval` struct, insert/close/query methods.
- **Service layer** (`internal/service/`): `AgentService` extended with duration tracking on activity updates, new `GetStateDurations(taskID)` method.
- **TUI board** (`internal/tui/board/card.go`): Card height increase, new rendering rows, `CardData` gains duration fields.
- **CLI** (`internal/cli/`): `AgentState` handler triggers interval recording alongside existing activity update.
- **IPC**: No protocol changes needed — existing `agent_state` message type suffices.
