## MODIFIED Requirements

### Requirement: Task Storage Schema

The tasks table SHALL include a nullable `pr_meta` TEXT column for storing PR tracking metadata as JSON. This column is independent of `remote_meta` and MUST NOT be affected by ticket provider sync operations.

#### Scenario: Migration adds pr_meta column

- **WHEN** the database is migrated to the new version
- **THEN** the `tasks` table SHALL have a new `pr_meta TEXT` column defaulting to NULL

#### Scenario: Existing tasks unaffected

- **WHEN** the migration runs on a database with existing tasks
- **THEN** all existing tasks SHALL have `pr_meta = NULL` and all other columns unchanged

### Requirement: Update PR metadata on a task

The store SHALL support updating the `pr_meta` JSON field for a given task ID without affecting other task fields.

#### Scenario: Set pr_meta on a task

- **WHEN** `UpdatePRMeta(id, prMetaJSON)` is called with valid JSON
- **THEN** the task's `pr_meta` column SHALL be updated and `updated_at` set to current time

#### Scenario: Clear pr_meta on a task

- **WHEN** `UpdatePRMeta(id, nil)` is called
- **THEN** the task's `pr_meta` column SHALL be set to NULL

#### Scenario: Update pr_meta for non-existent task

- **WHEN** `UpdatePRMeta` is called with an ID that does not exist
- **THEN** the store SHALL return an error indicating the task was not found

### Requirement: Query tasks with linked branches

The store SHALL support querying all tasks that have non-NULL `pr_meta` containing a `branch` field, for use by the PR polling service.

#### Scenario: List tasks with PR tracking

- **WHEN** `ListPRTrackedTasks()` is called and 3 tasks have `pr_meta` with branch values
- **THEN** the store SHALL return those 3 tasks with their `pr_meta` parsed

#### Scenario: No tasks with PR tracking

- **WHEN** `ListPRTrackedTasks()` is called and no tasks have `pr_meta` set
- **THEN** the store SHALL return an empty list without error
