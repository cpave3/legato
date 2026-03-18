## Why

The TUI is the primary user interface for Legato v0. Users need a keyboard-driven kanban board to view and navigate their Jira tickets in the terminal. This phase builds the Bubbletea application shell, kanban board rendering, and vim-style navigation — consuming the BoardService interface from Phase 2 with fake data, proving the architecture works end-to-end without any Jira dependency.

## What Changes

- Create the root Bubbletea application model with view routing (board view as default)
- Build the kanban board view with multi-column layout rendering via Lipgloss
- Implement column components showing card counts and card lists
- Implement card rendering with issue key, truncated summary, priority, and type indicators
- Add vim-style keyboard navigation: h/l between columns, j/k between cards, g/G for first/last
- Add column jump shortcuts (1-5)
- Build the status bar component subscribing to EventBus for sync state display
- Apply Lipgloss theming (dark terminal aesthetic with purple accent, matching mockups)

## Capabilities

### New Capabilities
- `tui-app-shell`: Root Bubbletea model with view routing, window size management, and keyboard dispatch
- `kanban-board`: Multi-column kanban board rendering with cards, vim navigation, and column jumping
- `status-bar`: Status bar component showing sync state, last sync time, and contextual key hints
- `tui-theme`: Lipgloss theme definition matching the mockup aesthetic (dark background, purple accents, priority colors)

### Modified Capabilities
<!-- None -->

## Impact

- New packages: `internal/tui/`, `internal/tui/board/`, `internal/tui/statusbar/`, `internal/tui/theme/`
- New dependencies: `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`, `github.com/charmbracelet/bubbles`
- Updates `cmd/legato/main.go` to wire services and launch the TUI
- Depends on Phase 2 (service layer) interfaces being defined
