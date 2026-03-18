## ADDED Requirements

### Requirement: Move overlay activation

The system SHALL open a move overlay when the user presses `m` from the board view or detail view. The overlay MUST display the ticket key and summary at the top, followed by a list of all available columns with single-letter shortcuts.

#### Scenario: Open move overlay from board view

- **WHEN** the user presses `m` while a card is selected on the board
- **THEN** a move overlay appears centered on a dimmed board background, showing "Move {ticket-key}" as the title, the ticket summary as a subtitle, and all columns listed with their shortcut keys

#### Scenario: Open move overlay from detail view

- **WHEN** the user presses `m` while viewing a ticket in the detail view
- **THEN** a move overlay appears for the currently viewed ticket with the same layout as from the board view

### Requirement: Current column highlighting

The move overlay MUST visually distinguish the ticket's current column from the other columns. The current column SHALL display a "current" label instead of a shortcut key.

#### Scenario: Current column is highlighted

- **WHEN** the move overlay is open for a ticket in the "Doing" column
- **THEN** the "Doing" row is visually highlighted and shows "current" instead of a shortcut key

### Requirement: Single-keypress column selection

Each column in the move overlay SHALL have a single-letter shortcut. Pressing the shortcut key MUST immediately trigger a move to that column and close the overlay. The default shortcuts SHALL be: `b` (Backlog), `r` (Ready), `d` (Doing), `v` (Review), `x` (Done).

#### Scenario: Move ticket via shortcut key

- **WHEN** the user presses `r` in the move overlay
- **THEN** the ticket moves to the "Ready" column, the overlay closes, and the board reflects the new position

#### Scenario: Pressing current column shortcut does nothing

- **WHEN** the user presses the shortcut key for the ticket's current column
- **THEN** no move occurs and the overlay closes

### Requirement: Async Jira transition on move

When a ticket is moved via the overlay, the system SHALL update the local SQLite state immediately and fire an asynchronous Jira transition. The UI MUST NOT block while waiting for the Jira response.

#### Scenario: Successful async transition

- **WHEN** the user moves a ticket to "Review" via the overlay
- **THEN** the card immediately appears in the "Review" column locally, and a Jira transition request is sent asynchronously in the background

### Requirement: Move failure error surfacing

If the Jira transition fails, the system SHALL display a warning icon on the affected card and show an error message in the status bar. The card MUST remain in the locally chosen column (preserving user intent).

#### Scenario: Transition failure

- **WHEN** a Jira transition fails after a move
- **THEN** the card stays in the target column, a warning indicator appears on the card, the status bar shows an error message, and the failure is logged to `sync_log`

### Requirement: Move overlay dismissal

The system SHALL close the move overlay without taking action when the user presses `esc`.

#### Scenario: Dismiss move overlay

- **WHEN** the user presses `esc` while the move overlay is active
- **THEN** the overlay closes and no move occurs
