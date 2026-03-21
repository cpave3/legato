## MODIFIED Requirements

### Requirement: Task data model

The system SHALL represent all board items as tasks with a core schema: `id` (TEXT PRIMARY KEY), `title`, `description`, `description_md`, `status`, `priority`, `sort_order`, `workspace_id` (nullable INTEGER FK to workspaces), `created_at`, `updated_at`. Tasks optionally link to an external provider via nullable `provider`, `remote_id`, and `remote_meta` (JSON) fields.

#### Scenario: Local task with no provider

- **WHEN** a task is created without specifying a provider
- **THEN** the task SHALL have `provider` and `remote_id` as NULL, and `remote_meta` as NULL

#### Scenario: Synced task with provider link

- **WHEN** a task is created via provider sync (e.g. Jira)
- **THEN** the task SHALL have `provider` set to the provider name (e.g. "jira"), `remote_id` set to the provider's ID (e.g. "REX-1234"), and `remote_meta` containing provider-specific fields as JSON

#### Scenario: Task with workspace

- **WHEN** a task is created with a workspace_id
- **THEN** the task SHALL be linked to that workspace and appear when that workspace is the active filter

#### Scenario: Task without workspace

- **WHEN** a task is created without a workspace_id
- **THEN** the task SHALL have `workspace_id` as NULL and appear in "Unassigned" and "All" views

### Requirement: Task CRUD operations

The store SHALL provide create, read, update, list, and delete operations for tasks.

#### Scenario: Create a task

- **WHEN** a new task is inserted
- **THEN** it SHALL be persisted with all core fields including `workspace_id` and `created_at`/`updated_at` set to the current time

#### Scenario: Get a task by ID

- **WHEN** a task is requested by ID and exists
- **THEN** the full task record SHALL be returned including `workspace_id` and `remote_meta`

#### Scenario: Get a non-existent task

- **WHEN** a task is requested by ID and does not exist
- **THEN** the system SHALL return a not-found error

#### Scenario: List tasks by status

- **WHEN** tasks are listed for a given status (column name)
- **THEN** all tasks with that status SHALL be returned ordered by `sort_order` ascending

#### Scenario: Update a task

- **WHEN** a task's fields are updated
- **THEN** the changes SHALL be persisted and `updated_at` set to the current time

#### Scenario: Upsert a task

- **WHEN** a task is upserted (insert or update on conflict)
- **THEN** if the task ID exists, it SHALL be updated; otherwise it SHALL be inserted

#### Scenario: Delete a task

- **WHEN** a task is deleted by ID
- **THEN** it SHALL be removed from the database

## MODIFIED Requirements

### Requirement: Task creation with workspace

`BoardService.CreateTask` SHALL accept an optional workspace parameter and assign the new task to that workspace.

#### Scenario: Create task with active workspace

- **WHEN** a task is created while a specific workspace is the active view
- **THEN** the task SHALL be assigned to that workspace by default

#### Scenario: Create task in "All" view

- **WHEN** a task is created while "All" is the active view
- **THEN** the task SHALL be created with no workspace (unassigned) unless the user selects one in the create overlay

#### Scenario: Create overlay workspace picker

- **WHEN** the create overlay is opened
- **THEN** it SHALL include a workspace field (cycling with h/l like column) pre-filled with the active workspace, or "None" if in All/Unassigned view
