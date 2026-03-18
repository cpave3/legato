## ADDED Requirements

### Requirement: Task data model

The system SHALL represent all board items as tasks with a core schema: `id` (TEXT PRIMARY KEY), `title`, `description`, `description_md`, `status`, `priority`, `sort_order`, `created_at`, `updated_at`. Tasks optionally link to an external provider via nullable `provider`, `remote_id`, and `remote_meta` (JSON) fields.

#### Scenario: Local task with no provider

- **WHEN** a task is created without specifying a provider
- **THEN** the task SHALL have `provider` and `remote_id` as NULL, and `remote_meta` as NULL

#### Scenario: Synced task with provider link

- **WHEN** a task is created via provider sync (e.g. Jira)
- **THEN** the task SHALL have `provider` set to the provider name (e.g. "jira"), `remote_id` set to the provider's ID (e.g. "REX-1234"), and `remote_meta` containing provider-specific fields as JSON

### Requirement: Task ID generation

The system SHALL generate short random IDs for locally created tasks. Users MAY specify a custom ID at creation time.

#### Scenario: Auto-generated ID

- **WHEN** a task is created without a specified ID
- **THEN** the system SHALL generate an 8-character lowercase alphanumeric ID using `crypto/rand` and verify it does not collide with existing task IDs before insert

#### Scenario: User-specified ID

- **WHEN** a task is created with a user-specified ID
- **THEN** the system SHALL use that ID, returning an error if it already exists

#### Scenario: Provider-assigned ID

- **WHEN** a task is created via provider sync
- **THEN** the task ID SHALL be the provider's ID (e.g. "REX-1234" for Jira)

### Requirement: Task CRUD operations

The store SHALL provide create, read, update, list, and delete operations for tasks.

#### Scenario: Create a task

- **WHEN** a new task is inserted
- **THEN** it SHALL be persisted with all core fields and `created_at`/`updated_at` set to the current time

#### Scenario: Get a task by ID

- **WHEN** a task is requested by ID and exists
- **THEN** the full task record SHALL be returned including `remote_meta`

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

### Requirement: Remote metadata handling

The `remote_meta` field SHALL store provider-specific data as a JSON string. The store layer SHALL treat it as opaque — parsing is the responsibility of the service/sync layer.

#### Scenario: Storing remote metadata

- **WHEN** a synced task is created or updated with remote metadata
- **THEN** the `remote_meta` field SHALL contain a valid JSON string with provider-specific fields

#### Scenario: Local task has no remote metadata

- **WHEN** a local task is created
- **THEN** the `remote_meta` field SHALL be NULL

#### Scenario: Querying by provider

- **WHEN** the system needs to find all tasks from a specific provider
- **THEN** it SHALL query using the indexed `provider` column, not by parsing `remote_meta`
