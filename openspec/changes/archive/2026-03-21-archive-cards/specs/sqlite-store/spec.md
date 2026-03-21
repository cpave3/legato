## MODIFIED Requirements

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

## ADDED Requirements

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
