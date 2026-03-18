## 1. Clipboard Package

- [x] 1.1 Create `internal/tui/clipboard/clipboard.go` with platform detection logic: check `runtime.GOOS`, `$WAYLAND_DISPLAY`, `$DISPLAY`, and probe for `pbcopy`/`wl-copy`/`xclip`/`xsel` via `exec.LookPath`. Expose `New() *Clipboard`, `Available() bool`, and `Copy(text string) error`. Validate: unit test with mock exec lookups covering macOS, Wayland, X11 xclip, X11 xsel fallback, and no-tool-found cases.

- [x] 1.2 Implement `Copy()` method: pipe text to the detected tool's stdin via `exec.Command`, capture stderr on failure, return wrapped errors. Validate: integration test on the current platform confirming text reaches the system clipboard (skip in CI if no tool available).

- [x] 1.3 Implement `OpenURL(url string) error` function in the clipboard package (or a shared `internal/tui/open/` package): use `open` on macOS, `xdg-open` on Linux. Validate: unit test confirming correct command is selected per `runtime.GOOS`.

## 2. Detail View Model

- [x] 2.1 Create `internal/tui/detail/model.go` with a Bubbletea `Model` struct that holds: the `CardDetail` data, a `viewport.Model` for scrolling, a Glamour renderer, clipboard reference, and current terminal dimensions. Implement `Init()`, `Update()`, `View()`. Validate: model compiles and can be instantiated with mock data.

- [x] 2.2 Implement the metadata header rendering: display ticket key and summary as the title, then a grid row with status, priority, type, epic, labels, and the Jira URL. Use Lipgloss for styling. Handle missing optional fields gracefully. Validate: render the header with full and partial metadata, confirm output matches the mockup layout.

- [x] 2.3 Implement Glamour markdown rendering for the description: create a `glamour.TermRenderer` with `WithWordWrap(width)`, render the description markdown, and place the output in the viewport. Validate: render a description with headings, lists, code blocks, and blockquotes; confirm styled output.

- [x] 2.4 Implement terminal resize handling: on `tea.WindowSizeMsg`, update terminal dimensions, recalculate viewport size, and re-create the Glamour renderer with the new width. Validate: simulate resize messages and confirm the viewport and rendered content adjust.

- [x] 2.5 Implement scroll keybindings: `j`/`k` for line scroll, `d`/`u` for half-page, `g`/`G` for top/bottom. Wire these through the viewport's key map. Validate: with a long description, confirm each keybinding scrolls the viewport correctly.

- [x] 2.6 Implement the detail view status bar: display keybinding hints (esc, y, Y, m, o) at the bottom. Support temporary feedback messages (e.g., "Copied!" or error text) that revert after a timeout. Validate: render the status bar, trigger a feedback message, confirm it reverts.

- [x] 2.7 Implement loading state: when `CardDetail` data is not yet available, show a loading indicator. Send a `tea.Cmd` to fetch via `BoardService.GetCard()` and transition to the rendered view on completion. Validate: instantiate with nil data, confirm loading indicator appears, then supply data and confirm the view renders.

## 3. Clipboard Keybindings in Detail View

- [x] 3.1 Wire `y` keybinding: on keypress, call `BoardService.ExportCardContext(ctx, id, FormatDescription)`, pass the result to `clipboard.Copy()`, display confirmation or error in the status bar. Validate: mock the service and clipboard, press `y`, confirm correct format requested and copy invoked.

- [x] 3.2 Wire `Y` keybinding: on keypress, call `BoardService.ExportCardContext(ctx, id, FormatFullContext)`, pass the result to `clipboard.Copy()`, display confirmation or error in the status bar. Validate: mock the service and clipboard, press `Y`, confirm correct format requested and copy invoked.

- [x] 3.3 Wire `o` keybinding: on keypress, call `OpenURL()` with the ticket's Jira URL. Show an error in the status bar if the URL is empty or the command fails. Validate: mock OpenURL, press `o`, confirm the correct URL is passed.

## 4. App Shell Integration

- [x] 4.1 Add detail view routing in `internal/tui/app.go`: on `enter` from the board view, create a detail `Model` with the selected card's ID and switch the active view. On `esc` from detail, return to the board view preserving card selection. Validate: navigate from board to detail and back, confirm view switching works and board selection is preserved.

- [x] 4.2 Wire the move overlay from the detail view: when `m` is pressed in the detail view, open the same move overlay used by the board view, targeting the currently viewed ticket. After move completes, update the detail view's metadata. Validate: press `m` in detail, select a column, confirm the ticket's status updates.

- [x] 4.3 Display clipboard startup warning: during TUI initialization, check `clipboard.Available()`. If false, publish a warning event or set a status bar message. Validate: initialize with no clipboard tool detected, confirm the warning appears in the status bar.
