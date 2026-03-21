## Requirements

### Requirement: BoardService interface definition

The system SHALL define a `BoardService` interface in `internal/service/interfaces.go` with the following methods: `ListColumns`, `ListCards`, `GetCard`, `MoveCard`, `ReorderCard`, `SearchCards`, `ExportCardContext`, `CreateTask`, and `DeleteTask`. All methods MUST accept a `context.Context` as the first parameter. The interface MUST NOT import any presentation-layer packages. The `BoardService` interface SHALL operate on tasks. The `Card` type SHALL have a `Title` field (replacing `Summary`). The `CardDetail` type SHALL include `Provider` and `RemoteID` fields.

#### Scenario: Interface is consumable by presentation layer

- **WHEN** the `BoardService` interface is defined
- **THEN** it SHALL be possible to write a consumer that depends only on the interface, with no dependency on the concrete implementation

#### Scenario: Interface matches spec contract

- **WHEN** a developer inspects the `BoardService` interface
- **THEN** it SHALL match the signatures defined in spec.md section 3.4, including return types `[]Column`, `[]Card`, `*CardDetail`, and `error`

#### Scenario: Card type reflects task schema

- **WHEN** a `Card` is returned from `ListCards`
- **THEN** it SHALL have fields: ID, Title, Priority, IssueType, Status, SortOrder, HasWarning, AgentActive

### Requirement: ListColumns returns configured board columns

`BoardService.ListColumns` SHALL return all configured kanban columns in their defined sort order. Each `Column` MUST include the column name and sort order.

#### Scenario: Columns returned in order

- **WHEN** `ListColumns` is called
- **THEN** it SHALL return columns ordered by their `sort_order` from the `column_mappings` table

#### Scenario: No columns configured

- **WHEN** `ListColumns` is called and no column mappings exist
- **THEN** it SHALL return an empty slice and no error

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

### Requirement: MoveCard transitions a card between columns

`BoardService.MoveCard` SHALL update the card's `status` field to the target column and set its `sort_order` to the end of the target column. It MUST publish an `EventCardMoved` event through the `EventBus` after a successful move.

#### Scenario: Successful move

- **WHEN** `MoveCard` is called with a valid card ID and a valid target column
- **THEN** the card's status SHALL be updated to the target column, its sort_order SHALL be set to place it at the end of the target column, and an `EventCardMoved` event SHALL be published

#### Scenario: Move to same column

- **WHEN** `MoveCard` is called with the card's current column as the target
- **THEN** it SHALL return successfully without modifying the card or publishing an event

#### Scenario: Invalid target column

- **WHEN** `MoveCard` is called with a column name that does not exist
- **THEN** it SHALL return an error and not modify the card

### Requirement: ReorderCard changes position within a column

`BoardService.ReorderCard` SHALL update the card's `sort_order` to the specified position and adjust the sort_order of other cards in the same column to maintain a consistent ordering. It MUST publish an `EventCardUpdated` event on success.

#### Scenario: Reorder within column

- **WHEN** `ReorderCard` is called with a valid card ID and a new position
- **THEN** the card SHALL be placed at the specified position and other cards' sort_orders SHALL be adjusted to avoid gaps or duplicates

#### Scenario: Position out of range

- **WHEN** `ReorderCard` is called with a position greater than the number of cards in the column
- **THEN** the card SHALL be placed at the end of the column

### Requirement: SearchCards performs text search

`BoardService.SearchCards` SHALL search across card id (issue key) and title fields, returning all cards that match the query string. The search MUST be case-insensitive.

#### Scenario: Query matches cards

- **WHEN** `SearchCards` is called with a query that matches one or more card titles or issue keys
- **THEN** it SHALL return all matching cards regardless of which column they belong to

#### Scenario: No matches

- **WHEN** `SearchCards` is called with a query that matches no cards
- **THEN** it SHALL return an empty slice and no error

#### Scenario: Empty query

- **WHEN** `SearchCards` is called with an empty string
- **THEN** it SHALL return all cards across all columns

### Requirement: ExportCardContext formats card for clipboard

`BoardService.ExportCardContext` SHALL format a card's content as a markdown string in the requested format. It MUST support at least two formats: description-only and full structured block.

#### Scenario: Description-only format

- **WHEN** `ExportCardContext` is called with `ExportFormatDescription`
- **THEN** it SHALL return a markdown string containing the issue key and title as a heading followed by the markdown description body

#### Scenario: Full structured block format

- **WHEN** `ExportCardContext` is called with `ExportFormatFull`
- **THEN** it SHALL return a markdown string containing metadata (title, type, priority, epic, labels, URL) followed by a separator and the full description, matching the format defined in spec.md section 8.1

#### Scenario: Card not found

- **WHEN** `ExportCardContext` is called with an ID that does not exist
- **THEN** it SHALL return an empty string and an error

### Requirement: CreateTask via BoardService

The `BoardService` SHALL provide a `CreateTask` method for creating local tasks from the TUI.

#### Scenario: Creating a local task

- **WHEN** `CreateTask` is called with a title, column, and priority
- **THEN** a new task SHALL be created with a generated ID, inserted into the database, and a board refresh event published

#### Scenario: Creating a task with custom ID

- **WHEN** `CreateTask` is called with an optional custom ID
- **THEN** the task SHALL use that ID if available, or return an error if it conflicts

### Requirement: DeleteTask via BoardService

The `BoardService` SHALL provide a `DeleteTask` method for removing tasks.

#### Scenario: Deleting a local task

- **WHEN** `DeleteTask` is called with a valid task ID
- **THEN** the task SHALL be verified to exist, deleted from the store, and an `EventCardsRefreshed` event published

#### Scenario: Deleting a remote-tracking task

- **WHEN** `DeleteTask` is called for a task with a provider set
- **THEN** only the local reference SHALL be removed; no remote deletion SHALL occur

### Requirement: ArchiveDoneCards operation

The `BoardService` SHALL provide an `ArchiveDoneCards` method that archives all non-archived tasks in the Done column. It SHALL determine the Done column status from `column_mappings`, call the store's bulk archive method, publish an `EventCardsRefreshed` event, and return the count of archived tasks.

#### Scenario: Archive done cards
- **WHEN** `ArchiveDoneCards` is called and there are done cards
- **THEN** the store's `ArchiveTasksByStatus` SHALL be called with the Done column's status, an `EventCardsRefreshed` event SHALL be published, and the count SHALL be returned

#### Scenario: Archive done cards with no done cards
- **WHEN** `ArchiveDoneCards` is called and there are no done cards
- **THEN** the method SHALL return 0 and still publish `EventCardsRefreshed`

### Requirement: ArchiveTask operation

The `BoardService` SHALL provide an `ArchiveTask(id)` method that archives a single task. It SHALL verify the task exists and is in the Done column before archiving. It SHALL publish `EventCardsRefreshed` after archiving.

#### Scenario: Archive a done task
- **WHEN** `ArchiveTask` is called with a task in the Done column
- **THEN** the task SHALL be archived and `EventCardsRefreshed` SHALL be published

#### Scenario: Archive a non-done task
- **WHEN** `ArchiveTask` is called with a task not in the Done column
- **THEN** an error SHALL be returned and no task SHALL be modified

### Requirement: CountDoneCards operation

The `BoardService` SHALL provide a `CountDoneCards` method that returns the number of non-archived tasks in the Done column. This is used by the confirmation overlay to show the count before archiving.

#### Scenario: Count done cards
- **WHEN** `CountDoneCards` is called and there are 3 non-archived done tasks
- **THEN** it SHALL return 3
