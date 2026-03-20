## Context

Legato's data model is built around a `tickets` table with Jira-shaped columns. The `BoardService` reads tickets from this table and renders them as cards. The sync service pulls from Jira and upserts into `tickets`. There is no way to create tasks locally — every card must come from a provider.

This change introduces a provider-agnostic `tasks` table as the single source of truth. Local tasks live here with no provider. Synced tasks live here with `provider` + `remote_id` set. All board operations work on tasks uniformly.

## Goals / Non-Goals

**Goals:**
- Replace `tickets` table with a lean `tasks` table — core fields only, provider metadata in JSON
- Generate short random IDs for local tasks, allow user-specified IDs
- Create tasks from the TUI with an inline overlay
- Preserve all existing Jira sync functionality by adapting it to produce tasks
- Migrate existing ticket data to the new schema without data loss
- Keep the `BoardService` interface stable — callers (TUI, server) should need minimal changes

**Non-Goals:**
- Removing any Jira code — provider, client, ADF converter all stay
- Task editing beyond status/column moves (description editing is future)
- Multi-provider sync in a single board (one provider configured at a time for now)
- Task deletion from TUI (future — too destructive for v1)
- Subtasks or task relationships

## Decisions

### 1. Single `tasks` table with JSON metadata

**Decision:** One table with core fields + nullable `provider`, `remote_id`, and `remote_meta` (JSON TEXT).

```sql
CREATE TABLE tasks (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    description_md TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT '',
    priority TEXT NOT NULL DEFAULT '',
    sort_order INTEGER NOT NULL DEFAULT 0,
    provider TEXT,          -- NULL for local, 'jira', 'linear', etc.
    remote_id TEXT,         -- provider's ID (e.g. Jira issue key)
    remote_meta TEXT,       -- JSON: provider-specific fields
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

**`remote_meta` JSON** stores provider-specific fields that don't belong in the core schema:
```json
{
    "remote_status": "In Progress",
    "remote_updated_at": "2024-01-15T10:30:00Z",
    "issue_type": "Story",
    "assignee": "cameron",
    "labels": "backend,auth",
    "epic_key": "REX-100",
    "epic_name": "Auth Rewrite",
    "url": "https://jira.example.com/browse/REX-1234",
    "stale_at": null,
    "local_move_at": null,
    "remote_transition": "31"
}
```

**Rationale:** Keeps the core schema clean — only fields legato needs universally. Provider-specific fields are preserved in JSON for sync purposes without polluting the table. Adding a new provider means defining a new JSON shape, not adding columns.

**Alternatives considered:**
- **KV store table** (`task_meta` with key/value rows): More normalized but painful to query — JOINs for every read. JSON column is simpler and SQLite has JSON functions if needed.
- **Keeping wide table with NULLs**: What we have now, but worse with each new provider.

### 2. Short random IDs (nanoid-style)

**Decision:** Local tasks get 8-character lowercase alphanumeric IDs (e.g. `a3f2xk9p`). Users can optionally specify a custom ID at creation time. Synced tasks use the provider's ID format (e.g. `REX-1234`).

**Generation:** Use `crypto/rand` to generate 8 chars from `[a-z0-9]` (36^8 ≈ 2.8 trillion combinations). Collision check against DB before insert.

**Rationale:** Short enough to type/remember, long enough to avoid collisions at any reasonable scale. No prefix needed — provider IDs are visually distinct (uppercase + dash pattern vs lowercase alphanumeric). User-specified IDs allow workflows like `BUG-1` or `sprint-42`.

### 3. Migration strategy: rename + reshape

**Decision:** Migration `005_tasks.sql` does:
1. Create new `tasks` table
2. `INSERT INTO tasks SELECT ... FROM tickets` — map existing columns to new schema, pack remote fields into `remote_meta` JSON
3. Update `agent_sessions` to reference new table
4. Update `sync_log` to reference new table
5. Drop `tickets` table

**Rationale:** Single migration, atomic. Existing data preserved. The `id` column maps directly (Jira keys become the task ID with `provider='jira'` and `remote_id` set to the same value).

### 4. Store layer: Task struct + renamed methods

**Decision:** Replace `store.Ticket` with `store.Task`. Rename all CRUD methods (`CreateTicket` → `CreateTask`, etc.). Add `RemoteMeta` as a `string` field (raw JSON) on the struct.

The store layer does NOT parse `remote_meta` — it's opaque JSON. Parsing happens in the service/sync layer where provider-specific logic lives.

### 5. Service layer: BoardService stays stable

**Decision:** `BoardService` interface methods don't change signatures. `Card` and `CardDetail` types are updated to reflect task fields:
- `Card.Summary` → `Card.Title`
- `CardDetail` drops Jira-specific fields, adds `Provider` and `RemoteID`
- Provider-specific detail (assignee, labels, epic) available via `CardDetail.RemoteMeta` map

The TUI calls the same methods. Only internal field names change.

### 6. Sync adapter: Jira → Task conversion

**Decision:** The existing `JiraProviderAdapter` continues to implement `TicketProvider`, but the sync service now converts provider output into `store.Task` objects before upserting. Jira-specific fields are packed into `remote_meta` JSON.

The `TicketProvider` interface stays as-is — it returns provider-native data. A new conversion step in `SyncService.Sync()` transforms provider output into tasks.

### 7. Task creation overlay

**Decision:** Press `n` on the board to open a create-task overlay. Fields:
- **Title** (required, text input)
- **Column** (defaults to current column, single-letter shortcuts like move overlay)
- **Priority** (optional, cycle through: none/low/medium/high)

On submit: generate ID, insert task, refresh board, navigate cursor to new task.

The overlay follows the same pattern as move/search overlays — `overlayCreate` enum value, dedicated model in `internal/tui/overlay/`.

## Risks / Trade-offs

**[JSON column querying]** → SQLite has `json_extract()` but it's slower than indexed columns. For fields that need querying (e.g. "find all tasks from Jira"), `provider` and `remote_id` are real columns, not in JSON. JSON is only for display/sync metadata.

**[Migration on existing data]** → Users with real Jira data need a clean migration. Test with production-like data. The migration is reversible in principle (tasks → tickets) but we won't provide a down migration.

**[ID collisions]** → 8-char alphanumeric has 2.8T combinations. At 1000 tasks, collision probability is ~0.00000036%. Check-before-insert handles the theoretical case.

**[Breaking change for sync_log/agent_sessions]** → These reference `ticket_id`. Migration renames the column to `task_id`. Code references must update.

## Open Questions

- Should `remote_meta` be validated against a schema per provider, or just treated as opaque JSON? (Suggest: opaque for now, validate in sync adapter before storing)
- Should moving a synced task still attempt remote transition? (Suggest: yes, preserve existing push behavior via `remote_meta` fields)
