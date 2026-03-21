## Requirements

### Requirement: State intervals table

The system SHALL persist agent activity state intervals in a `state_intervals` SQLite table. Each row records a task ID, state name, start time, and optional end time.

#### Scenario: Table schema

- **WHEN** migration 007 is applied
- **THEN** the `state_intervals` table SHALL exist with columns: `id` (INTEGER PRIMARY KEY AUTOINCREMENT), `task_id` (TEXT NOT NULL, FK to tasks.id with CASCADE delete), `state` (TEXT NOT NULL, CHECK IN ('working', 'waiting')), `started_at` (DATETIME NOT NULL DEFAULT datetime('now')), `ended_at` (DATETIME nullable), and an index on `task_id`

### Requirement: State transition recording

The store SHALL record state transitions by closing any open interval and optionally opening a new one.

#### Scenario: Transition from one state to another

- **WHEN** `RecordStateTransition(taskID, "waiting")` is called and the task has an open interval with state "working"
- **THEN** the store SHALL set `ended_at = datetime('now')` on the open interval AND insert a new row with state "waiting" and `ended_at = NULL`

#### Scenario: Transition to empty state (clear)

- **WHEN** `RecordStateTransition(taskID, "")` is called and the task has an open interval
- **THEN** the store SHALL set `ended_at = datetime('now')` on the open interval AND SHALL NOT insert a new row

#### Scenario: Transition when no open interval exists

- **WHEN** `RecordStateTransition(taskID, "working")` is called and the task has no open interval
- **THEN** the store SHALL insert a new row with state "working" and `ended_at = NULL`

#### Scenario: Idempotent same-state transition

- **WHEN** `RecordStateTransition(taskID, "working")` is called and the task already has an open interval with state "working"
- **THEN** the store SHALL NOT close the existing interval and SHALL NOT insert a new row

### Requirement: Duration aggregation query

The store SHALL provide a method to query aggregated durations per state for a given task.

#### Scenario: Aggregating completed intervals

- **WHEN** `GetStateDurations(taskID)` is called for a task with multiple completed intervals
- **THEN** it SHALL return a map of state → total duration (as `time.Duration`), computed as the sum of `ended_at - started_at` for all intervals of each state

#### Scenario: Including open interval in aggregation

- **WHEN** `GetStateDurations(taskID)` is called and the task has an open (active) interval
- **THEN** the open interval's duration SHALL be calculated as `now - started_at` and included in the total for its state

#### Scenario: No intervals exist

- **WHEN** `GetStateDurations(taskID)` is called for a task with no intervals
- **THEN** it SHALL return an empty map

### Requirement: Batch duration query

The store SHALL support querying durations for multiple tasks in a single call to avoid N+1 queries during board loading.

#### Scenario: Batch query for board

- **WHEN** `GetStateDurationsBatch(taskIDs)` is called with a list of task IDs
- **THEN** it SHALL return a map of taskID → (state → duration) for all tasks that have intervals, using a single SQL query

### Requirement: Service layer integration

The `AgentService` SHALL record state transitions when agent activity is updated.

#### Scenario: Activity update triggers interval recording

- **WHEN** `UpdateAgentActivity(taskID, activity)` is called (from CLI or IPC handler)
- **THEN** the service SHALL call `store.RecordStateTransition(taskID, activity)` after updating the activity column

#### Scenario: Reconcile closes orphaned intervals

- **WHEN** `ReconcileSessions()` detects a dead agent session
- **THEN** it SHALL close any open intervals for that task's ID by calling `RecordStateTransition(taskID, "")`
