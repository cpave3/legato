## 1. Store: Task Schema & Migration

- [ ] 1.1 Create `store.Task` struct replacing `store.Ticket` — fields: ID, Title, Description, DescriptionMD, Status, Priority, SortOrder, Provider, RemoteID, RemoteMeta, CreatedAt, UpdatedAt — validate: compiles
- [ ] 1.2 Create migration `005_tasks.sql` — CREATE tasks table, INSERT INTO tasks SELECT from tickets (map summary→title, pack remote fields into remote_meta JSON, set provider='jira'), update agent_sessions/sync_log column references, DROP tickets — validate: migration applies on fresh DB and on DB with existing ticket data
- [ ] 1.3 Register migration in store.go migrate() list — validate: New() applies all 5 migrations on fresh DB
- [ ] 1.4 Rename store CRUD methods: CreateTicket→CreateTask, GetTicket→GetTask, ListTicketsByStatus→ListTasksByStatus, UpdateTicket→UpdateTask, UpsertTicket→UpsertTask, DeleteTicket→DeleteTask, ListAllTickets→ListAllTasks, ListTicketIDs→ListTaskIDs — validate: all existing store tests updated and passing with new method names
- [ ] 1.5 Add task ID generation: `GenerateTaskID() string` using crypto/rand, 8-char lowercase alphanumeric — validate: test generates valid IDs, no collisions in 1000 iterations

## 2. Service: BoardService Task Adaptation

- [ ] 2.1 Update `service.Card` type: rename Summary→Title, keep all other fields — validate: compiles
- [ ] 2.2 Update `service.CardDetail` type: rename Summary→Title, drop Jira-specific fields (Assignee, Labels, EpicKey, EpicName, URL), add Provider, RemoteID, RemoteMeta (map[string]string) — validate: compiles
- [ ] 2.3 Update `boardService` methods to use store.Task instead of store.Ticket — ListCards, GetCard, MoveCard, ReorderCard, SearchCards, ExportCardContext — validate: all board_test.go tests passing with renamed fields
- [ ] 2.4 Add `CreateTask(ctx, title, column, priority string) (*Card, error)` to BoardService interface and implementation — generates ID, inserts task, publishes event — validate: test creates task and returns card
- [ ] 2.5 Update `CardDetail` construction: parse `remote_meta` JSON into RemoteMeta map for synced tasks, leave empty for local tasks — validate: test with both local and synced task data

## 3. Sync Service: Jira → Task Conversion

- [ ] 3.1 Update sync service to convert provider output into `store.Task` objects — map Jira fields to task core fields, pack remaining into remote_meta JSON — validate: test with mock provider, verify task has correct provider/remote_id/remote_meta
- [ ] 3.2 Update conflict resolution to read stale_at/local_move_at from remote_meta instead of direct columns — validate: existing sync tests pass with adapted data
- [ ] 3.3 Update push sync to read remote_transition from remote_meta for synced tasks, skip push for local tasks (provider is NULL) — validate: test local task move does not trigger push

## 4. TUI: Rename Summary → Title

- [ ] 4.1 Update board CardData.Summary → CardData.Title, update card rendering and all board tests — validate: board tests pass
- [ ] 4.2 Update detail view to use CardDetail.Title instead of Summary — validate: detail tests pass
- [ ] 4.3 Update overlay search to search by Title instead of Summary — validate: search tests pass
- [ ] 4.4 Update context export to use Title — validate: export tests pass
- [ ] 4.5 Update FakeBoardService in fakeservice.go to use Title field — validate: app tests pass

## 5. TUI: Create Task Overlay

- [ ] 5.1 Create `overlay.CreateOverlay` model — text input for title, column selector (defaults to current), priority selector (cycles none/low/medium/high) — validate: compiles, Init/Update/View work
- [ ] 5.2 Implement title input: typing appends, backspace deletes, enter submits — validate: test Update with key messages
- [ ] 5.3 Implement column selector: tab cycles columns, display shows current selection — validate: test tab cycling
- [ ] 5.4 Implement priority selector: p cycles priorities — validate: test p cycling
- [ ] 5.5 Implement submit: enter with non-empty title emits `CreateTaskMsg{Title, Column, Priority}` — validate: test enter emits message
- [ ] 5.6 Implement cancel: esc emits `CreateCancelledMsg` — validate: test esc emits message
- [ ] 5.7 Render overlay panel using shared `RenderPanel` — validate: View output contains title input and selectors

## 6. TUI: App Integration

- [ ] 6.1 Add `overlayCreate` to overlayKind enum — validate: compiles
- [ ] 6.2 Wire `n` key in board view to open create overlay with current column — validate: test n key sets overlayType
- [ ] 6.3 Handle `CreateTaskMsg`: call BoardService.CreateTask, close overlay, refresh board, navigate to new task — validate: test message creates task
- [ ] 6.4 Handle `CreateCancelledMsg`: close overlay — validate: test message closes overlay

## 7. Update References

- [ ] 7.1 Update agent_sessions references: rename any ticket_id references in code to task_id where applicable — validate: agent service tests pass
- [ ] 7.2 Update sync_log references: rename ticket_id → task_id in store methods and queries — validate: sync log tests pass
- [ ] 7.3 Update server stub if it references ticket types — validate: server tests pass
- [ ] 7.4 Update CLAUDE.md documentation — validate: docs reflect new task-centric model

## 8. Wiring & Smoke Test

- [ ] 8.1 Update cmd/legato/main.go if needed — validate: app compiles and starts
- [ ] 8.2 Manual smoke test: create local task from board, move it between columns, verify it persists across restart. If Jira configured, verify synced tasks still appear and move correctly.
