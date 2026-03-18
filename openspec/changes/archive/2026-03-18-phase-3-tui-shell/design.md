# Phase 3 — TUI Shell: Design

## Context

The TUI is the first presentation layer for Legato, consuming BoardService and SyncService via Go interfaces defined in Phase 2. It lives entirely within `internal/tui/` and imports nothing from `internal/service/` except through interfaces. This separation ensures a future web UI can consume the same service layer without any TUI coupling.

The TUI is built on Bubbletea (Elm architecture), styled with Lipgloss, and renders a kanban board as the default and only view in this phase. Detail view, overlays, and clipboard are deferred to Phases 5-6.

## Goals

- Render a multi-column kanban board populated from BoardService, matching the mockup aesthetic.
- Provide vim-style keyboard navigation: h/l between columns, j/k between cards, g/G for first/last card, 1-5 for column jumping.
- Display a status bar showing sync state (via EventBus subscription), last sync time, and contextual key hints.
- Define a Lipgloss theme with dark background, purple accents, and priority-colored card borders.
- Establish the root Bubbletea model with view routing so future views (detail, overlays) can be added without restructuring.

## Non-Goals

- Detail view (Phase 5).
- Overlays: move picker, help screen, fuzzy search (Phase 6).
- Clipboard integration (Phase 5).
- Card movement mutations — this phase is read-only navigation with fake data.
- Jira connectivity — the TUI consumes service interfaces backed by fake/stub data.

## Decisions

### Bubbletea Elm Architecture

All TUI state flows through the standard Bubbletea `Init` / `Update` / `View` cycle. Each visual component (board, status bar) is its own Bubbletea model with its own `Update` and `View` methods. The root model composes these sub-models and delegates messages accordingly.

### Model Composition

The root model (`internal/tui/app.go`) owns:
- An `activeView` enum tracking which view is displayed (board is the only view in this phase, but the routing mechanism is established).
- A `board.Model` sub-model for the kanban board.
- A `statusbar.Model` sub-model for the status bar.
- Window dimensions (`width`, `height`) received from `tea.WindowSizeMsg`.

On each `Update`, the root model dispatches key messages to the active view's sub-model and always updates the status bar. On `View`, it composes the active view above the status bar using `lipgloss.JoinVertical`.

### View Routing

The root model uses a simple enum (`viewBoard`, `viewDetail`, etc.) to determine which sub-model receives key messages and which sub-model's `View()` is rendered. In this phase only `viewBoard` exists, but the pattern is in place for Phase 5 to add `viewDetail` without restructuring.

### Lipgloss for Layout and Styling

Lipgloss handles all visual concerns:
- Column layout via `lipgloss.JoinHorizontal` with fixed-width columns calculated from terminal width.
- Card rendering with borders, padding, and priority-colored left borders.
- The theme package (`internal/tui/theme/`) exports named style constants so all components reference a single source of truth.

### EventBus Integration

The status bar subscribes to the EventBus for sync events. Since Bubbletea owns the event loop, EventBus events are bridged into Bubbletea via a `tea.Cmd` that listens on the EventBus channel and emits custom `tea.Msg` types. This keeps the Bubbletea model pure (no goroutine management inside Update).

## Risks / Trade-offs

### Terminal Size Handling

Columns must reflow when the terminal is too narrow to fit all five columns at a readable width. The design sets a minimum column width (20 characters). If the terminal is narrower than `minColumnWidth * columnCount`, columns beyond the visible area are hidden and the user scrolls with h/l. This adds complexity to cursor tracking but avoids unreadable compressed columns.

### Color Support Across Terminals

Lipgloss auto-detects terminal color profile (TrueColor, 256-color, ANSI). The theme defines colors in hex (TrueColor) and Lipgloss degrades gracefully. However, some terminals (e.g., the Linux console, older tmux configurations) may render poorly. The trade-off is accepted: the theme targets TrueColor terminals (iTerm2, Ghostty, Kitty, Alacritty, modern GNOME Terminal) and degrades rather than maintaining separate color palettes.

### Sub-model Message Routing

Bubbletea does not have built-in message routing to sub-models. The root model manually dispatches messages, which means every new sub-model requires a wiring change in the root. This is acceptable for the small number of components in Legato. A message-bus abstraction would add complexity without clear benefit at this scale.
