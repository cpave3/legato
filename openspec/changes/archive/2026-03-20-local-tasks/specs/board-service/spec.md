## MODIFIED Requirements

### Requirement: ListCards returns cards for a column

The `ListCards` method SHALL return cards for the given column. Cards represent tasks and SHALL include the task ID, title, priority, status, sort order, and warning indicator.

#### Scenario: Cards returned in sort order

- **WHEN** `ListCards` is called with a valid column name
- **THEN** cards are returned ordered by sort_order ascending, with `Card.ID` from `task.ID`, `Card.Title` from `task.Title`, and other fields mapped from the task

#### Scenario: Empty column

- **WHEN** `ListCards` is called for a column with no tasks
- **THEN** an empty slice is returned

#### Scenario: Invalid column name

- **WHEN** `ListCards` is called with a column name that doesn't exist
- **THEN** an error is returned

### Requirement: GetCard returns full card detail

The `GetCard` method SHALL return full detail for a task, including provider metadata when available.

#### Scenario: Local task detail

- **WHEN** `GetCard` is called for a local task (no provider)
- **THEN** the returned `CardDetail` SHALL have the task's core fields populated, `Provider` and `RemoteID` empty, and provider-specific fields (Assignee, Labels, etc.) empty

#### Scenario: Synced task detail

- **WHEN** `GetCard` is called for a synced task (provider set)
- **THEN** the returned `CardDetail` SHALL have core fields from the task and provider-specific fields parsed from `remote_meta` JSON

#### Scenario: Card does not exist

- **WHEN** `GetCard` is called with a non-existent ID
- **THEN** an error is returned

### Requirement: BoardService interface definition

The `BoardService` interface SHALL operate on tasks. The `Card` type SHALL have a `Title` field (replacing `Summary`). The `CardDetail` type SHALL include `Provider` and `RemoteID` fields.

#### Scenario: Interface is consumable by presentation layer

- **WHEN** the TUI or server imports `BoardService`
- **THEN** it SHALL be able to call all methods without knowing whether cards represent local tasks or synced tickets

#### Scenario: Card type reflects task schema

- **WHEN** a `Card` is returned from `ListCards`
- **THEN** it SHALL have fields: ID, Title, Priority, IssueType, Status, SortOrder, HasWarning, AgentActive

## ADDED Requirements

### Requirement: CreateTask via BoardService

The `BoardService` SHALL provide a `CreateTask` method for creating local tasks from the TUI.

#### Scenario: Creating a local task

- **WHEN** `CreateTask` is called with a title, column, and priority
- **THEN** a new task SHALL be created with a generated ID, inserted into the database, and a board refresh event published

#### Scenario: Creating a task with custom ID

- **WHEN** `CreateTask` is called with an optional custom ID
- **THEN** the task SHALL use that ID if available, or return an error if it conflicts
