## Requirements

### Requirement: Archive done cards

The system SHALL provide an `ArchiveDoneCards` operation that marks all tasks in the Done column as archived by setting their `archived_at` timestamp to the current time. The operation SHALL return the count of archived tasks. The operation SHALL only archive tasks whose current status maps to the Done column.

#### Scenario: Bulk archive done cards
- **WHEN** `ArchiveDoneCards` is called and there are 5 tasks in the Done column
- **THEN** all 5 tasks SHALL have `archived_at` set to the current timestamp, and the method SHALL return count 5

#### Scenario: Bulk archive with no done cards
- **WHEN** `ArchiveDoneCards` is called and the Done column is empty
- **THEN** zero tasks SHALL be archived and the method SHALL return count 0

#### Scenario: Bulk archive skips non-done cards
- **WHEN** `ArchiveDoneCards` is called and there are tasks in Backlog, Ready, Doing, and Review columns
- **THEN** none of those tasks SHALL be archived

#### Scenario: Already archived cards are not re-archived
- **WHEN** `ArchiveDoneCards` is called and some Done cards are already archived
- **THEN** only non-archived Done cards SHALL be affected

### Requirement: Archive individual task

The system SHALL provide an `ArchiveTask(id)` operation that marks a single task as archived. The task MUST be in the Done column. Attempting to archive a task not in Done SHALL return an error.

#### Scenario: Archive a single done task
- **WHEN** `ArchiveTask` is called with a valid task ID in the Done column
- **THEN** that task SHALL have `archived_at` set to the current timestamp

#### Scenario: Archive a task not in Done
- **WHEN** `ArchiveTask` is called with a task ID in the Doing column
- **THEN** the operation SHALL return an error and the task SHALL NOT be modified

#### Scenario: Archive a nonexistent task
- **WHEN** `ArchiveTask` is called with a task ID that does not exist
- **THEN** the operation SHALL return an error

### Requirement: Archived tasks are hidden from the board

All board listing operations SHALL exclude archived tasks. This includes `ListCards`, `ListCardsByWorkspace`, and `SearchCards`.

#### Scenario: Archived task not shown in column
- **WHEN** a task is archived and `ListCards` is called for its column
- **THEN** the archived task SHALL NOT appear in the results

#### Scenario: Archived task not shown in search
- **WHEN** a task is archived and `SearchCards` is called with a query matching that task
- **THEN** the archived task SHALL NOT appear in the search results

#### Scenario: Archived task not shown in workspace view
- **WHEN** a task is archived and `ListCardsByWorkspace` is called
- **THEN** the archived task SHALL NOT appear in the results

### Requirement: Archive confirmation overlay

The board SHALL present a confirmation overlay before archiving. For bulk archive (`X` key), the overlay SHALL show the count of done cards that will be archived. The user SHALL confirm with `y` or cancel with `n`/`esc`.

#### Scenario: Bulk archive confirmation shown
- **WHEN** the user presses `X` on the board and there are done cards
- **THEN** a confirmation overlay SHALL appear showing "Archive N done cards?"

#### Scenario: Bulk archive confirmed
- **WHEN** the user presses `y` on the archive confirmation overlay
- **THEN** the archive operation SHALL execute and the board SHALL refresh

#### Scenario: Bulk archive cancelled
- **WHEN** the user presses `n` or `esc` on the archive confirmation overlay
- **THEN** no cards SHALL be archived and the overlay SHALL close

#### Scenario: No done cards to archive
- **WHEN** the user presses `X` on the board and there are no done cards
- **THEN** no overlay SHALL appear (no-op)

### Requirement: Sync does not resurface archived tasks

When the sync service pulls remote updates, it SHALL skip tasks that are archived locally. An archived task SHALL NOT be un-archived by a sync pull.

#### Scenario: Sync pull encounters archived task
- **WHEN** a sync pull returns an update for a task that is archived locally
- **THEN** the sync service SHALL skip that task and not update or un-archive it
