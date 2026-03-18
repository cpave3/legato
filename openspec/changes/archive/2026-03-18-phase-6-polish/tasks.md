## 1. Overlay Infrastructure

- [x] 1.1 Add overlay state management to `internal/tui/app.go` — track active overlay type (none, search, move, help), route key events to overlay when active, render dimmed board behind overlay panel
- [x] 1.2 Create `internal/tui/overlay/overlay.go` — shared overlay rendering utilities (centered panel, dimmed background, border styling) used by all three overlays

## 2. Search Overlay

- [x] 2.1 Implement `internal/tui/overlay/search.go` — Bubbletea model with text input, result list, `j`/`k` navigation, `enter` to select, `esc` to dismiss
- [x] 2.2 Wire search overlay to `BoardService.SearchCards` — call service on each keystroke (debounced), render returned cards as selectable list items showing key + summary + column
- [x] 2.3 Implement board cursor navigation on search result selection — on `enter`, close overlay and move board cursor to the selected ticket's column and row position
- [x] 2.4 Add search overlay tests — verify filtering logic, result selection, dismissal behavior, and empty/no-match states

## 3. Move Overlay

- [x] 3.1 Implement `internal/tui/overlay/move.go` — Bubbletea model showing ticket key + summary, column list with single-letter shortcuts, current column highlighting with "current" label
- [x] 3.2 Wire move overlay to `BoardService.MoveCard` — on shortcut key press, call MoveCard (async Jira transition), close overlay, update board position
- [x] 3.3 Handle move from both board and detail views — ensure `m` keypress triggers move overlay with correct ticket context from either view
- [x] 3.4 Add move overlay tests — verify shortcut key mapping, current column display, dismiss behavior, and move-to-current-column no-op

## 4. Help Overlay

- [x] 4.1 Implement `internal/tui/overlay/help.go` — Bubbletea model rendering keybinding reference organized into Navigation, Actions, and General sections with key-description pairs
- [x] 4.2 Wire help overlay activation from all views — `?` opens help from board, detail, or replaces any active overlay; `esc` or `?` dismisses
- [x] 4.3 Add help overlay tests — verify content includes all documented keybindings, dismissal by `esc` and `?`, and non-dismissal keys are ignored

## 5. Error Handling

- [x] 5.1 Define error event types in `internal/engine/events/` — add `EventSyncError`, `EventTransitionFailed`, `EventAuthFailed`, `EventRateLimited` event types with structured payloads (error type, message, affected ticket key)
- [x] 5.2 Publish error events from sync and Jira client — update `internal/service/sync.go` and `internal/engine/jira/client.go` to publish error events on the EventBus for all failure modes (offline, auth, transition, rate limit)
- [x] 5.3 Add exponential backoff for rate-limited requests — implement backoff logic in the Jira client for 429 responses, resetting after the backoff period expires
- [x] 5.4 Update status bar to subscribe to error events — modify `internal/tui/statusbar/model.go` to subscribe to error events from the EventBus and display transient error messages ("offline", "auth error", "rate limited", transition failure details)
- [x] 5.5 Add card warning indicators for failed transitions — modify `internal/tui/board/card.go` to render a `!` prefix on cards with pending sync failures, sourced from `sync_log` entries with `push_fail` action
- [x] 5.6 Clear warning indicators after successful retry — on successful transition retry (detected via `EventCardMoved` or sync success), remove the warning indicator from the affected card
- [x] 5.7 Add error handling tests — verify error events are published for each failure mode, status bar displays correct messages, card warning indicators appear and clear appropriately

## 6. Server Stub

- [x] 6.1 Create `internal/server/server.go` — HTTP server struct accepting `BoardService` interface, configurable listen address, `Start` and `Stop` methods
- [x] 6.2 Implement `GET /health` handler in `internal/server/handlers.go` — calls `BoardService.ListColumns` and `BoardService.ListCards` to build JSON response with `status`, `columns` (name + cards), and `synced_at` fields
- [x] 6.3 Define JSON response types in `internal/server/types.go` — `HealthResponse`, `ColumnResponse`, `CardResponse` structs with JSON tags
- [x] 6.4 Add server stub tests — start server with mock `BoardService`, verify `/health` returns correct JSON structure, verify empty board response, verify server starts without TUI dependencies

## 7. Integration Validation

- [x] 7.1 Verify overlay z-ordering and dismissal — manual test that each overlay renders correctly over dimmed board, only one overlay is active at a time, and `esc` always returns to the previous view
- [x] 7.2 Verify end-to-end move flow — manual test that `m` -> shortcut key moves a card, Jira transition fires async, failure shows warning icon and status bar error
- [x] 7.3 Verify search-to-navigation flow — manual test that `/` -> type query -> `enter` navigates the board cursor to the selected ticket
