## ADDED Requirements

### Requirement: Edit description from detail view

The detail view SHALL allow the user to edit the description of a local task by pressing `e`, which opens the configured terminal editor.

#### Scenario: Edit local task description

- **WHEN** the user presses `e` in the detail view of a local task (provider is nil)
- **THEN** the system SHALL write the current `description_md` to a temporary file, open the configured editor via `tea.ExecProcess`, and on editor exit read the file contents and call `BoardService.UpdateTaskDescription` to persist the changes

#### Scenario: Detail view re-renders after edit

- **WHEN** the editor exits successfully and the description has changed
- **THEN** the detail view SHALL re-render with the updated description using Glamour markdown rendering

#### Scenario: Editor exits with error

- **WHEN** the editor process exits with a non-zero exit code
- **THEN** the system SHALL discard any changes, clean up the temporary file, and display a feedback message in the status bar

#### Scenario: Edit blocked for remote tasks

- **WHEN** the user presses `e` in the detail view of a remote/synced task (provider is non-nil)
- **THEN** the system SHALL display a feedback message "Cannot edit remote task description" and take no further action

#### Scenario: Edit key shown in status bar

- **WHEN** the detail view is rendered for a local task
- **THEN** the status bar SHALL include `e edit` in the keybinding hints

## MODIFIED Requirements

### Requirement: Detail View Status Bar

The detail view MUST display a status bar at the bottom showing available keybindings: `esc` back, `y` copy description, `Y` copy full context, `m` move, `D` delete, `o` open in browser, and `e` edit (for local tasks).

#### Scenario: Status bar displays keybinding hints

- **WHEN** the detail view is rendered
- **THEN** the bottom status bar shows the keybinding hints: esc back, y copy description, Y copy full context, m move, D delete, o open in browser

#### Scenario: Status bar shows edit hint for local tasks

- **WHEN** the detail view is rendered for a local task (no provider)
- **THEN** the status bar additionally shows `e edit` in the keybinding hints

#### Scenario: Status bar shows feedback after action

- **WHEN** the user performs a copy action (`y` or `Y`) or encounters an error
- **THEN** the status bar temporarily displays a confirmation or error message before reverting to the default keybinding hints
