## MODIFIED Requirements

### Requirement: task note subcommand

The system SHALL provide a `legato task note <task-id> <message>` subcommand that appends a timestamped note to a task. Notes SHALL be persisted as entries in the knowledge-memory system linked to the task (`note_links` with `target_kind='task'`) rather than appended to the task's free-text description.

#### Scenario: Adding a note to a task

- **WHEN** `legato task note abc123 "Fixed the auth bug"` is executed
- **THEN** the system SHALL create or append to the task's "task-notes" entry (a synthetic note slug `task-<task-id>-notes` with a timestamped bullet)
- **AND** a `note_links` row SHALL link the note to the task with `target_kind='task'` and `target_id='abc123'`
- **AND** the system SHALL send an IPC message to trigger a board refresh
- **AND** the command SHALL exit with code 0

#### Scenario: Multiple notes on a task

- **WHEN** `legato task note abc123 "first"` is executed and then `legato task note abc123 "second"` is executed
- **THEN** the same synthetic note SHALL contain both timestamped entries as a chronological list

#### Scenario: Detail view shows task notes

- **WHEN** a task with notes is opened in the detail view
- **THEN** a "Notes" section SHALL render listing each note entry with timestamp; backlinks from other notes to the task SHALL also appear

## ADDED Requirements

### Requirement: Task note migration on startup

On first startup after this change is applied, the system SHALL migrate existing task descriptions that contain the legacy timestamped-note format into synthetic notes in the knowledge-memory system.

#### Scenario: Description with legacy notes

- **WHEN** legato starts and a task description contains lines matching `^\[YYYY-MM-DD HH:MM:SS\] .+`
- **THEN** the system SHALL extract those lines into a synthetic note slug `task-<task-id>-notes`, link the note to the task, and remove the lines from the description
- **AND** an idempotency marker SHALL be set so the migration runs at most once per task

#### Scenario: Description without legacy notes

- **WHEN** a task description has no lines matching the legacy format
- **THEN** the migration SHALL be a no-op for that task
