## ADDED Requirements

### Requirement: Ephemeral task creation

The system SHALL support creating ephemeral tasks — lightweight task rows that exist solely to back an agent session without appearing on the kanban board.

#### Scenario: Creating an ephemeral task

- **WHEN** the user spawns an ephemeral agent with title "debugging auth flow"
- **THEN** the system SHALL insert a task row with `ephemeral = 1`, the given title, a generated 8-char alphanumeric ID, status set to the first column name, NULL provider, and no workspace

#### Scenario: Ephemeral task ID format

- **WHEN** an ephemeral task is created
- **THEN** its ID SHALL follow the same 8-char lowercase alphanumeric format as local tasks

### Requirement: Ephemeral tasks excluded from board

The board SHALL NOT display ephemeral tasks in any column or workspace view.

#### Scenario: Board listing filters ephemeral tasks

- **WHEN** the board loads cards via `ListTasksByStatus` or `ListTasksByStatusAndWorkspace`
- **THEN** tasks with `ephemeral = 1` SHALL be excluded from results

#### Scenario: Card count excludes ephemeral tasks

- **WHEN** the board displays column card counts
- **THEN** ephemeral tasks SHALL NOT be counted

#### Scenario: Search excludes ephemeral tasks

- **WHEN** the user searches for cards via the search overlay
- **THEN** ephemeral tasks SHALL NOT appear in search results

### Requirement: Ephemeral task database schema

The tasks table SHALL have a column to distinguish ephemeral tasks from regular tasks.

#### Scenario: Migration adds ephemeral column

- **WHEN** the database is migrated
- **THEN** the `tasks` table SHALL have an `ephemeral` column of type `INTEGER NOT NULL DEFAULT 0`

#### Scenario: Existing tasks default to non-ephemeral

- **WHEN** the migration runs on an existing database with tasks
- **THEN** all existing tasks SHALL have `ephemeral = 0`

### Requirement: Ephemeral agent lifecycle

Ephemeral agent sessions SHALL participate in the full agent lifecycle including state tracking, duration recording, and session reconciliation.

#### Scenario: State interval recording for ephemeral agent

- **WHEN** an ephemeral agent's activity state changes (working/waiting/clear)
- **THEN** the system SHALL record state intervals in `state_intervals` using the ephemeral task's ID (FK satisfied by the task row)

#### Scenario: Duration display for ephemeral agent

- **WHEN** the agent list displays an ephemeral agent session
- **THEN** it SHALL show accumulated working/waiting durations the same as any other agent

#### Scenario: Reconciliation of ephemeral agent sessions

- **WHEN** legato starts and an ephemeral agent's tmux session no longer exists
- **THEN** the system SHALL mark the session as `dead` following the standard reconciliation flow
