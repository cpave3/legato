## ADDED Requirements

### Requirement: Workspaces table

The system SHALL maintain a `workspaces` table with columns: `id` (INTEGER PRIMARY KEY), `name` (TEXT UNIQUE NOT NULL), `color` (TEXT, nullable), `sort_order` (INTEGER NOT NULL DEFAULT 0).

#### Scenario: Creating a workspace

- **WHEN** a workspace is inserted with a unique name
- **THEN** it SHALL be persisted with an auto-generated integer ID and the provided name, color, and sort_order

#### Scenario: Duplicate workspace name

- **WHEN** a workspace is inserted with a name that already exists
- **THEN** the system SHALL return a uniqueness constraint error

#### Scenario: Listing workspaces

- **WHEN** workspaces are listed
- **THEN** all workspaces SHALL be returned ordered by `sort_order` ascending, then `name` ascending

### Requirement: Task workspace foreign key

The `tasks` table SHALL have a nullable `workspace_id` INTEGER column referencing `workspaces(id)`.

#### Scenario: Task with workspace

- **WHEN** a task is created or updated with a valid workspace_id
- **THEN** the task SHALL be linked to that workspace

#### Scenario: Task without workspace

- **WHEN** a task is created without a workspace_id (or with NULL)
- **THEN** the task SHALL be unassigned and appear in "Unassigned" and "All" views

#### Scenario: Invalid workspace reference

- **WHEN** a task is created with a workspace_id that does not exist in the workspaces table
- **THEN** the system SHALL return a foreign key constraint error

### Requirement: Workspace-filtered task queries

The store SHALL support listing tasks by status filtered by workspace view.

#### Scenario: List tasks for a specific workspace

- **WHEN** tasks are listed with a workspace filter for workspace ID N
- **THEN** only tasks with `workspace_id = N` and the given status SHALL be returned, ordered by sort_order

#### Scenario: List unassigned tasks

- **WHEN** tasks are listed with the "unassigned" workspace filter
- **THEN** only tasks with `workspace_id IS NULL` and the given status SHALL be returned, ordered by sort_order

#### Scenario: List all tasks (no filter)

- **WHEN** tasks are listed with the "all" workspace view
- **THEN** all tasks with the given status SHALL be returned regardless of workspace_id, ordered by sort_order

### Requirement: Workspace seeding from config

On startup, the system SHALL read the `workspaces` config section and ensure each listed workspace exists in the database.

#### Scenario: New workspace in config

- **WHEN** a workspace name from config does not exist in the database
- **THEN** the system SHALL insert it with the configured name, color, and sort_order

#### Scenario: Existing workspace in config

- **WHEN** a workspace name from config already exists in the database
- **THEN** the system SHALL not create a duplicate (skip or update color/sort_order)

#### Scenario: Workspace removed from config

- **WHEN** a workspace exists in the database but is not in the config
- **THEN** the system SHALL NOT delete it (preserves tasks linked to it)

#### Scenario: No workspaces in config

- **WHEN** the config has no `workspaces` section
- **THEN** no workspaces SHALL be seeded, and all tasks remain unassigned

### Requirement: Workspace switcher overlay

The board SHALL provide a `w` keybinding that opens a workspace switcher overlay listing all workspaces plus "All" and "Unassigned" options.

#### Scenario: Opening the switcher

- **WHEN** the user presses `w` on the board view
- **THEN** a workspace switcher overlay SHALL appear showing "All", "Unassigned", and each workspace name (with color indicator), ordered by sort_order

#### Scenario: Selecting a workspace

- **WHEN** the user selects a workspace from the switcher (via shortcut key or enter)
- **THEN** the board SHALL filter to show only tasks in that workspace, the overlay SHALL close, and the status bar SHALL update to show the selected workspace

#### Scenario: Selecting "All"

- **WHEN** the user selects "All" from the switcher
- **THEN** the board SHALL show all tasks from all workspaces and unassigned, with workspace tags on each card

#### Scenario: Selecting "Unassigned"

- **WHEN** the user selects "Unassigned" from the switcher
- **THEN** the board SHALL show only tasks with no workspace assigned

#### Scenario: Dismissing the switcher

- **WHEN** the user presses `esc` in the workspace switcher
- **THEN** the overlay SHALL close with no change to the active workspace view

### Requirement: Workspace view persistence within session

The active workspace view SHALL persist for the duration of the TUI session.

#### Scenario: Default view on startup

- **WHEN** the TUI starts
- **THEN** the active workspace view SHALL be "All"

#### Scenario: View persists across data refreshes

- **WHEN** the board data is refreshed (sync, create, delete)
- **THEN** the active workspace filter SHALL remain unchanged

### Requirement: Update task workspace

The service layer SHALL provide a method to update a task's workspace assignment.

#### Scenario: Assign workspace to local task

- **WHEN** `UpdateTaskWorkspace(id, workspaceID)` is called for a local task
- **THEN** the task's `workspace_id` SHALL be updated and `EventCardsRefreshed` published

#### Scenario: Remove workspace from task

- **WHEN** `UpdateTaskWorkspace(id, nil)` is called
- **THEN** the task's `workspace_id` SHALL be set to NULL (unassigned)

#### Scenario: Reject workspace update for remote task

- **WHEN** `UpdateTaskWorkspace` is called for a task with a non-null provider
- **THEN** the system SHALL return an error (remote tasks cannot have workspaces assigned)
