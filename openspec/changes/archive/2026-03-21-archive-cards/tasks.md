## 1. Database Migration

- [x] 1.1 Create `internal/engine/store/migrations/009_archive.sql` adding `archived_at DATETIME` column to `tasks` table
- [x] 1.2 Update migration embed and version constant to include migration 009

## 2. Store Layer

- [x] 2.1 Add `ArchivedAt` field (`*time.Time`) to `store.Task` struct
- [x] 2.2 Add `ArchiveTask(ctx, id)` method — sets `archived_at = datetime('now')` for a task, returns error if not found
- [x] 2.3 Add `ArchiveTasksByStatus(ctx, status)` method — bulk sets `archived_at` for all non-archived tasks with given status, returns count
- [x] 2.4 Add `AND archived_at IS NULL` to `ListTasksByStatus`, `ListTasksByStatusAndWorkspace`, and search queries
- [x] 2.5 Write tests for archive store methods and filtered listing

## 3. Service Layer

- [x] 3.1 Add `ArchiveDoneCards(ctx)` to `BoardService` — resolves Done column status, calls `ArchiveTasksByStatus`, publishes `EventCardsRefreshed`, returns count
- [x] 3.2 Add `ArchiveTask(ctx, id)` to `BoardService` — verifies task is in Done column, calls store `ArchiveTask`, publishes event
- [x] 3.3 Add `CountDoneCards(ctx)` to `BoardService` — returns count of non-archived tasks in Done column
- [x] 3.4 Update sync service pull to skip archived tasks during upsert
- [x] 3.5 Write tests for archive service methods and sync skip behavior

## 4. TUI — Archive Overlay

- [x] 4.1 Create `internal/tui/overlay/archive.go` — confirmation overlay showing "Archive N done cards?", y/n/esc handling, emits `ArchiveDoneMsg`
- [x] 4.2 Add `overlayArchive` to the `overlayType` enum in the app model
- [x] 4.3 Wire `X` keypress in board view to call `CountDoneCards`, open archive overlay if count > 0

## 5. TUI — App Wiring

- [x] 5.1 Handle `ArchiveDoneMsg` in app `Update` — call `BoardService.ArchiveDoneCards`, trigger data reload
- [x] 5.2 Add `X` → "Archive done cards" to the help overlay keybinding list
- [x] 5.3 Update `BoardService` interface in `internal/service/interfaces.go` with new methods
