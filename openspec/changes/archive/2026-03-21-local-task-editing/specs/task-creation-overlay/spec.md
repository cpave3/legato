## MODIFIED Requirements

### Requirement: Create task overlay

The user SHALL be able to create a new task from the board view by pressing `n`, which opens an inline overlay with a form.

#### Scenario: Opening the create overlay

- **WHEN** the user presses `n` on the board view
- **THEN** a create-task overlay SHALL appear with a title input field, a description input field, a column selector (defaulting to the current column), and a priority selector

#### Scenario: Submitting a new task with title and description

- **WHEN** the user fills in the title (required) and optionally a description, then presses enter to submit
- **THEN** the system SHALL generate an ID, create the task in the database with the title, description (stored in both `description` and `description_md`), selected column as status, and priority, refresh the board, and navigate the cursor to the new task

#### Scenario: Empty title rejected

- **WHEN** the user submits the form with an empty title
- **THEN** the system SHALL not create a task and SHALL keep the overlay open with the title field focused

#### Scenario: Cancelling creation

- **WHEN** the user presses `esc` in the create overlay
- **THEN** the overlay SHALL close without creating a task

## ADDED Requirements

### Requirement: Description field in create overlay

The create overlay SHALL include a description input field that accepts multi-line text.

#### Scenario: Tab cycling includes description

- **WHEN** the user presses `tab` in the create overlay
- **THEN** the focus SHALL cycle through: title → column selector → description → title

#### Scenario: Typing in description field

- **WHEN** the description field is focused and the user types characters including spaces
- **THEN** the text SHALL be appended to the description

#### Scenario: Newlines in description

- **WHEN** the description field is focused and the user presses `ctrl+j`
- **THEN** a newline SHALL be inserted into the description text

#### Scenario: Submitting with empty description

- **WHEN** the user submits the form with a title but no description
- **THEN** the task SHALL be created with empty description fields
