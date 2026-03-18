## MODIFIED Requirements

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

## ADDED Requirements

### Requirement: Create task keybinding

The user SHALL be able to create a new task from the board view.

#### Scenario: Press n to create task

- **WHEN** the user presses `n` on the board view
- **THEN** the create-task overlay SHALL open with the current column pre-selected

#### Scenario: New task appears on board

- **WHEN** a task is successfully created via the overlay
- **THEN** the board SHALL refresh and the cursor SHALL navigate to the newly created task
