## Context

Board cards are currently 3 lines tall: key line (with provider icon, agent prefix, warning prefix crammed together), title line, and metadata line (priority + issue type). Agent activity transitions (working/waiting/idle) are tracked live in the `agent_sessions.activity` column but not persisted historically — when an agent transitions from "working" to "waiting", the previous state is overwritten with no record of duration.

The existing flow: Claude Code hooks → `legato agent state` CLI → `store.UpdateAgentActivity()` → IPC broadcast → TUI refresh. This already touches every layer, so adding interval tracking means inserting persistence at the store/service boundary without changing the CLI or IPC protocol.

## Goals / Non-Goals

**Goals:**
- Expand card height to ~5-6 lines with clear visual sections (identity, title, agent status, metadata/duration)
- Record state intervals in a new `state_intervals` table so duration in each activity state is queryable
- Display aggregated working/waiting durations on cards when an agent is or has been active
- Maintain backward compatibility — cards without agents render cleanly without duration info

**Non-Goals:**
- Tracking time spent in board columns (Backlog → Doing → Done lifecycle) — different concern, future work
- Configurable card height or density modes
- Historical analytics views or reports — just card-level display for now
- Idle time tracking — per user input, this matters less and adds noise to cards

## Decisions

### 1. Separate `state_intervals` table vs. extending `agent_sessions`

**Decision**: New `state_intervals` table with foreign key to `tasks.id`.

**Rationale**: State intervals are a many-to-one relationship with tasks (many intervals per task over time, across potentially multiple agent sessions). Embedding them in `agent_sessions` would require denormalization or complex JSON. A relational table is natural for SQLite and makes aggregation queries straightforward.

**Schema**:
```sql
CREATE TABLE state_intervals (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    state TEXT NOT NULL,         -- 'working', 'waiting'
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    ended_at DATETIME,           -- NULL = currently active
    CONSTRAINT valid_state CHECK (state IN ('working', 'waiting'))
);
CREATE INDEX idx_state_intervals_task ON state_intervals(task_id);
```

**Alternatives considered**: (a) JSON array in `agent_sessions` — poor queryability; (b) Adding columns to `agent_sessions` for cumulative times — loses granularity of individual intervals.

### 2. Interval lifecycle: close-then-open on transition

**Decision**: When `UpdateAgentActivity` is called with a new state, the store closes any open interval for that task (sets `ended_at = now`) and opens a new one (if the new state is 'working' or 'waiting'). When activity is cleared (`""`), only close — don't open a new interval.

**Rationale**: This is simple, idempotent, and handles restart/crash scenarios gracefully. If an interval is left open (no `ended_at`), the next transition or a reconciliation pass closes it.

### 3. Card layout expansion

**Decision**: Expand cards to 5 lines for agent-active cards, 4 lines for cards without agents.

```
Line 1: [provider icon] [key]                    ← identity
Line 2: [title, truncated]                       ← content
Line 3: [agent state icon] [STATE] [duration]    ← agent row (only if agent active/has history)
Line 4: [priority badge] · [issue type]          ← metadata
```

For cards with no agent and no duration history, line 3 is omitted (stays at current ~3 lines + some padding). The card height adapts based on whether there's agent data to show.

**Rationale**: Fixed 5-line height for all cards wastes space on boards with few agents. Adaptive height keeps the board compact when agent features aren't used while giving full context when they are.

### 4. Duration display format

**Decision**: Show cumulative working and waiting time on the agent status line, formatted as human-readable durations: `2h 15m working · 45m waiting`.

**Rationale**: Per-interval breakdown is too detailed for a card — aggregated totals give the useful signal. Detailed breakdown can live in the detail view later.

### 5. Duration aggregation: query-time vs. cached

**Decision**: Compute durations at query time via SQL aggregation (`SUM` of interval durations). Cache in `CardData` during data load, not on every render.

**Rationale**: SQLite handles this aggregation efficiently for the expected data volume (hundreds of intervals at most per task). Computing during the existing `DataLoadedMsg` flow avoids adding a cache invalidation concern. The data load already hits SQLite for cards, columns, agents — one more query per board refresh is negligible.

### 6. Store layer placement

**Decision**: Interval CRUD lives in `internal/engine/store/` as new methods on `Store`. The close-then-open logic lives in the store layer since it's a data integrity concern (ensuring no overlapping open intervals).

**Rationale**: Keeps the service layer thin — it just calls `store.RecordStateTransition(taskID, newState)` and `store.GetStateDurations(taskID)`. The store ensures data consistency.

## Risks / Trade-offs

**[Risk] Open intervals on crash** → Mitigation: `AgentService.ReconcileSessions()` already runs on startup to clean up stale tmux sessions. Extend it to close any open intervals for tasks whose agents are no longer running.

**[Risk] Clock skew in duration calculations** → Mitigation: All timestamps use SQLite's `datetime('now')` which is server-local. Since legato is a local-only tool, clock skew is not a concern.

**[Risk] Adaptive card height causes visual jitter** → Mitigation: Within a single column, all cards will use the same height (the max needed for that column's cards). This prevents cards jumping when agents start/stop. Columns with no agent-active cards stay compact.

**[Trade-off] No idle time tracking** → User explicitly deprioritized this. Can be added later by recording intervals for the `""` state if needed.
