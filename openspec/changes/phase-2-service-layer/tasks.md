## 1. Interface Definitions

- [ ] 1.1 Define `ExportFormat` type and constants (`ExportFormatDescription`, `ExportFormatFull`) in `internal/service/interfaces.go`
  - Validation: type compiles, constants are distinct values
- [ ] 1.2 Define service data types (`Column`, `Card`, `CardDetail`, `SyncResult`, `SyncStatus`, `SyncEvent`) in `internal/service/interfaces.go`
  - Validation: types compile and include all fields needed by interface methods
- [ ] 1.3 Define `BoardService` interface with `ListColumns`, `ListCards`, `GetCard`, `MoveCard`, `ReorderCard`, `SearchCards`, `ExportCardContext` in `internal/service/interfaces.go`
  - Validation: interface compiles, all methods accept `context.Context` as first parameter, no presentation-layer imports
- [ ] 1.4 Define `SyncService` interface with `Sync`, `Status`, `Subscribe` in `internal/service/interfaces.go`
  - Validation: interface compiles, no Jira or presentation imports
- [ ] 1.5 Define `EventBus` interface with `Publish`, `Subscribe`, `Unsubscribe` and event types (`EventCardMoved`, `EventCardUpdated`, `EventCardsRefreshed`, `EventSyncStarted`, `EventSyncCompleted`, `EventSyncFailed`) in `internal/service/interfaces.go`
  - Validation: interface compiles, event type constants are defined

## 2. BoardService Implementation

- [ ] 2.1 Create `internal/service/board.go` with `boardService` struct holding store and event bus dependencies
  - Validation: struct compiles, constructor function accepts store and event bus
- [ ] 2.2 Implement `ListColumns` — query `column_mappings` table ordered by `sort_order`
  - Validation: unit test returns columns in correct order; returns empty slice when no mappings exist
- [ ] 2.3 Implement `ListCards` — query `tickets` table filtered by status (column), ordered by `sort_order`
  - Validation: unit test returns cards in order; returns empty slice for empty column; returns error for invalid column
- [ ] 2.4 Implement `GetCard` — query single ticket by ID, return full `CardDetail`
  - Validation: unit test returns all fields for existing card; returns error for non-existent card
- [ ] 2.5 Implement `MoveCard` — update ticket status and sort_order, publish `EventCardMoved`
  - Validation: unit test confirms status updated, sort_order set to end of target column, event published; no-op when moving to same column; error on invalid column
- [ ] 2.6 Implement `ReorderCard` — update sort_order and adjust other cards, publish `EventCardUpdated`
  - Validation: unit test confirms card at new position, other cards adjusted; out-of-range position places card at end
- [ ] 2.7 Implement `SearchCards` — case-insensitive search across id and summary fields
  - Validation: unit test matches by key, by summary substring; empty query returns all; no match returns empty slice

## 3. Context Export Implementation

- [ ] 3.1 Create `internal/service/context.go` with context formatting functions
  - Validation: file compiles with no presentation-layer imports
- [ ] 3.2 Implement description-only format — heading with key:summary, then description body
  - Validation: unit test output starts with `## KEY: Summary`, includes description; handles empty description
- [ ] 3.3 Implement full structured block format — metadata block, separator, description
  - Validation: unit test output matches spec.md section 8.1 format; omits missing optional fields; handles empty description
- [ ] 3.4 Implement `ExportCardContext` on `boardService` — delegates to format functions, validates format
  - Validation: unit test returns correct format for each `ExportFormat`; returns error for unknown format; returns error for non-existent card
- [ ] 3.5 Verify export output contains no ANSI escape sequences or non-printable characters
  - Validation: unit test scans output bytes and asserts no escape sequences present

## 4. Stub SyncService Implementation

- [ ] 4.1 Create `internal/service/sync_stub.go` with `stubSyncService` struct holding store, event bus, and sync state
  - Validation: struct compiles, constructor function accepts store and event bus
- [ ] 4.2 Define fake ticket data — at least 8 tickets across 3+ columns with varied metadata, including edge cases (empty description, long summary)
  - Validation: fake data slice has 8+ entries, at least 3 distinct statuses, at least 1 empty description, at least 1 summary > 60 chars
- [ ] 4.3 Implement `Sync` — seed fake data on first call, no-op on subsequent calls, publish events
  - Validation: unit test confirms tickets inserted on first call; second call returns zero new; events published in correct order
- [ ] 4.4 Implement `Status` — return current sync state and last sync timestamp
  - Validation: unit test confirms zero-valued time before sync; correct time after sync; not-in-progress state after sync completes
- [ ] 4.5 Implement `Subscribe` — return channel that receives `SyncEvent` values during sync
  - Validation: unit test confirms subscriber channel receives events; multiple subscribers each receive events independently

## 5. Integration Validation

- [ ] 5.1 Write integration test: create store with seed data, instantiate `boardService`, call all methods end-to-end
  - Validation: test passes, covering list/get/move/reorder/search/export flow
- [ ] 5.2 Write integration test: instantiate `stubSyncService`, call `Sync`, then use `boardService` to query seeded data
  - Validation: test passes, confirming synced data is queryable through BoardService
- [ ] 5.3 Write a CLI validation harness in `cmd/validate/phase2/main.go` that wires services together, calls key methods, and prints results to stdout
  - Validation: harness compiles and runs, producing readable output confirming service layer works without any TUI code
