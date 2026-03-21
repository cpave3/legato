## MODIFIED Requirements

### Requirement: Contextual Key Hints

The status bar SHALL display key hints relevant to the current view and context.

#### Scenario: Board view key hints

- **WHEN** the board view is active
- **THEN** the status bar SHALL display hints for: `h/l` column, `j/k` card, `enter` detail, `m` move, `w` workspace, `r` sync, `/` search, `?` help

#### Scenario: Key hint layout

- **WHEN** the status bar renders key hints
- **THEN** each hint SHALL be formatted as the key binding in a distinct style followed by the action name, with hints separated by spacing

## ADDED Requirements

### Requirement: Workspace indicator

The status bar SHALL display the active workspace name when a workspace filter is active.

#### Scenario: Specific workspace active

- **WHEN** a specific workspace is the active filter
- **THEN** the status bar SHALL display the workspace name in the workspace's configured color, positioned after the sync state

#### Scenario: "All" view active

- **WHEN** "All" is the active workspace view
- **THEN** the status bar SHALL display "All" in a neutral color after the sync state

#### Scenario: "Unassigned" view active

- **WHEN** "Unassigned" is the active workspace view
- **THEN** the status bar SHALL display "Unassigned" in a dim/muted color after the sync state
