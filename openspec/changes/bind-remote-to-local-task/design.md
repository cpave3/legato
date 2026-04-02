## Context

Local tasks use 8-char random alphanumeric IDs (e.g., `a1b2c3d4`). Imported remote tasks use the provider's key as the task ID (e.g., `REX-123`). The task ID is the primary key and is referenced by `agent_sessions` (soft FK), `state_intervals` (hard FK with CASCADE), `sync_log` (soft FK), tmux session names (`legato-<id>`), and PR tracking.

Currently there is no way to associate a remote ticket with an existing local task. The only path is `ImportRemoteTask`, which creates a new task row using the remote ticket's key as the ID.

## Goals / Non-Goals

**Goals:**
- Allow binding a remote ticket to an existing local task, preserving all history (agent sessions, durations, PR links)
- Bound tasks participate in pull/push sync like normally-imported tasks
- Zero disruption to running agents — no ID change, no tmux session rename

**Non-Goals:**
- Unbinding (removing remote association) — not in scope for v1
- Merging two existing tasks (one local, one already-imported remote)
- Changing the task's internal ID to match the remote key
- Supporting binding to multiple remote tickets

## Decisions

### Decision 1: Keep local ID, store remote ID separately

The task retains its original local ID (`a1b2c3d4`) as the primary key. The remote ticket key is stored in `remote_id` (which already exists as a nullable column). The `provider` and `remote_meta` fields are populated on bind.

**Why not change the ID to match remote?** Changing the PK would require updating `agent_sessions.task_id`, renaming the tmux session (`legato-<oldid>` → `legato-<newid>`), migrating `state_intervals` and `sync_log`, and updating any IPC references. This is fragile and disruptive to running agents.

**Why not add a separate linking table?** The existing `provider`/`remote_id`/`remote_meta` columns already model this relationship. A separate table adds complexity for no benefit since we only support one remote ticket per task.

**Trade-off**: Cards and the detail view will show the remote ID (e.g., `REX-123`) as the display key, but internal references (agent sessions, tmux, CLI commands) use the local ID. Users targeting a bound task via CLI can use either ID — store lookups fall back to `remote_id` if `id` doesn't match.

### Decision 2: Sync lookup by remote_id, not just task id

Currently `pullSync` matches incoming remote tickets to local tasks by `id`. After binding, `id` won't match the remote key. Pull sync must also check `remote_id` to find bound tasks.

Implementation: `store.GetTaskByRemoteID(provider, remoteID)` query. `pullSync` checks this after the existing `GetTask(rt.ID)` miss. `ImportRemoteTask` also checks this to prevent double-binding.

### Decision 3: Detail view `i` triggers bind overlay for local tasks

The `i` key on the detail view opens the same search overlay used for import on the board, but instead of creating a new task, it calls `SyncService.BindRemoteTicket(taskID, remoteTicketID)`. Only available for local tasks (provider is nil).

The board `i` (import) continues to work as before — creates a new task. The detail view `i` (bind) associates a remote ticket with the current task.

### Decision 4: Store method is a targeted update

`store.BindRemoteTicket(ctx, taskID, provider, remoteID, remoteMeta)` updates only the `provider`, `remote_id`, and `remote_meta` columns. It rejects if the task already has a provider set (already bound). This is not a general-purpose update — it's a one-way transition from local to remote-tracked.

## Risks / Trade-offs

**[Risk] Display ID confusion** → Users see `REX-123` on the card but need to use `a1b2c3d4` for CLI commands targeting the task. Mitigation: CLI commands accept either ID (lookup falls back to `remote_id`). Agent sessions and tmux sessions continue using the local ID transparently.

**[Risk] Duplicate binding** → User tries to bind a remote ticket that's already imported as a separate task. Mitigation: `BindRemoteTicket` checks `GetTaskByRemoteID` first and rejects with a clear error ("ticket already tracked as task X").

**[Risk] Sync conflict on first pull after bind** → The remote ticket's status may differ from the local task's column. Mitigation: Same conflict resolution as normal sync — local wins within 5-minute window, then remote state accepted. The bind operation sets `local_move_at` in `remote_meta` to give the user the 5-minute grace period.

**[Risk] Title/description divergence** → Local task may have a different title/description than the remote ticket. Mitigation: On bind, update title and description from remote (same as import). User can see the diff in the bind confirmation overlay before accepting.
