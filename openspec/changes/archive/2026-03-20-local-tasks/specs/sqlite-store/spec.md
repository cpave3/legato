## MODIFIED Requirements

### Requirement: Tickets Table Schema

The database SHALL contain a `tasks` table (renamed from `tickets`) with columns: `id` (TEXT PRIMARY KEY), `title` (TEXT NOT NULL), `description` (TEXT NOT NULL DEFAULT ''), `description_md` (TEXT NOT NULL DEFAULT ''), `status` (TEXT NOT NULL DEFAULT ''), `priority` (TEXT NOT NULL DEFAULT ''), `sort_order` (INTEGER NOT NULL DEFAULT 0), `provider` (TEXT, nullable), `remote_id` (TEXT, nullable), `remote_meta` (TEXT, nullable — JSON), `created_at` (DATETIME NOT NULL), `updated_at` (DATETIME NOT NULL).

#### Scenario: Tasks table exists after migration

- **WHEN** the database is initialized or migrated
- **THEN** the `tasks` table SHALL exist with the specified columns, and the `tickets` table SHALL NOT exist

#### Scenario: Migration from tickets to tasks

- **WHEN** the database has an existing `tickets` table from a prior version
- **THEN** the migration SHALL create the `tasks` table, copy all ticket data into it (mapping `summary` → `title`, packing remote fields into `remote_meta` JSON, setting `provider='jira'` and `remote_id` to the ticket ID), update `agent_sessions` and `sync_log` references, and drop the `tickets` table

### Requirement: Ticket CRUD Operations

The store SHALL provide CRUD operations for tasks (renamed from tickets): `CreateTask`, `GetTask`, `ListTasksByStatus`, `UpdateTask`, `UpsertTask`, `DeleteTask`, `ListAllTasks`, `ListTaskIDs`.

#### Scenario: Create a task

- **WHEN** `CreateTask` is called with a valid Task struct
- **THEN** a new row is inserted into the `tasks` table with all fields populated

#### Scenario: Create a duplicate task

- **WHEN** `CreateTask` is called with an ID that already exists
- **THEN** an error is returned

#### Scenario: Get a task by ID

- **WHEN** `GetTask` is called with an existing ID
- **THEN** the full task record is returned

#### Scenario: Get a non-existent task

- **WHEN** `GetTask` is called with a non-existent ID
- **THEN** `ErrNotFound` is returned

#### Scenario: List tasks by status

- **WHEN** `ListTasksByStatus` is called with a status string
- **THEN** all tasks matching that status are returned ordered by `sort_order` ascending

#### Scenario: Update a task

- **WHEN** `UpdateTask` is called with modified fields
- **THEN** the task record is updated in place

#### Scenario: Delete a task

- **WHEN** `DeleteTask` is called with an existing ID
- **THEN** the task is removed from the database

### Requirement: Schema Migrations

The migration system SHALL support the new `005_tasks.sql` migration that transforms the `tickets` table into the `tasks` table.

#### Scenario: Fresh database receives all migrations

- **WHEN** a new database is created
- **THEN** all migrations including `005_tasks.sql` SHALL be applied, resulting in a `tasks` table (no `tickets` table)

#### Scenario: Existing database migrates from tickets to tasks

- **WHEN** an existing database at user_version 4 is opened
- **THEN** migration `005_tasks.sql` SHALL run, preserving all data in the new `tasks` table
