## ADDED Requirements

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
