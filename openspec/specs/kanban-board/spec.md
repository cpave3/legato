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

Each card SHALL display the task ID, a truncated title, and visual indicators for priority and agent status.

#### Scenario: Card content display

- **WHEN** a card is rendered within a column
- **THEN** it SHALL show the task ID on the first line, the title truncated to fit the column width on the second line, and priority indicator on the third line

#### Scenario: Priority indicator

- **WHEN** a card has a priority value
- **THEN** the card SHALL display a colored left border matching the priority: red/orange for high, yellow for medium, green for low, and grey for unset

#### Scenario: Title truncation

- **WHEN** a card title exceeds the available column width minus padding
- **THEN** the title SHALL be truncated with an ellipsis to fit within the available space

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
