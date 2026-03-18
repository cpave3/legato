## Why

The polish phase turns a functional prototype into a complete v0 product. Fuzzy search, the move overlay, and help screen are table-stakes UX features that users expect from a keyboard-driven TUI. Comprehensive error handling ensures the app degrades gracefully when Jira is unavailable. The server stub validates that the service layer is web-ready for future phases.

## What Changes

- Build fuzzy search/filter overlay (`/`): filters tickets across all columns by key, summary, or description
- Build move overlay (`m`): single-keypress column picker for moving cards, matching the mockup design
- Build help overlay (`?`): keyboard reference screen showing all available keybindings by context
- Add comprehensive error handling for all Jira failure modes, surfaced via event bus and status bar
- Add sync log viewer for diagnostics (viewable sync history from `sync_log` table)
- Stub out `internal/server/` with a health-check endpoint returning board state as JSON, confirming service layer is web-ready

## Capabilities

### New Capabilities
- `search-overlay`: Fuzzy search/filter overlay for finding tickets by key, summary, or description across all columns
- `move-overlay`: Single-keypress column picker overlay for moving cards between kanban columns
- `help-overlay`: Keyboard reference overlay showing all keybindings organized by context
- `error-handling`: Comprehensive error surfacing for Jira failures via event bus, status bar notifications, and card warning indicators
- `server-stub`: Minimal HTTP server with health-check endpoint returning board state as JSON, proving web-readiness

### Modified Capabilities
<!-- None -->

## Impact

- New packages: `internal/tui/overlay/`, `internal/server/`
- Modifies `internal/tui/app.go` to add overlay routing and keyboard dispatch
- Modifies `internal/tui/statusbar/` to display error states
- No new external dependencies
