## Why

Legato's data model is currently coupled to Jira — the `tickets` table has Jira-shaped fields (`remote_status`, `remote_updated_at`, `issue_type`, `epic_key`, etc.) and the board only shows data pulled from a provider. Users cannot create local tasks without a backing ticket system. This limits legato to being a Jira viewer rather than a standalone task board that *optionally* syncs with external systems.

Decoupling the data model makes legato useful on its own — a developer can spin it up, create tasks, and manage work without configuring any provider. External providers become an enhancement, not a prerequisite.

## What Changes

- **New `tasks` table** replacing `tickets` — lean core schema (id, title, description_md, status, priority, sort_order, created_at, updated_at) with nullable `provider` and `remote_id` fields for synced tasks. Provider-specific metadata stored in a JSON `remote_meta` column.
- **Migration** from existing `tickets` table to new `tasks` table, preserving all data
- **Short random IDs** (nanoid-style) for locally created tasks, with optional user-specified IDs. Synced tasks use provider-format IDs (e.g. `REX-1234` from Jira).
- **Task creation from TUI** — press `n` on the board to create a new task via inline form (title, column, priority)
- **Updated service layer** — `BoardService` operates on tasks generically; sync providers produce tasks with `provider`/`remote_id` set
- **Updated store layer** — CRUD operations renamed from ticket-centric to task-centric
- **Provider sync adapter** — Jira sync converts Jira issues into legato tasks with `provider="jira"` and `remote_id` set, stores Jira-specific fields in `remote_meta` JSON

## Capabilities

### New Capabilities
- `local-task-management`: CRUD for local tasks — create, read, update status/priority, delete. Core schema, ID generation, and store operations.
- `task-creation-overlay`: TUI overlay for creating new tasks inline from the board view.

### Modified Capabilities
- `sqlite-store`: Migrate from `tickets` to `tasks` table with new schema. Rename all CRUD methods.
- `board-service`: Operate on tasks instead of tickets. `Card`/`CardDetail` types reflect task schema.
- `jira-sync`: Sync adapter converts Jira issues to legato tasks with `provider`/`remote_id` link. Jira-specific fields go into `remote_meta`.
- `kanban-board`: Add `n` keybinding to create task. Cards render task data (title instead of summary, etc.).

## Impact

- **Database**: New migration replacing `tickets` with `tasks`. Data migration for existing users.
- **Store layer**: All ticket CRUD methods renamed/reshaped. `Ticket` struct → `Task` struct.
- **Service layer**: `Card`/`CardDetail` updated. `BoardService` methods unchanged in signature but operate on tasks.
- **Sync service**: Provider adapter bridges external issues → legato tasks.
- **TUI**: New create-task overlay. Minor card rendering updates.
- **Agent sessions**: `ticket_id` FK updated to reference `tasks.id` (column rename).
- **No Jira code removed** — Jira provider, client, ADF converter all preserved. Sync flow still works, just targets the new task table.
