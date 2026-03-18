## 1. Theme Package

- [ ] 1.1 Create `internal/tui/theme/theme.go` with color palette constants (background, text primary/secondary/tertiary, accent purple, priority colors, sync state colors, column border colors)
- [ ] 1.2 Define Lipgloss style exports: base card style, selected card style, priority badge styles, column header styles (default and active), status bar container and key hint styles, done-column muted styles
- [ ] 1.3 Write unit tests verifying all expected style constants and colors are exported and non-zero

## 2. Status Bar Component

- [ ] 2.1 Create `internal/tui/statusbar/model.go` with Bubbletea model struct holding sync state enum, last sync time, and terminal width
- [ ] 2.2 Implement `Update` method handling sync event messages (started, completed, failed) and window resize messages
- [ ] 2.3 Implement `View` method rendering sync indicator (colored dot + state text), relative time since last sync, and contextual key hints using theme styles
- [ ] 2.4 Implement key hint truncation for narrow terminals, preserving sync state display
- [ ] 2.5 Write unit tests for: state transitions on each sync event type, relative time formatting (seconds, minutes, hours), view output contains expected indicators

## 3. Card Rendering

- [ ] 3.1 Create `internal/tui/board/card.go` with a function to render a single card given card data, column width, selection state, and column name
- [ ] 3.2 Implement issue key display, summary truncation with ellipsis, priority left-border coloring, and type indicator
- [ ] 3.3 Implement selected card highlighting with accent border and highlighted background
- [ ] 3.4 Implement done-column muted styling (reduced opacity, strikethrough summary)
- [ ] 3.5 Write unit tests for: summary truncation at various widths, priority border color selection, selected vs unselected rendering differences

## 4. Column Component

- [ ] 4.1 Create `internal/tui/board/column.go` with a function to render a column given column name, cards, column height, active state, and selected card index
- [ ] 4.2 Implement column header with name (uppercase) and card count, using active vs default header style
- [ ] 4.3 Implement vertical card list rendering with appropriate spacing
- [ ] 4.4 Handle empty columns: render header with count 0 and empty space below
- [ ] 4.5 Write unit tests for: header formatting with card count, active vs inactive header styling, empty column rendering

## 5. Board Model

- [ ] 5.1 Create `internal/tui/board/model.go` with Bubbletea model struct holding columns, cards per column, cursor state (column index, card index), and terminal dimensions
- [ ] 5.2 Implement `Init` method that loads columns and cards from BoardService interface
- [ ] 5.3 Implement `Update` for h/l navigation between columns with card index clamping
- [ ] 5.4 Implement `Update` for j/k navigation between cards with boundary stopping
- [ ] 5.5 Implement `Update` for g/G jump to first/last card in current column
- [ ] 5.6 Implement `Update` for 1-5 column jump shortcuts with bounds checking
- [ ] 5.7 Implement `Update` for window resize: store dimensions, recalculate column widths
- [ ] 5.8 Implement `View` method: calculate column widths, render columns side by side via Lipgloss horizontal join, handle overflow when terminal is too narrow
- [ ] 5.9 Write unit tests for: all navigation scenarios (h/l/j/k/g/G/1-5), boundary conditions, cursor clamping on column change, empty column navigation, initial cursor position

## 6. Root Application Model

- [ ] 6.1 Create `internal/tui/app.go` with root Bubbletea model owning board sub-model, status bar sub-model, active view enum, and terminal dimensions
- [ ] 6.2 Implement `Init` method returning initial commands from sub-models and EventBus bridge command
- [ ] 6.3 Implement EventBus bridge: a `tea.Cmd` that reads from the EventBus subscription channel and returns a custom `tea.Msg`
- [ ] 6.4 Implement `Update` for window resize propagation to all sub-models
- [ ] 6.5 Implement `Update` for global keybindings (q to quit) and delegation of other keys to active view
- [ ] 6.6 Implement `View` composing active view and status bar via `lipgloss.JoinVertical`
- [ ] 6.7 Implement EventBus subscription cleanup on quit
- [ ] 6.8 Write unit tests for: quit on q, window resize propagation, view composition output structure

## 7. Integration Wiring

- [ ] 7.1 Create a stub/fake BoardService implementation in `internal/tui/` (or test package) that returns hardcoded columns and cards for development and testing
- [ ] 7.2 Wire the TUI launch in `cmd/legato/main.go`: instantiate fake BoardService, create root model, call `tea.NewProgram(model).Run()`
- [ ] 7.3 Add Bubbletea, Lipgloss, and Bubbles dependencies to `go.mod`
- [ ] 7.4 Manual validation: run the application, verify board renders with fake data, all navigation keys work, status bar displays, and quit works
