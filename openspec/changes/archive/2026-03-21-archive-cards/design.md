## Context

The Done column accumulates completed tasks indefinitely. Over time this makes the board noisy and harder to navigate. Tasks need to remain in the database for history (duration tracking, sync log references) but should be hideable from the active board view.

The existing `tasks` table has no concept of archival. All listing queries (by status, by workspace) return every task regardless of age or completion.

## Goals / Non-Goals

**Goals:**
- Allow users to archive done cards, removing them from the board
- Support both bulk archive (all done cards) and single-card archive
- Archived tasks remain in the database — no data loss
- Minimal schema change: a single nullable timestamp column

**Non-Goals:**
- Unarchive / restore functionality (can be added later)
- Viewing archived cards in the TUI (future feature)
- Auto-archiving based on age or rules
- Archiving cards in non-Done columns

## Decisions

### 1. Archive representation: `archived_at` nullable timestamp on `tasks`

Add `archived_at DATETIME` (nullable, default NULL) to the `tasks` table. NULL = active, non-NULL = archived with timestamp.

**Why over a boolean flag**: The timestamp preserves when the archive happened, useful for future features (e.g., "archived 3 days ago") at zero extra cost. The WHERE clause is identical: `WHERE archived_at IS NULL`.

**Alternative considered**: Separate `archived_tasks` table. Rejected — moves rows between tables, breaks FK references from `agent_sessions`, `sync_log`, and `state_intervals`.

### 2. Filter at the store layer, not service layer

All `ListTasksByStatus*` and `ListTasksByStatusAndWorkspace` queries add `AND archived_at IS NULL`. This ensures no caller can accidentally show archived tasks.

**Why**: Single enforcement point. The service layer doesn't need to remember to filter.

### 3. Keybinding: `X` from board view for bulk archive, confirmation required

`X` (shift-x) from the board triggers "Archive all done cards". This uses a confirmation overlay (reusing the delete overlay pattern) showing the count of cards to archive.

**Why `X`**: `a` is unused but too close to common navigation. `X` suggests a destructive-ish action (like `D` for delete) and is easy to reach.

**Alternative considered**: Per-card archive via context menu. Deferred — bulk archive of done cards covers the primary use case. Individual card archive can be added to the move overlay later.

### 4. New migration `009_archive.sql`

Single migration adding the column:
```sql
ALTER TABLE tasks ADD COLUMN archived_at DATETIME;
```
No index needed initially — the NULL filter on `archived_at` is fast enough for typical board sizes (hundreds of tasks, not millions).

## Risks / Trade-offs

- **[Synced tasks]** → Archiving a synced task hides it locally but doesn't affect the remote provider. If the remote ticket is re-opened or updated, the next sync pull will see it. Decision: skip archived tasks during sync upsert — once archived locally, sync won't resurface them. If the user needs them back, a future unarchive feature handles this.
- **[No undo]** → There's no unarchive in v1. Mitigation: the confirmation overlay shows exactly how many cards will be archived, and the data is never deleted. A `legato task unarchive` CLI command or TUI feature can be added later.
- **[Query performance]** → Adding `AND archived_at IS NULL` to every listing query is minimal overhead. If the table grows very large, an index on `archived_at` can be added later.
