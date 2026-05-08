## ADDED Requirements

### Requirement: Swarm decomposition overlay

The TUI SHALL provide a swarm decomposition overlay accessible via the `s` key on the board view (and `s` on the detail view) for the selected task.

#### Scenario: Open the overlay

- **WHEN** the user selects a task and presses `s`
- **THEN** the swarm overlay SHALL open with one empty sub-task row pre-populated

#### Scenario: Add and remove sub-task rows

- **WHEN** the overlay is open
- **AND** the user presses `tab` to add a sub-task row OR `shift+tab` on a focused row to remove it
- **THEN** the row list SHALL update without losing other field values

#### Scenario: Per-row fields

- **WHEN** a sub-task row is rendered
- **THEN** it SHALL include inputs for: title (text), scope_globs (comma-separated text), role (cycle via `h`/`l` through `builder|scout|reviewer`)

#### Scenario: Submit decomposition

- **WHEN** the user presses `enter` with at least one valid sub-task row
- **THEN** the overlay SHALL emit a `SwarmDecomposeMsg` with the sub-task list and close

#### Scenario: Cancel

- **WHEN** the user presses `esc`
- **THEN** the overlay SHALL close without changes

### Requirement: Swarm graph in detail view

When viewing a swarm parent task in the detail view, the system SHALL render a sub-task graph showing each sub-task, its role, scope, status, and assigned agent.

#### Scenario: Detail view for swarm parent

- **WHEN** a task with at least one sub-task is opened in detail view
- **THEN** a "Swarm" section SHALL render below the metadata header listing each sub-task: `[<status-icon>] <role>: <title> — <scope>` with the agent ID if running

#### Scenario: Navigate to sub-task

- **WHEN** the user presses `j`/`k` on the swarm section to highlight a sub-task and presses `enter`
- **THEN** focus SHALL move to that sub-task's detail (sub-task description, builder logs, reviewer notes)

### Requirement: Review action keybindings

When a sub-task in `review` state is focused, the detail view SHALL surface keybindings to approve or reject.

#### Scenario: Approve sub-task

- **WHEN** the user presses `a` while focused on a sub-task in `review` state
- **THEN** the system SHALL call `SwarmService.Review(subtaskID, approve=true, notes="")` and the sub-task SHALL transition to `done`

#### Scenario: Reject sub-task with notes

- **WHEN** the user presses `r` while focused on a sub-task in `review` state
- **THEN** the system SHALL prompt for rejection notes via a single-line input overlay
- **AND** on submit, SHALL call `SwarmService.Review(subtaskID, approve=false, notes=<input>)`

### Requirement: Coordination surface visibility

The agent split-view SHALL display the swarm coordination surface as an additional panel when the focused agent has a `parent_task_id`.

#### Scenario: Split-view shows coordination surface

- **WHEN** the user focuses on a swarm agent in the agents view
- **THEN** a third panel SHALL render the JSON snapshot from `SwarmService.Snapshot(parentTaskID)` formatted as a list of sibling sub-tasks with status badges
