## ADDED Requirements

### Requirement: Sync State Display

The status bar SHALL display the current sync state using a colored indicator and descriptive text.

#### Scenario: Synced state

- **WHEN** the most recent sync completed successfully
- **THEN** the status bar SHALL display a green dot indicator followed by "synced"

#### Scenario: Syncing state

- **WHEN** a sync operation is in progress
- **THEN** the status bar SHALL display a yellow dot indicator followed by "syncing..."

#### Scenario: Sync error state

- **WHEN** the most recent sync failed
- **THEN** the status bar SHALL display a red dot indicator followed by "sync error"

#### Scenario: Offline state

- **WHEN** no sync has been attempted or the service is unavailable
- **THEN** the status bar SHALL display a grey dot indicator followed by "offline"

### Requirement: Last Sync Time

The status bar SHALL display the elapsed time since the last successful sync in a human-readable relative format.

#### Scenario: Recent sync

- **WHEN** the last sync completed less than 60 seconds ago
- **THEN** the status bar SHALL display the time as "Ns ago" (e.g., "45s ago")

#### Scenario: Minutes-old sync

- **WHEN** the last sync completed between 1 and 59 minutes ago
- **THEN** the status bar SHALL display the time as "Nm ago" (e.g., "2m ago")

#### Scenario: Hours-old sync

- **WHEN** the last sync completed 1 or more hours ago
- **THEN** the status bar SHALL display the time as "Nh ago" (e.g., "1h ago")

### Requirement: Contextual Key Hints

The status bar SHALL display key hints relevant to the current view and context.

#### Scenario: Board view key hints

- **WHEN** the board view is active
- **THEN** the status bar SHALL display hints for: `h/l` column, `j/k` card, `enter` detail, `m` move, `w` workspace, `r` sync, `/` search, `?` help

#### Scenario: Key hint layout

- **WHEN** the status bar renders key hints
- **THEN** each hint SHALL be formatted as the key binding in a distinct style followed by the action name, with hints separated by spacing

### Requirement: Status Bar Layout

The status bar SHALL span the full terminal width and occupy a single line at the bottom of the screen.

#### Scenario: Full-width rendering

- **WHEN** the status bar is rendered
- **THEN** it SHALL use the full terminal width with sync state and time on the left and key hints distributed across the remaining space

#### Scenario: Narrow terminal

- **WHEN** the terminal is too narrow to fit all key hints
- **THEN** the status bar SHALL truncate key hints from the right, always preserving the sync state display

### Requirement: EventBus Subscription

The status bar model SHALL update its state in response to sync events delivered as Bubbletea messages.

#### Scenario: Sync started event

- **WHEN** the status bar receives a sync-started message
- **THEN** it SHALL transition to the syncing display state

#### Scenario: Sync completed event

- **WHEN** the status bar receives a sync-completed message
- **THEN** it SHALL transition to the synced display state and record the current time as the last sync time

#### Scenario: Sync failed event

- **WHEN** the status bar receives a sync-failed message
- **THEN** it SHALL transition to the sync error display state

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
