# Phase 6 — Polish: Design

## Context

Phase 6 is the final phase of v0, turning a functional prototype into a complete product. It adds the remaining user-facing features that users expect from a keyboard-driven TUI: fuzzy search across tickets, a move overlay for quick column transitions, and a help overlay showing all keybindings. It also completes the error handling story so the app degrades gracefully when Jira is unavailable, and stubs out the HTTP server layer to validate that the service architecture is web-ready for future phases.

All five capabilities are additive — they build on the existing engine, service, and TUI layers without modifying their core contracts.

## Goals / Non-Goals

### Goals

- **Search overlay**: Fuzzy search/filter activated by `/` that matches across ticket key, summary, and description, with real-time filtering and result selection.
- **Move overlay**: Single-keypress column picker triggered by `m` that shows all columns with letter shortcuts, highlights the current column, fires an async Jira transition, and surfaces errors on failure.
- **Help overlay**: Keyboard reference screen triggered by `?` that lists all keybindings organized by context (Navigation, Actions, General).
- **Error surfacing**: Comprehensive error handling for offline mode, auth failures, transition failures, and rate limiting — all surfaced via the event bus, status bar, and card-level warning indicators.
- **Server stub**: Minimal HTTP server with a single `GET /health` endpoint returning board state as JSON, proving the service layer can be consumed over HTTP.

### Non-Goals

- Full web UI or frontend client.
- Agent spawning, lifecycle management, or orchestration features.
- WebSocket event streaming (future phases).
- Custom keybinding rebinding (config supports it, but not implemented in v0).
- Advanced search operators (project filters, status filters, etc.).

## Decisions

### Overlay rendering pattern

All three overlays (search, move, help) follow the same rendering pattern: a centered panel drawn on top of a dimmed board view. The board remains visible at reduced opacity behind the overlay. The root `app.go` model manages overlay state and routes keyboard input to the active overlay when one is open.

Overlay lifecycle:
1. A trigger key (`/`, `m`, `?`) sets the active overlay in the app model.
2. While an overlay is active, all key events route to the overlay model.
3. The overlay returns a result (selected card, selected column, or nil for dismiss).
4. `esc` always dismisses the overlay without action.

This avoids the complexity of stacking overlays — only one overlay can be active at a time.

### Fuzzy matching approach

Search uses substring matching on the concatenation of ticket key, summary, and description. The `BoardService.SearchCards` method already exists in the service interface. The TUI overlay calls this method on each keystroke (debounced) and renders the filtered results.

No external fuzzy-matching library is needed for v0. Simple case-insensitive substring matching across the three fields is sufficient for the expected ticket volume (tens to low hundreds of tickets).

### Move overlay column shortcuts

Each column gets a single-letter shortcut derived from its name (first letter, lowercased). For the default columns: `b` (Backlog), `r` (Ready), `d` (Doing), `v` (Review), `x` (Done). The `v` and `x` shortcuts avoid conflicts with common vim keys. These shortcuts match the mockup design.

If custom column names create letter conflicts, the overlay falls back to number keys (`1`-`9`).

### Server stub as validation only

The `internal/server/` package contains a minimal HTTP server with one endpoint. It is not wired into the main TUI binary — it exists to prove that `BoardService` can be wrapped in HTTP handlers. A test or standalone `main` can start it. This validates the architecture claim that the service layer is web-ready without adding runtime complexity to v0.

### Error surfacing via event bus

All Jira errors are published as events through the existing `EventBus`. The TUI status bar subscribes to error events and displays them as transient notifications. Cards with failed transitions show a warning indicator (`!` prefix or icon). The error state is stored in the `sync_log` table for diagnostics.

Error categories and their handling:
- **Offline**: Load from SQLite, show "offline" in status bar, retry on interval.
- **Auth failure**: Show persistent error in status bar, suggest config check.
- **Transition failure**: Card stays in local column, warning icon on card, error in status bar, logged to `sync_log`.
- **Rate limiting**: Exponential backoff on 429 responses, "rate limited" in status bar.

## Risks / Trade-offs

### Overlay z-ordering in terminal

Terminal rendering does not have true z-index layering. The overlay effect (dimmed board + centered panel) is achieved by rendering the full board with muted colors, then overwriting the center region with the overlay content. This works well in Bubbletea's immediate-mode rendering but requires careful coordinate math to center the overlay panel. If terminal dimensions are very small, the overlay may need to take the full screen instead.

### Search performance with large ticket sets

Searching on every keystroke against all tickets could be slow if a user has hundreds of tickets. Mitigation: debounce input (100-200ms), and search operates on the in-memory ticket list (already loaded from SQLite), not against Jira. For v0's expected scale (tens of tickets per user), this is not a practical concern.

### Single overlay constraint

Only one overlay can be active at a time. This means you cannot open the help overlay while the move overlay is open. This is a deliberate simplification — stacking overlays adds significant complexity for minimal UX benefit.
