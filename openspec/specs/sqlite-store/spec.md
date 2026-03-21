## Requirements

### Requirement: Database Initialization

The store package SHALL initialize a SQLite database connection using `modernc.org/sqlite` via the `database/sql` interface wrapped with `sqlx`. The store SHALL create the database file and any parent directories if they do not exist. The store SHALL enable WAL journal mode for concurrent read performance. The store SHALL set `foreign_keys = ON`.

#### Scenario: First-time database creation
- **WHEN** the store is opened with a path where no database file exists
- **THEN** the database file SHALL be created, WAL mode SHALL be enabled, and the connection SHALL be usable for queries

#### Scenario: Existing database reopened
- **WHEN** the store is opened with a path to an existing database file
- **THEN** the existing data SHALL be preserved and accessible

#### Scenario: Parent directory does not exist
- **WHEN** the store is opened with a path whose parent directories do not exist
- **THEN** the parent directories SHALL be created with permissions 0700 before creating the database file

### Requirement: Schema Migrations

The store SHALL apply schema migrations on startup using embedded SQL files. Migrations SHALL be tracked using SQLite's `user_version` pragma. Each migration SHALL run inside a transaction so that a failed migration does not leave the database in a partial state.

#### Scenario: Fresh database receives all migrations
- **WHEN** the store opens a database with `user_version = 0`
- **THEN** all migrations including `005_tasks.sql` SHALL be applied, resulting in a `tasks` table (no `tickets` table)

#### Scenario: Already-migrated database is opened
- **WHEN** the store opens a database whose `user_version` matches the latest migration version
- **THEN** no migrations SHALL be applied and existing data SHALL remain intact

#### Scenario: Migration failure rolls back
- **WHEN** a migration fails partway through execution
- **THEN** the transaction SHALL be rolled back, `user_version` SHALL remain at its previous value, and the store SHALL return an error

#### Scenario: Existing database migrates from tickets to tasks
- **WHEN** an existing database at user_version 4 is opened
- **THEN** migration `005_tasks.sql` SHALL run, preserving all data in the new `tasks` table

### Requirement: Tasks Table Schema

The database SHALL contain a `tasks` table with columns: `id` (TEXT PRIMARY KEY), `title` (TEXT NOT NULL), `description` (TEXT NOT NULL DEFAULT ''), `description_md` (TEXT NOT NULL DEFAULT ''), `status` (TEXT NOT NULL DEFAULT ''), `priority` (TEXT NOT NULL DEFAULT ''), `sort_order` (INTEGER NOT NULL DEFAULT 0), `workspace_id` (INTEGER, nullable FK to workspaces), `provider` (TEXT, nullable), `remote_id` (TEXT, nullable), `remote_meta` (TEXT, nullable -- JSON), `archived_at` (DATETIME, nullable), `created_at` (DATETIME NOT NULL), `updated_at` (DATETIME NOT NULL).

#### Scenario: Tasks table exists after migration
- **WHEN** the database is initialized or migrated
- **THEN** the `tasks` table SHALL exist with the specified columns including `archived_at`

#### Scenario: Migration from tickets to tasks
- **WHEN** the database has an existing `tickets` table from a prior version
- **THEN** the migration SHALL create the `tasks` table, copy all ticket data into it (mapping `summary` to `title`, packing remote fields into `remote_meta` JSON, setting `provider='jira'` and `remote_id` to the ticket ID), update `agent_sessions` and `sync_log` references, and drop the `tickets` table

#### Scenario: Existing database receives archive migration
- **WHEN** an existing database without `archived_at` is opened
- **THEN** migration `009_archive.sql` SHALL add the `archived_at` column with NULL default, preserving all existing data as non-archived

### Requirement: Column Mappings Table Schema

The initial migration SHALL create a `column_mappings` table with the following columns: `id` (INTEGER PRIMARY KEY AUTOINCREMENT), `column_name` (TEXT NOT NULL UNIQUE), `jira_statuses` (TEXT NOT NULL, JSON array), `jira_transition` (TEXT), `sort_order` (INTEGER DEFAULT 0).

#### Scenario: Column mappings table exists after migration
- **WHEN** the initial migration has been applied
- **THEN** inserting a row into `column_mappings` with a unique `column_name` SHALL succeed, and inserting a duplicate `column_name` SHALL fail with a constraint error

### Requirement: Sync Log Table Schema

The initial migration SHALL create a `sync_log` table with the following columns: `id` (INTEGER PRIMARY KEY AUTOINCREMENT), `ticket_id` (TEXT NOT NULL), `action` (TEXT NOT NULL), `detail` (TEXT), `created_at` (TEXT NOT NULL DEFAULT datetime('now')).

#### Scenario: Sync log table exists after migration
- **WHEN** the initial migration has been applied
- **THEN** inserting a row into `sync_log` with `ticket_id` and `action` SHALL succeed, and the `created_at` column SHALL be automatically populated

### Requirement: Task CRUD Operations

The store SHALL provide CRUD operations for tasks (renamed from tickets): `CreateTask`, `GetTask`, `ListTasksByStatus`, `UpdateTask`, `UpsertTask`, `DeleteTask`, `ListAllTasks`, `ListTaskIDs`. All operations MUST accept a `context.Context` for cancellation. The store SHALL use `sqlx` named parameters for insert and update operations.

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

#### Scenario: Upsert a task
- **WHEN** a task is upserted (insert or update on conflict)
- **THEN** if the task ID exists, it SHALL be updated; otherwise it SHALL be inserted

#### Scenario: Delete a task
- **WHEN** `DeleteTask` is called with an existing ID
- **THEN** the task is removed from the database

### Requirement: Column Mapping CRUD Operations

The store SHALL provide functions for creating, reading, updating, and deleting column mappings. Listing column mappings SHALL return them ordered by `sort_order` ascending.

#### Scenario: Create a column mapping
- **WHEN** a column mapping with a unique `column_name` is passed to the create function
- **THEN** the mapping SHALL be persisted in the `column_mappings` table

#### Scenario: List all column mappings
- **WHEN** the list function is called
- **THEN** all column mappings SHALL be returned ordered by `sort_order` ascending

#### Scenario: Update a column mapping
- **WHEN** a column mapping with an existing `id` and modified `jira_statuses` is passed to the update function
- **THEN** the updated values SHALL be persisted

#### Scenario: Delete a column mapping
- **WHEN** a valid column mapping `id` is passed to the delete function
- **THEN** the mapping SHALL be removed from the `column_mappings` table

### Requirement: Sync Log Operations

The store SHALL provide a function to insert sync log entries and a function to list recent sync log entries for a given ticket. The insert function MUST NOT require `created_at` as the database default handles it.

#### Scenario: Insert a sync log entry
- **WHEN** a sync log entry with `ticket_id` and `action` is passed to the insert function
- **THEN** the entry SHALL be persisted with `created_at` automatically set

#### Scenario: List sync log entries for a ticket
- **WHEN** a `ticket_id` is passed to the list function
- **THEN** all sync log entries for that ticket SHALL be returned ordered by `created_at` descending

### Requirement: Remote metadata handling

The `remote_meta` field SHALL store provider-specific data as a JSON string. The store layer SHALL treat it as opaque -- parsing is the responsibility of the service/sync layer.

#### Scenario: Storing remote metadata
- **WHEN** a synced task is created or updated with remote metadata
- **THEN** the `remote_meta` field SHALL contain a valid JSON string with provider-specific fields

#### Scenario: Local task has no remote metadata
- **WHEN** a local task is created
- **THEN** the `remote_meta` field SHALL be NULL

#### Scenario: Querying by provider
- **WHEN** the system needs to find all tasks from a specific provider
- **THEN** it SHALL query using the indexed `provider` column, not by parsing `remote_meta`

### Requirement: Store Cleanup

The store SHALL provide a `Close` method that closes the underlying database connection. The store MUST be safe to close even if no operations have been performed.

#### Scenario: Close an open store
- **WHEN** the `Close` method is called on an initialized store
- **THEN** the database connection SHALL be closed and subsequent operations SHALL return an error

### Requirement: Archive task in store

The store SHALL provide an `ArchiveTask(id)` method that sets `archived_at = datetime('now')` for the given task ID. The method SHALL return an error if the task does not exist.

#### Scenario: Archive a task
- **WHEN** `ArchiveTask` is called with a valid task ID
- **THEN** the task's `archived_at` SHALL be set to the current timestamp

#### Scenario: Archive nonexistent task
- **WHEN** `ArchiveTask` is called with an ID that does not exist
- **THEN** an error SHALL be returned

### Requirement: Bulk archive tasks by status

The store SHALL provide an `ArchiveTasksByStatus(status)` method that sets `archived_at = datetime('now')` for all non-archived tasks with the given status. The method SHALL return the count of affected rows.

#### Scenario: Bulk archive by status
- **WHEN** `ArchiveTasksByStatus("Done")` is called and there are 3 non-archived tasks with status "Done"
- **THEN** all 3 tasks SHALL have `archived_at` set and the method SHALL return 3

#### Scenario: Bulk archive with no matching tasks
- **WHEN** `ArchiveTasksByStatus("Done")` is called and no non-archived tasks have status "Done"
- **THEN** the method SHALL return 0

### Requirement: Task listing excludes archived

All store methods that list tasks (`ListTasksByStatus`, `ListTasksByStatusAndWorkspace`) SHALL include `AND archived_at IS NULL` in their WHERE clause.

#### Scenario: Archived tasks excluded from status listing
- **WHEN** a task is archived and `ListTasksByStatus` is called for that status
- **THEN** the archived task SHALL NOT appear in results

#### Scenario: Archived tasks excluded from workspace listing
- **WHEN** a task is archived and `ListTasksByStatusAndWorkspace` is called
- **THEN** the archived task SHALL NOT appear in results

### Requirement: Task search excludes archived

The store's task search query SHALL include `AND archived_at IS NULL` to exclude archived tasks from search results.

#### Scenario: Archived task not in search results
- **WHEN** a task is archived and a search query matches its title
- **THEN** the archived task SHALL NOT appear in search results

### Requirement: Check if task is archived

The store SHALL provide an `IsTaskArchived(id)` method or the `GetTask` method SHALL include the `archived_at` field, allowing callers to check archive status.

#### Scenario: Check archived status
- **WHEN** `GetTask` is called for an archived task
- **THEN** the returned task SHALL have a non-nil `archived_at` value
