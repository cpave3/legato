## Why

The Done column accumulates completed cards over time, making the board cluttered and harder to navigate. Users need a way to archive done cards to keep the board focused on active work while retaining task history in the database.

## What Changes

- Add an `archived_at` timestamp column to the `tasks` table (nullable — NULL means not archived)
- Filter archived tasks out of all board queries by default
- Add an archive action accessible from the board (bulk-archive all done cards) and per-card (archive individual done cards)
- Add a keybinding to trigger archiving from the board view

## Capabilities

### New Capabilities
- `card-archiving`: Ability to mark done cards as archived, hiding them from the board while retaining them in the database. Covers the archive flag, bulk/single archive operations, and board filtering.

### Modified Capabilities
- `board-service`: Board queries must exclude archived tasks
- `sqlite-store`: New migration adding `archived_at` column, updated queries to filter archived tasks
- `kanban-board`: New keybinding and visual feedback for archive action

## Impact

- **Database**: New migration adding `archived_at` column to `tasks` table
- **Store layer**: All task listing queries need `WHERE archived_at IS NULL` filter
- **Service layer**: New `ArchiveDoneCards()` and `ArchiveTask(id)` methods on BoardService
- **TUI**: New keybinding (e.g., `X` from board) to archive done cards, confirmation overlay
- **CLI**: No immediate CLI impact (could add `legato task archive` later)
