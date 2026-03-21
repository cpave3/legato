## ADDED Requirements

### Requirement: Detail View Layout

The detail view SHALL be a full-screen Bubbletea model that displays a ticket's complete information. The view MUST contain a metadata header, a separator, and a Glamour-rendered markdown description section.

The metadata header MUST display the following fields in a structured grid: status, priority, type, epic, labels, and the Jira URL. The ticket key and summary MUST appear as the view title.

#### Scenario: Open detail view from board

- **WHEN** the user presses `enter` on a selected card in the board view
- **THEN** the app shell routes to the detail view, displaying the full ticket information for the selected card

#### Scenario: Metadata header displays all fields

- **WHEN** the detail view is rendered for a ticket with all metadata populated
- **THEN** the header shows status, priority, type, epic, labels, and URL in a structured layout matching the mockup grid

#### Scenario: Metadata with missing optional fields

- **WHEN** a ticket has no epic or no labels
- **THEN** those fields are either omitted or display a dash placeholder, and the layout does not break

#### Scenario: Task with linked PR showing full status

- **WHEN** the detail view opens for a task with `pr_meta` containing a PR
- **THEN** the header SHALL display a "PR" section showing: `#<number>` as a link/label, review decision (e.g., "Approved", "Changes Requested"), CI status (pass/fail/pending icon), and comment count if non-zero

#### Scenario: Task with linked branch but no PR yet

- **WHEN** the detail view opens for a task with a linked branch but no PR found
- **THEN** the header SHALL display "Branch: <name>" with a note "No PR found"

#### Scenario: Task with no linked branch

- **WHEN** the detail view opens for a task with no `pr_meta`
- **THEN** the header SHALL NOT display any PR section (same as current behavior)

#### Scenario: PR is merged

- **WHEN** the detail view shows a task whose linked PR has state MERGED
- **THEN** the PR section SHALL display "Merged" with appropriate styling

#### Scenario: PR is draft

- **WHEN** the detail view shows a task whose linked PR is a draft
- **THEN** the PR section SHALL display "Draft" indicator alongside other status fields

### Requirement: Glamour Markdown Rendering

The ticket description MUST be rendered using Glamour with terminal-appropriate styling. The renderer MUST use word-wrap configured to the available terminal width minus padding.

#### Scenario: Description renders as styled markdown

- **WHEN** the detail view displays a ticket with a markdown description containing headings, lists, code blocks, and blockquotes
- **THEN** Glamour renders each element with appropriate terminal styling (bold headings, indented lists, syntax-highlighted code blocks)

#### Scenario: Glamour re-renders on terminal resize

- **WHEN** the terminal is resized while the detail view is open
- **THEN** the Glamour renderer is re-created with the new width and the description is re-rendered to fit

### Requirement: Scroll Support

The description section MUST be scrollable using a Bubbletea viewport model. The metadata header MUST remain fixed above the scrollable area.

#### Scenario: Long description scrolls vertically

- **WHEN** the rendered description exceeds the available viewport height
- **THEN** the user can scroll down with `j` and scroll up with `k` to view the full content

#### Scenario: Half-page and full-jump scrolling

- **WHEN** the user presses `d`/`u` in the detail view
- **THEN** the viewport scrolls down/up by half a page

#### Scenario: Jump to top and bottom

- **WHEN** the user presses `g` or `G` in the detail view
- **THEN** the viewport jumps to the top or bottom of the description respectively

### Requirement: Detail View Navigation

The detail view MUST support returning to the board view and accessing the move overlay.

#### Scenario: Return to board view

- **WHEN** the user presses `esc` in the detail view
- **THEN** the app returns to the board view with the previously selected card still selected

#### Scenario: Open move overlay from detail view

- **WHEN** the user presses `m` in the detail view
- **THEN** the move overlay opens for the currently viewed ticket

### Requirement: Open in Browser

The detail view MUST support opening the ticket's Jira URL in the default system browser via the `o` keybinding.

#### Scenario: Open ticket URL on macOS

- **WHEN** the user presses `o` on macOS
- **THEN** the system executes `open <url>` to launch the Jira ticket in the default browser

#### Scenario: Open ticket URL on Linux

- **WHEN** the user presses `o` on Linux
- **THEN** the system executes `xdg-open <url>` to launch the Jira ticket in the default browser

#### Scenario: Open URL with no URL available

- **WHEN** the user presses `o` and the ticket has no URL
- **THEN** the status bar displays an error message indicating no URL is available

#### Scenario: Open PR URL

- **WHEN** the user presses `o` while viewing a task with a linked PR
- **THEN** the PR URL SHALL be opened in the default browser using the existing clipboard/browser-open mechanism

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

### Requirement: Loading State

The detail view MUST show a loading indicator if the full ticket data is not yet available and needs to be fetched.

#### Scenario: Ticket data not yet cached

- **WHEN** the user opens a ticket whose full description has not been fetched from Jira
- **THEN** the detail view displays a loading spinner or message while `GetCard()` fetches the data

#### Scenario: Ticket data already cached

- **WHEN** the user opens a ticket whose data is already in SQLite
- **THEN** the detail view renders immediately without a loading state
