## ADDED Requirements

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

#### Scenario: Fresh database receives initial migration
- **WHEN** the store opens a database with `user_version = 0`
- **THEN** the initial migration SHALL create the `tickets`, `column_mappings`, and `sync_log` tables with all columns and indexes as defined in the schema, and `user_version` SHALL be set to 1

#### Scenario: Already-migrated database is opened
- **WHEN** the store opens a database whose `user_version` matches the latest migration version
- **THEN** no migrations SHALL be applied and existing data SHALL remain intact

#### Scenario: Migration failure rolls back
- **WHEN** a migration fails partway through execution
- **THEN** the transaction SHALL be rolled back, `user_version` SHALL remain at its previous value, and the store SHALL return an error

### Requirement: Tickets Table Schema

The initial migration SHALL create a `tickets` table with the following columns: `id` (TEXT PRIMARY KEY, Jira issue key), `summary` (TEXT NOT NULL), `description` (TEXT), `description_md` (TEXT), `status` (TEXT NOT NULL, local kanban column), `jira_status` (TEXT NOT NULL), `priority` (TEXT), `issue_type` (TEXT), `assignee` (TEXT), `labels` (TEXT, JSON array), `epic_key` (TEXT), `epic_name` (TEXT), `url` (TEXT), `created_at` (TEXT NOT NULL, ISO 8601), `updated_at` (TEXT NOT NULL, ISO 8601), `jira_updated_at` (TEXT NOT NULL), `sort_order` (INTEGER DEFAULT 0). An index `idx_tickets_status` SHALL be created on the `status` column. An index `idx_tickets_updated` SHALL be created on the `jira_updated_at` column.

#### Scenario: Tickets table exists after migration
- **WHEN** the initial migration has been applied
- **THEN** inserting a row with all required fields into the `tickets` table SHALL succeed, and querying it back SHALL return matching values

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

### Requirement: Ticket CRUD Operations

The store SHALL provide functions for creating, reading, updating, and deleting tickets. All operations MUST accept a `context.Context` for cancellation. The store SHALL use `sqlx` named parameters for insert and update operations.

#### Scenario: Create a ticket
- **WHEN** a ticket struct with a unique `id` is passed to the create function
- **THEN** the ticket SHALL be persisted in the `tickets` table with all fields stored correctly

#### Scenario: Create a duplicate ticket
- **WHEN** a ticket struct with an `id` that already exists is passed to the create function
- **THEN** the function SHALL return an error indicating the ticket already exists

#### Scenario: Get a ticket by ID
- **WHEN** a valid ticket `id` is passed to the get function
- **THEN** the function SHALL return the ticket struct with all fields populated from the database

#### Scenario: Get a non-existent ticket
- **WHEN** a ticket `id` that does not exist is passed to the get function
- **THEN** the function SHALL return a not-found error

#### Scenario: List tickets by status
- **WHEN** a status string is passed to the list function
- **THEN** the function SHALL return all tickets with that status, ordered by `sort_order` ascending

#### Scenario: Update a ticket
- **WHEN** a ticket struct with an existing `id` and modified fields is passed to the update function
- **THEN** the modified fields SHALL be persisted and the `updated_at` timestamp SHALL reflect the update time

#### Scenario: Delete a ticket
- **WHEN** a valid ticket `id` is passed to the delete function
- **THEN** the ticket SHALL be removed from the `tickets` table

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

### Requirement: Store Cleanup

The store SHALL provide a `Close` method that closes the underlying database connection. The store MUST be safe to close even if no operations have been performed.

#### Scenario: Close an open store
- **WHEN** the `Close` method is called on an initialized store
- **THEN** the database connection SHALL be closed and subsequent operations SHALL return an error
