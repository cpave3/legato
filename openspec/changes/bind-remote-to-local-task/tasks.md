## 1. Store Layer

- [ ] 1.1 Add `GetTaskByRemoteID(ctx, provider, remoteID)` method to store — query `tasks` where `provider` and `remote_id` match, return task or not-found error. Add tests with real SQLite.
- [ ] 1.2 Add `BindRemoteTicket(ctx, taskID, provider, remoteID, remoteMeta)` method to store — update `provider`, `remote_id`, `remote_meta`, `updated_at` on existing task. Reject if task already has non-NULL provider. Add tests.

## 2. Service Layer

- [ ] 2.1 Add `BindRemoteTicket(ctx, taskID, remoteTicketID)` to `SyncService` — fetch remote ticket via provider, check for duplicate binding (by `id` and `remote_id`), call `store.BindRemoteTicket`, update title/description from remote, set `local_move_at` in remote_meta. Add tests with mock provider.
- [ ] 2.2 Update `SyncService.pullSync` to match incoming tickets by `remote_id` when `id` lookup misses — call `store.GetTaskByRemoteID` as fallback. Add test for pull sync updating a bound task.
- [ ] 2.3 Update `SyncService.ImportRemoteTask` to check `GetTaskByRemoteID` before importing — prevent importing a ticket that's already bound to a local task. Add test.
- [ ] 2.4 Add `BindRemoteTicket` to the `SyncService` interface (or equivalent) so the TUI can call it.

## 3. CLI Layer

- [ ] 3.1 Update CLI task lookup to fall back to `remote_id` when `id` lookup fails — affects `TaskUpdate`, `TaskNote`, `AgentState`, `TaskLink`, `TaskUnlink`. Add test for CLI resolving a bound task by remote ID.

## 4. Board Rendering

- [ ] 4.1 Update card rendering to display `remote_id` (with provider icon) when present, falling back to `id` for local tasks. Verify in `board/card.go` where task key is rendered.

## 5. TUI Detail View & Overlay

- [ ] 5.1 Add `i` keybinding to detail view for local tasks — opens bind overlay (reuses import search overlay pattern). No-op for remote tasks or when sync service is nil.
- [ ] 5.2 Create bind overlay (or adapt import overlay) — same search UI, but on selection calls `SyncService.BindRemoteTicket(currentTaskID, selectedRemoteID)` instead of `ImportRemoteTask`. Show confirmation before binding. Return to detail view on success.
- [ ] 5.3 After bind completes, refresh detail view to show updated remote metadata, provider fields, and disable local-only keybindings (e/t).

## 6. Integration

- [ ] 6.1 Wire `SyncService.BindRemoteTicket` through the TUI app model — handle `BindRemoteTicketMsg` in app `Update`, call service, publish refresh.
- [ ] 6.2 End-to-end manual test: create local task → spawn agent → bind remote ticket → verify agent still running, card shows remote ID, pull sync updates the bound task.
