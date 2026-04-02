## MODIFIED Requirements

### Requirement: Task data model

The system SHALL represent all board items as tasks with a core schema: `id` (TEXT PRIMARY KEY), `title`, `description`, `description_md`, `status`, `priority`, `sort_order`, `workspace_id` (nullable INTEGER FK to workspaces), `created_at`, `updated_at`. Tasks optionally link to an external provider via nullable `provider`, `remote_id`, and `remote_meta` (JSON) fields. For bound tasks (local task linked to remote ticket), `id` SHALL remain the original local ID while `remote_id` stores the provider's key.

#### Scenario: Local task with no provider

- **WHEN** a task is created without specifying a provider
- **THEN** the task SHALL have `provider` and `remote_id` as NULL, and `remote_meta` as NULL

#### Scenario: Synced task with provider link

- **WHEN** a task is created via provider sync (e.g. Jira)
- **THEN** the task SHALL have `provider` set to the provider name (e.g. "jira"), `remote_id` set to the provider's ID (e.g. "REX-1234"), and `remote_meta` containing provider-specific fields as JSON

#### Scenario: Bound task with divergent IDs

- **WHEN** a local task is bound to a remote ticket
- **THEN** the task SHALL have `id` set to the original local ID (e.g., `a1b2c3d4`), `provider` set to the provider name, `remote_id` set to the provider's key (e.g., `REX-123`), and `remote_meta` populated with provider-specific fields

#### Scenario: Task with workspace

- **WHEN** a task is created with a workspace_id
- **THEN** the task SHALL be linked to that workspace and appear when that workspace is the active filter

#### Scenario: Task without workspace

- **WHEN** a task is created without a workspace_id
- **THEN** the task SHALL have `workspace_id` as NULL and appear in "Unassigned" and "All" views

### Requirement: Remote metadata handling

The `remote_meta` field SHALL store provider-specific data as a JSON string. The store layer SHALL treat it as opaque — parsing is the responsibility of the service/sync layer.

#### Scenario: Storing remote metadata

- **WHEN** a synced task is created or updated with remote metadata
- **THEN** the `remote_meta` field SHALL contain a valid JSON string with provider-specific fields

#### Scenario: Local task has no remote metadata

- **WHEN** a local task is created
- **THEN** the `remote_meta` field SHALL be NULL

#### Scenario: Bound task has remote metadata

- **WHEN** a local task is bound to a remote ticket
- **THEN** the `remote_meta` field SHALL be populated with provider-specific fields, identical to a normally-imported task

#### Scenario: Querying by provider

- **WHEN** the system needs to find all tasks from a specific provider
- **THEN** it SHALL query using the indexed `provider` column, not by parsing `remote_meta`

### Requirement: Update task description via service

The `BoardService` SHALL expose an `UpdateTaskDescription(ctx, id, description)` method that updates the description of a local task.

#### Scenario: Successful description update

- **WHEN** `UpdateTaskDescription` is called with a valid local task ID and new description content
- **THEN** the service SHALL update both `description` and `description_md` fields on the task, persist via the store, and publish a cards-refreshed event

#### Scenario: Reject editing remote task description

- **WHEN** `UpdateTaskDescription` is called with a task ID that has a non-nil provider (remote/synced task, including bound tasks)
- **THEN** the service SHALL return an error indicating that remote task descriptions cannot be edited locally

#### Scenario: Task not found

- **WHEN** `UpdateTaskDescription` is called with a non-existent task ID
- **THEN** the service SHALL return a not-found error
