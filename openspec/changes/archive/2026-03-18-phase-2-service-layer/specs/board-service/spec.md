## ADDED Requirements

### Requirement: BoardService interface definition

The system SHALL define a `BoardService` interface in `internal/service/interfaces.go` with the following methods: `ListColumns`, `ListCards`, `GetCard`, `MoveCard`, `ReorderCard`, `SearchCards`, and `ExportCardContext`. All methods MUST accept a `context.Context` as the first parameter. The interface MUST NOT import any presentation-layer packages.

#### Scenario: Interface is consumable by presentation layer

- **WHEN** the `BoardService` interface is defined
- **THEN** it SHALL be possible to write a consumer that depends only on the interface, with no dependency on the concrete implementation

#### Scenario: Interface matches spec contract

- **WHEN** a developer inspects the `BoardService` interface
- **THEN** it SHALL match the signatures defined in spec.md section 3.4, including return types `[]Column`, `[]Card`, `*CardDetail`, and `error`

### Requirement: ListColumns returns configured board columns

`BoardService.ListColumns` SHALL return all configured kanban columns in their defined sort order. Each `Column` MUST include the column name and sort order.

#### Scenario: Columns returned in order

- **WHEN** `ListColumns` is called
- **THEN** it SHALL return columns ordered by their `sort_order` from the `column_mappings` table

#### Scenario: No columns configured

- **WHEN** `ListColumns` is called and no column mappings exist
- **THEN** it SHALL return an empty slice and no error

### Requirement: ListCards returns cards for a column

`BoardService.ListCards` SHALL return all cards in the specified column, ordered by `sort_order`. Each `Card` MUST include at minimum: id (issue key), summary, priority, and issue type.

#### Scenario: Cards returned in sort order

- **WHEN** `ListCards` is called with a valid column name
- **THEN** it SHALL return cards in that column ordered by `sort_order` ascending

#### Scenario: Empty column

- **WHEN** `ListCards` is called for a column with no cards
- **THEN** it SHALL return an empty slice and no error

#### Scenario: Invalid column name

- **WHEN** `ListCards` is called with a column name that does not exist
- **THEN** it SHALL return an error indicating the column was not found

### Requirement: GetCard returns full card detail

`BoardService.GetCard` SHALL return the full detail for a single card, including all metadata fields and the rendered markdown description.

#### Scenario: Card exists

- **WHEN** `GetCard` is called with a valid card ID
- **THEN** it SHALL return a `*CardDetail` containing id, summary, description_md, status, priority, issue_type, assignee, labels, epic_key, epic_name, url, created_at, and updated_at

#### Scenario: Card does not exist

- **WHEN** `GetCard` is called with an ID that does not exist in the store
- **THEN** it SHALL return `nil` and an error indicating the card was not found

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

`BoardService.SearchCards` SHALL search across card id (issue key) and summary fields, returning all cards that match the query string. The search MUST be case-insensitive.

#### Scenario: Query matches cards

- **WHEN** `SearchCards` is called with a query that matches one or more card summaries or issue keys
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
- **THEN** it SHALL return a markdown string containing the issue key and summary as a heading followed by the markdown description body

#### Scenario: Full structured block format

- **WHEN** `ExportCardContext` is called with `ExportFormatFull`
- **THEN** it SHALL return a markdown string containing metadata (summary, type, priority, epic, labels, URL) followed by a separator and the full description, matching the format defined in spec.md section 8.1

#### Scenario: Card not found

- **WHEN** `ExportCardContext` is called with an ID that does not exist
- **THEN** it SHALL return an empty string and an error
