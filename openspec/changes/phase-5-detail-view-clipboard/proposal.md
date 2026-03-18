## Why

The detail view and clipboard integration complete Legato's core use case: users need to read full ticket descriptions rendered as clean markdown in the terminal, then copy that context to paste into AI coding agents like Claude Code. Without this, users would still need to context-switch to a browser to read and copy ticket details.

## What Changes

- Build the ticket detail view as a full-screen Bubbletea model rendered with Glamour
- Display ticket metadata (status, priority, type, epic, labels, URL) in a structured header
- Render the markdown description with syntax highlighting via Glamour
- Implement clipboard copy: `y` for description-only, `Y` for full structured context block
- Implement OS-native clipboard detection (pbcopy on macOS, xclip/xsel on Linux X11, wl-copy on Wayland)
- Add `o` keybinding to open Jira ticket URL in default browser
- Wire the detail view into the app shell's view routing (enter from board → detail, esc → back)

## Capabilities

### New Capabilities
- `detail-view`: Full-screen ticket detail view with Glamour-rendered markdown, metadata header, and scroll support
- `clipboard`: OS-native clipboard integration with platform detection and copy operations

### Modified Capabilities
<!-- None -->

## Impact

- New packages: `internal/tui/detail/`, `internal/tui/clipboard/`
- New dependency: `github.com/charmbracelet/glamour`
- Modifies `internal/tui/app.go` to add detail view routing
- Calls `BoardService.GetCard()` and `BoardService.ExportCardContext()` from the service layer
