# Design: Detail View & Clipboard

## Context

The detail view and clipboard integration complete Legato's core use case: a user reads their assigned Jira tickets on the kanban board, opens a ticket to see the full description rendered as clean terminal markdown, then copies that context to paste into an AI coding agent like Claude Code. Without this, users must context-switch to a browser to read and copy ticket details, defeating the purpose of a terminal-native workflow tool.

This phase builds two new TUI packages (`internal/tui/detail/` and `internal/tui/clipboard/`) and wires them into the existing app shell's view routing. The detail view consumes `BoardService.GetCard()` for ticket data and `BoardService.ExportCardContext()` for clipboard formatting, keeping all business logic in the service layer.

## Goals

- Glamour-rendered full-screen detail view with metadata header (status, priority, type, epic, labels, URL) and scrollable markdown description.
- Clipboard copy with two formats: description-only (`y`) and full structured context block (`Y`).
- Open ticket URL in the default browser (`o`).
- Navigation: `enter` from board opens detail, `esc` returns to board.
- Move overlay accessible from detail view via `m`.

## Non-Goals

- Editing ticket fields from the detail view.
- Inline comments or comment history display.
- Custom Glamour themes (use the default dark/light auto-detection).
- Attachment viewing or downloading.

## Decisions

### Glamour for Markdown Rendering

Glamour is already in the tech stack (spec section 3.2). It handles syntax highlighting, heading styles, list formatting, and blockquotes out of the box. The rendered output is placed inside a Bubbletea `viewport` model for scrolling.

Glamour's `TermRendererOption` will be configured with `glamour.WithWordWrap(width)` where width is derived from the terminal size minus padding. The renderer is re-created on terminal resize.

### Viewport Model for Scrolling

The description section uses `bubbletea/viewport` for scroll support. The metadata header is rendered above the viewport as a fixed section. The viewport gets the remaining vertical space (terminal height minus header height minus status bar height).

Keybindings for scrolling: `j`/`k` scroll line-by-line, `d`/`u` scroll half-page, `g`/`G` jump to top/bottom. These are handled by the viewport model's default key map.

### exec.Command for Clipboard

Clipboard access uses OS-native tools via `exec.Command`, piping content to stdin:

- **macOS**: `pbcopy` (always available)
- **Linux X11**: `xclip -selection clipboard` (fallback: `xsel --clipboard --input`)
- **Linux Wayland**: `wl-copy`

Detection order at startup:
1. Check `$WAYLAND_DISPLAY` -- if set, look for `wl-copy`.
2. Check `$DISPLAY` -- if set, look for `xclip`, then `xsel`.
3. On macOS (`runtime.GOOS == "darwin"`), use `pbcopy`.

The clipboard package exposes a simple `Copy(text string) error` function and a `Available() bool` check. Detection runs once at TUI initialization.

### Browser Open via exec.Command

Opening URLs uses:
- **macOS**: `open <url>`
- **Linux**: `xdg-open <url>`

This is a fire-and-forget operation. Errors are shown in the status bar but do not block the TUI.

### Service Layer Boundary

The detail view calls `BoardService.GetCard(ctx, id)` to fetch a `CardDetail` struct containing all metadata and the markdown description. Clipboard formatting uses `BoardService.ExportCardContext(ctx, id, format)` which returns a pre-formatted markdown string. The TUI never constructs clipboard content itself -- it passes the service output directly to the clipboard package.

## Risks / Trade-offs

### Clipboard Tool Availability

Not all Linux systems have `xclip`, `xsel`, or `wl-copy` installed. Mitigation: detect at startup, show a non-blocking warning in the status bar ("clipboard unavailable -- install xclip"). The `y`/`Y` keybindings show an inline error message rather than silently failing.

### Glamour Rendering Edge Cases

Glamour may not perfectly render all markdown produced by the ADF converter (e.g., deeply nested lists, complex tables, or unusual Unicode). Mitigation: the ADF converter (phase 4) produces standard CommonMark, and Glamour handles CommonMark well. Edge cases will be handled incrementally.

### Terminal Width Handling

Narrow terminals (< 60 columns) may make the metadata header and rendered markdown difficult to read. Mitigation: set a minimum usable width; if the terminal is too narrow, truncate the metadata grid to fewer columns and let Glamour's word-wrap handle the description. The viewport scrolls horizontally if needed.

### Detail View Load Time

`GetCard()` may need to fetch from Jira if the full description is not yet cached in SQLite. Mitigation: show a loading indicator while fetching. Once cached, subsequent opens are instant (sub-100ms target from spec).
