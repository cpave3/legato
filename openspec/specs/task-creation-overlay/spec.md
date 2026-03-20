## ADDED Requirements

### Requirement: Create task overlay

The user SHALL be able to create a new task from the board view by pressing `n`, which opens an inline overlay with a form.

#### Scenario: Opening the create overlay

- **WHEN** the user presses `n` on the board view
- **THEN** a create-task overlay SHALL appear with a title input field, a column selector (defaulting to the current column), and a priority selector

#### Scenario: Submitting a new task

- **WHEN** the user fills in the title and presses enter to submit
- **THEN** the system SHALL generate an ID, create the task in the database with the selected column as status and priority, refresh the board, and navigate the cursor to the new task

#### Scenario: Empty title rejected

- **WHEN** the user submits the form with an empty title
- **THEN** the system SHALL not create a task and SHALL keep the overlay open with the title field focused

#### Scenario: Cancelling creation

- **WHEN** the user presses `esc` in the create overlay
- **THEN** the overlay SHALL close without creating a task

### Requirement: Column selection in create overlay

The create overlay SHALL allow the user to select which column the new task is placed in.

#### Scenario: Default column

- **WHEN** the create overlay opens
- **THEN** the column selector SHALL default to the column where the board cursor is currently positioned

#### Scenario: Cycling columns

- **WHEN** the user presses `tab` in the create overlay
- **THEN** the column selector SHALL cycle to the next column

### Requirement: Priority selection in create overlay

The create overlay SHALL allow the user to set a priority for the new task.

#### Scenario: Default priority

- **WHEN** the create overlay opens
- **THEN** the priority SHALL default to none/unset

#### Scenario: Cycling priority

- **WHEN** the user presses `p` in the create overlay (while not typing in the title field)
- **THEN** the priority SHALL cycle through: none → low → medium → high → none
