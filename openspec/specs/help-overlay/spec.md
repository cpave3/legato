## ADDED Requirements

### Requirement: Help overlay activation

The system SHALL open a help overlay when the user presses `?` from any view (board, detail, or while another overlay is active). The help overlay MUST replace any currently active overlay.

#### Scenario: Open help from board view

- **WHEN** the user presses `?` on the board view
- **THEN** a help overlay appears centered on a dimmed board background, titled "Legato -- Keyboard Reference"

#### Scenario: Open help from detail view

- **WHEN** the user presses `?` while viewing a ticket in the detail view
- **THEN** a help overlay appears with the same content and layout

### Requirement: Keybinding display organized by context

The help overlay SHALL display all keybindings organized into three sections: Navigation, Actions, and General. Each entry MUST show the key(s) on the left and a description on the right.

#### Scenario: Navigation section content

- **WHEN** the help overlay is displayed
- **THEN** the Navigation section lists: `h/l` (move between columns), `j/k` (move up/down within column), `g/G` (jump to first/last card), `1-5` (jump to column by number)

#### Scenario: Actions section content

- **WHEN** the help overlay is displayed
- **THEN** the Actions section lists: `enter` (open ticket detail), `m` (move ticket), `y` (copy description), `Y` (copy full context), `r` (force refresh), `/` (filter tickets), `esc` (back/close overlay)

#### Scenario: General section content

- **WHEN** the help overlay is displayed
- **THEN** the General section lists: `?` (help screen), `q` (quit)

### Requirement: Help overlay dismissal

The system SHALL close the help overlay when the user presses `esc` or `?` again, returning to the previous view.

#### Scenario: Dismiss help with escape

- **WHEN** the user presses `esc` while the help overlay is active
- **THEN** the overlay closes and the previous view is restored

#### Scenario: Dismiss help by pressing ? again

- **WHEN** the user presses `?` while the help overlay is active
- **THEN** the overlay closes and the previous view is restored

### Requirement: Help overlay is read-only

The help overlay SHALL not accept any input other than dismissal keys. All other key presses MUST be ignored.

#### Scenario: Non-dismissal keys are ignored

- **WHEN** the user presses any key other than `esc` or `?` while the help overlay is active
- **THEN** nothing happens and the overlay remains displayed
