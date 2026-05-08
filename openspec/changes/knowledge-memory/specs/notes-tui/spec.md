## ADDED Requirements

### Requirement: Notes list view

The TUI SHALL provide a notes view accessible via `N` from the board view, displaying all notes ordered by `updated_at DESC` with title, first-line snippet, and tag list.

#### Scenario: Open notes view

- **WHEN** the user presses `N` from the board view
- **THEN** the notes view SHALL open as a full-screen view (not an overlay) showing the note list

#### Scenario: Navigate notes list

- **WHEN** the user presses `j`/`k` in the notes view
- **THEN** the highlight SHALL move down/up; `gg`/`G` SHALL jump to first/last

#### Scenario: Open note detail

- **WHEN** the user presses `enter` on a highlighted note
- **THEN** the note detail view SHALL open

#### Scenario: Return to board

- **WHEN** the user presses `esc` or `b` in the notes view
- **THEN** the board view SHALL return as the active view

### Requirement: Notes search

The notes view SHALL support real-time FTS search via `/`.

#### Scenario: Open search

- **WHEN** the user presses `/` in the notes view
- **THEN** a search input SHALL appear at the top and the list SHALL filter to FTS matches as the user types

#### Scenario: Submit search

- **WHEN** the user presses `enter` on the search input
- **THEN** focus SHALL return to the list with the filtered results

#### Scenario: Cancel search

- **WHEN** the user presses `esc` while searching
- **THEN** the search input SHALL close and the full list SHALL return

### Requirement: Note detail view

The note detail view SHALL render the markdown body via Glamour and display backlinks plus outgoing links.

#### Scenario: Render markdown

- **WHEN** a note is opened in detail view
- **THEN** the body SHALL render via Glamour with the dark style configuration

#### Scenario: Backlinks section

- **WHEN** a note is rendered in detail view and has backlinks
- **THEN** a "Backlinks" section SHALL appear at the bottom listing each backlink as a navigable item

#### Scenario: Edit note

- **WHEN** the user presses `e` in note detail
- **THEN** the system SHALL open the note's markdown file in `$EDITOR` via `tea.ExecProcess`; on close, the file SHALL be reloaded into the DB

### Requirement: Wikilink autocomplete in editor

When the user types `[[` in the description editor (task or note), the TUI SHALL show a popup with note slugs and task IDs to complete the wikilink.

#### Scenario: Trigger autocomplete

- **WHEN** the user types `[[` in a description text input
- **THEN** an autocomplete popup SHALL appear listing notes (by title) and tasks (by `task:<id> — title`)

#### Scenario: Filter completions

- **WHEN** the popup is open and the user continues typing
- **THEN** the list SHALL filter via prefix match against title and slug

#### Scenario: Accept completion

- **WHEN** the user presses `enter` on a highlighted completion
- **THEN** the wikilink text SHALL be inserted (e.g., `[[arch-overview]]` or `[[task:abc12345]]`) and the popup SHALL close

#### Scenario: Cancel completion

- **WHEN** the user presses `esc` while the popup is open
- **THEN** the popup SHALL close without modifying the input

### Requirement: New note creation

The notes view SHALL support creating a new note via `n`.

#### Scenario: Create new note

- **WHEN** the user presses `n` in the notes view
- **THEN** a create overlay SHALL prompt for title; on submit, the system SHALL open `$EDITOR` for the body, then save on editor close

### Requirement: Wikilink navigation

In the note detail view, the user SHALL be able to follow rendered wikilinks.

#### Scenario: List outgoing links

- **WHEN** a note is rendered in detail view and has outgoing wikilinks
- **THEN** an "Outgoing" section SHALL list each link as a navigable item with `j`/`k`/`enter`

#### Scenario: Follow note link

- **WHEN** an outgoing link to another note is selected and `enter` is pressed
- **THEN** the detail view SHALL navigate to that note

#### Scenario: Follow task link

- **WHEN** an outgoing link to a task is selected and `enter` is pressed
- **THEN** the detail view SHALL switch to the task detail view for that task ID
