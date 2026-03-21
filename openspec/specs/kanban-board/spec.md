## Requirements

### Requirement: Multi-Column Layout

The kanban board SHALL render columns side by side, each with a header showing the column name and card count.

#### Scenario: Rendering columns from BoardService

- **WHEN** the board model is initialized with data from BoardService
- **THEN** it SHALL call `ListColumns` to get column definitions, call `ListCards` for each column, and render them as adjacent vertical panels using Lipgloss horizontal joining

#### Scenario: Column width calculation

- **WHEN** the terminal width is known
- **THEN** each column SHALL be rendered at equal width calculated as `terminalWidth / numberOfColumns`, with a minimum width of 20 characters

#### Scenario: More columns than fit the terminal

- **WHEN** the terminal is too narrow to render all columns at the minimum width
- **THEN** the board SHALL render only the columns that fit, centered around the currently selected column, and the user SHALL be able to scroll to hidden columns with h/l navigation

### Requirement: Card Rendering

Each card SHALL display the task ID, a truncated title, agent status with duration (when applicable), visual indicators for priority, a workspace tag when in "All" view, and — when a PR is linked — PR state indicators showing CI check status, review decision, and comment presence. Cards with agent data SHALL be taller than cards without.

#### Scenario: Card content display — no agent

- **WHEN** a card is rendered that has no active agent and no duration history
- **THEN** it SHALL show the provider icon and task ID on the first line, the title truncated to fit on the second line, and priority/issue type metadata on the third line

#### Scenario: Card content display — with agent

- **WHEN** a card is rendered that has an active agent or has duration history
- **THEN** it SHALL show the provider icon and task ID on the first line, the title on the second line, the agent status with duration on the third line, and priority/issue type metadata on the fourth line

#### Scenario: Workspace tag in "All" view

- **WHEN** a card is rendered while the "All" workspace view is active and the card has a workspace assigned
- **THEN** the card SHALL display a workspace tag (workspace name in workspace color) on the metadata line

#### Scenario: Workspace tag omitted in workspace view

- **WHEN** a card is rendered while a specific workspace view is active
- **THEN** the card SHALL NOT display a workspace tag (the workspace is implicit from the view)

#### Scenario: Unassigned card in "All" view

- **WHEN** a card is rendered in "All" view with no workspace assigned
- **THEN** the card SHALL NOT display a workspace tag (no tag is better than "Unassigned" clutter)

#### Scenario: Agent status line rendering

- **WHEN** the agent status line is rendered for a card with an active agent
- **THEN** it SHALL display the agent state icon, the state label (RUNNING/WAITING/IDLE), and the cumulative duration for the current state formatted as a human-readable string (e.g., "2h 15m")

#### Scenario: Agent duration display for inactive agent with history

- **WHEN** a card has no active agent but has accumulated duration history
- **THEN** the agent line SHALL display the total working and waiting durations (e.g., "1h 30m working · 20m waiting")

#### Scenario: Priority indicator

- **WHEN** a card has a priority value
- **THEN** the card SHALL display a colored left border matching the priority: red/orange for high, yellow for medium, green for low, and grey for unset

#### Scenario: Title truncation

- **WHEN** a card title exceeds the available column width minus padding
- **THEN** the title SHALL be truncated with an ellipsis to fit within the available space

#### Scenario: Warning indicator placement

- **WHEN** a card has a warning flag set
- **THEN** the warning icon SHALL be displayed on the task ID line after the provider icon, before the key

#### Scenario: Card with PR passing CI and approved

- **WHEN** a card has `pr_meta` with `check_status="pass"` and `review_decision="APPROVED"`
- **THEN** the card SHALL display a green checkmark icon for CI and an approval indicator

#### Scenario: Card with PR failing CI

- **WHEN** a card has `pr_meta` with `check_status="fail"`
- **THEN** the card SHALL display a red X icon for CI status

#### Scenario: Card with PR pending CI

- **WHEN** a card has `pr_meta` with `check_status="pending"`
- **THEN** the card SHALL display a yellow/orange pending icon for CI status

#### Scenario: Card with changes requested

- **WHEN** a card has `pr_meta` with `review_decision="CHANGES_REQUESTED"`
- **THEN** the card SHALL display a warning-colored indicator signaling rework needed

#### Scenario: Card with PR but no checks

- **WHEN** a card has `pr_meta` with `check_status=""`
- **THEN** the card SHALL NOT display any CI icon

#### Scenario: Card with draft PR

- **WHEN** a card has `pr_meta` with `is_draft=true`
- **THEN** the card SHALL display a dimmed/draft indicator instead of review status

#### Scenario: Card with no PR linked

- **WHEN** a card has no `pr_meta`
- **THEN** the card SHALL render identically to current behavior (no PR indicators)

#### Scenario: Card with comments on PR

- **WHEN** a card has `pr_meta` with `comment_count > 0`
- **THEN** the card SHALL display a comment indicator with the count

### Requirement: Vim Navigation -- Column Movement

The user SHALL be able to move the cursor between columns using h and l keys.

#### Scenario: Move cursor right

- **WHEN** the user presses `l` and the cursor is not on the rightmost column
- **THEN** the cursor SHALL move to the next column to the right, preserving the card index or clamping to the last card if the new column has fewer cards

#### Scenario: Move cursor left

- **WHEN** the user presses `h` and the cursor is not on the leftmost column
- **THEN** the cursor SHALL move to the next column to the left, preserving the card index or clamping to the last card if the new column has fewer cards

#### Scenario: Cursor at boundary

- **WHEN** the user presses `l` on the rightmost column or `h` on the leftmost column
- **THEN** the cursor SHALL remain in the current position (no wrapping)

### Requirement: Vim Navigation -- Card Movement

The user SHALL be able to move the cursor between cards within a column using j and k keys.

#### Scenario: Move cursor down

- **WHEN** the user presses `j` and the cursor is not on the last card in the column
- **THEN** the cursor SHALL move to the next card below

#### Scenario: Move cursor up

- **WHEN** the user presses `k` and the cursor is not on the first card in the column
- **THEN** the cursor SHALL move to the previous card above

#### Scenario: Cursor at boundary within column

- **WHEN** the user presses `j` on the last card or `k` on the first card
- **THEN** the cursor SHALL remain in the current position (no wrapping)

### Requirement: Vim Navigation -- Jump to First/Last

The user SHALL be able to jump to the first or last card in the current column using g and G keys.

#### Scenario: Jump to first card

- **WHEN** the user presses `g`
- **THEN** the cursor SHALL move to the first card in the current column

#### Scenario: Jump to last card

- **WHEN** the user presses `G`
- **THEN** the cursor SHALL move to the last card in the current column

### Requirement: Column Jump Shortcuts

The user SHALL be able to jump directly to a column by pressing its number key (1-5).

#### Scenario: Jump to column by number

- **WHEN** the user presses a number key between 1 and 5
- **THEN** the cursor SHALL move to that column (1-indexed), preserving the card index or clamping to the last card if the target column has fewer cards

#### Scenario: Number exceeds column count

- **WHEN** the user presses a number key greater than the number of columns
- **THEN** the cursor SHALL remain in the current position

### Requirement: Cursor Tracking

The board model SHALL maintain cursor state as a column index and a card index within that column.

#### Scenario: Initial cursor position

- **WHEN** the board view is first displayed
- **THEN** the cursor SHALL be positioned at column 0, card 0

#### Scenario: Cursor persists across renders

- **WHEN** the board re-renders (e.g., after a window resize)
- **THEN** the cursor position SHALL be preserved, clamped to valid bounds if data has changed

### Requirement: Card Selection Highlighting

The currently selected card SHALL be visually distinguished from other cards.

#### Scenario: Selected card appearance

- **WHEN** a card is at the cursor position
- **THEN** it SHALL be rendered with a highlighted border (purple accent color), a selection indicator marker, and a contrasting background color to distinguish it from non-selected cards

#### Scenario: Active column header

- **WHEN** a column contains the cursor
- **THEN** the column header SHALL be rendered in the accent color to indicate it is the active column

### Requirement: Empty Column Handling

The board SHALL handle columns with no cards gracefully.

#### Scenario: Rendering an empty column

- **WHEN** a column has zero cards
- **THEN** the column SHALL still render its header with a count of 0 and display empty space below

#### Scenario: Navigating into an empty column

- **WHEN** the user navigates into a column with no cards
- **THEN** the cursor SHALL be at card index 0 and no card SHALL be highlighted, and j/k/g/G keypresses SHALL have no effect

### Requirement: Create task keybinding

The user SHALL be able to create a new task from the board view.

#### Scenario: Press n to create task

- **WHEN** the user presses `n` on the board view
- **THEN** the create-task overlay SHALL open with the current column pre-selected

#### Scenario: New task appears on board

- **WHEN** a task is successfully created via the overlay
- **THEN** the board SHALL refresh and the cursor SHALL navigate to the newly created task

### Requirement: Spawn agent from board

The user SHALL be able to spawn an agent on the currently selected card directly from the board view.

#### Scenario: Spawning via keybinding

- **WHEN** the user presses `a` while a card is selected on the board
- **THEN** the board SHALL emit a message requesting an agent spawn for the selected card's task ID, and the root app SHALL handle spawning the agent session

#### Scenario: Spawn on card with existing agent

- **WHEN** the user presses `a` on a card that already has a running agent
- **THEN** the system SHALL switch to the agent view with that agent selected instead of spawning a duplicate

#### Scenario: Agent indicator on board cards

- **WHEN** a card has an active agent session
- **THEN** the card SHALL display a small indicator (e.g., `>` prefix) to show an agent is running on it

### Requirement: Duration data on CardData

The `CardData` struct SHALL include fields for aggregated state durations so the board can render them without additional queries.

#### Scenario: CardData population during data load

- **WHEN** the board loads data via `DataLoadedMsg`
- **THEN** the app SHALL query `GetStateDurationsBatch` for all visible task IDs and populate `CardData.WorkingDuration` and `CardData.WaitingDuration` fields

#### Scenario: CardData with no duration data

- **WHEN** a task has no state intervals
- **THEN** `CardData.WorkingDuration` and `CardData.WaitingDuration` SHALL be zero-value `time.Duration`

### Requirement: Human-readable duration formatting

The board SHALL format durations as concise human-readable strings.

#### Scenario: Duration under one hour

- **WHEN** a duration is less than 60 minutes
- **THEN** it SHALL be formatted as `Xm` (e.g., "45m")

#### Scenario: Duration over one hour

- **WHEN** a duration is 60 minutes or more
- **THEN** it SHALL be formatted as `Xh Ym` (e.g., "2h 15m")

#### Scenario: Duration under one minute

- **WHEN** a duration is less than 60 seconds
- **THEN** it SHALL be formatted as `<1m`

#### Scenario: Zero duration

- **WHEN** a duration is zero
- **THEN** it SHALL not be displayed (the label for that state is omitted)

### Requirement: Uniform card height within columns

Cards within a single column SHALL have uniform height to prevent visual jitter.

#### Scenario: Mixed agent and non-agent cards in a column

- **WHEN** a column contains both cards with agent data and cards without
- **THEN** all cards in that column SHALL be rendered at the height of the tallest card, with shorter cards padded with empty lines

### Requirement: Archive keybinding

The board SHALL respond to `X` (shift-x) by initiating the bulk archive flow. If there are done cards, it SHALL open an archive confirmation overlay. If there are no done cards, the keypress SHALL be a no-op.

#### Scenario: X pressed with done cards
- **WHEN** the user presses `X` on the board and `CountDoneCards` returns > 0
- **THEN** an archive confirmation overlay SHALL open showing the count

#### Scenario: X pressed with no done cards
- **WHEN** the user presses `X` on the board and `CountDoneCards` returns 0
- **THEN** nothing SHALL happen

### Requirement: Archive confirmation overlay

The archive overlay SHALL display "Archive N done cards?" with instructions to press `y` to confirm or `n`/`esc` to cancel. On confirmation, it SHALL emit an `ArchiveDoneMsg`. On cancel, it SHALL close without action.

#### Scenario: Confirm archive
- **WHEN** the user presses `y` on the archive overlay
- **THEN** an `ArchiveDoneMsg` SHALL be emitted and the overlay SHALL close

#### Scenario: Cancel archive
- **WHEN** the user presses `n` or `esc` on the archive overlay
- **THEN** the overlay SHALL close and no message SHALL be emitted

### Requirement: Help overlay includes archive keybinding

The help overlay SHALL include the `X` keybinding with description "Archive done cards" in its keybinding reference.

#### Scenario: Archive in help
- **WHEN** the help overlay is displayed
- **THEN** it SHALL list `X` → "Archive done cards" among the board keybindings
