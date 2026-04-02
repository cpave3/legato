## Context

Currently, every agent session in Legato requires a pre-existing task (either local or Jira-synced). The `SpawnAgent` flow starts from a selected board card and uses its task ID for the tmux session name, DB tracking, and state interval recording. Users who want ad-hoc terminal sessions must create throwaway tasks on the board, cluttering the kanban view.

The `agent_sessions` table has no FK constraint on `task_id`, but `state_intervals` does have a FK to `tasks(id)`. This means we need a real task row for ephemeral sessions to participate in state tracking.

## Goals / Non-Goals

**Goals:**
- Allow spawning managed tmux sessions without a pre-existing board task
- Let users title ephemeral sessions for disambiguation
- Full agent lifecycle support (spawn, attach, kill, capture, state tracking)
- Keep ephemeral sessions out of the kanban board view

**Non-Goals:**
- Converting ephemeral sessions into real tasks (future feature)
- Ephemeral session persistence across `legato` restarts beyond normal reconciliation
- Any changes to the Jira sync flow or provider architecture

## Decisions

### 1. Ephemeral tasks as real task rows with a boolean flag

**Decision**: Add an `ephemeral` boolean column to the `tasks` table. Ephemeral tasks are real rows that satisfy FK constraints but are filtered from board queries.

**Alternatives considered**:
- *Synthetic IDs without task rows*: Breaks `state_intervals` FK. Would require either removing the FK or special-casing interval recording. Fragile.
- *Dedicated `ephemeral_tasks` table*: Unnecessary complexity. The existing `tasks` table already handles the data model; a flag is simpler.
- *Use a sentinel column value (e.g. status="ephemeral")*: Overloads the status field, risks interaction with sync logic and column mapping.

**Rationale**: A boolean flag is the minimal change. Board queries already filter by status and workspace — adding `AND ephemeral = 0` is trivial. The task row gives us title storage, state tracking, and future extensibility (e.g., promoting to a real task) for free.

### 2. Spawn flow from agent view with title input overlay

**Decision**: Pressing `s` in the agent view opens a title input overlay (reusing the existing text input pattern from `TitleEditOverlay`). On submit, the system creates an ephemeral task row and spawns the agent.

**Alternatives considered**:
- *Spawn immediately with auto-generated name*: Users explicitly want to set titles for disambiguation.
- *Reuse the full create overlay*: Too heavy — ephemeral sessions don't need column, priority, workspace, or description fields.

**Rationale**: A lightweight single-field overlay keeps the flow fast. The title is the only meaningful metadata for an ephemeral session.

### 3. Ephemeral task defaults

**Decision**: Ephemeral tasks get:
- ID: standard 8-char alphanumeric (same as local tasks)
- Status: first column name (e.g., "Backlog") — doesn't matter since they're filtered from board, but satisfies NOT NULL
- Priority: empty
- Workspace: none
- Provider: NULL (local)

### 4. Board filtering

**Decision**: All board listing queries (`ListTasksByStatus`, `ListTasksByStatusAndWorkspace`) add `AND ephemeral = 0`. The `BoardService` layer doesn't need changes beyond the store query filter.

### 5. Migration

**Decision**: New migration `011_ephemeral.sql` adds `ephemeral INTEGER NOT NULL DEFAULT 0` to tasks. All existing tasks default to non-ephemeral.

## Risks / Trade-offs

- **[Risk] Ephemeral tasks accumulate over time** → Ephemeral tasks with dead agents could be auto-cleaned on startup reconciliation, or archived. For v1, they persist (same as dead agent sessions today). Users can manually delete via `d` from agent view if needed.
- **[Risk] Status field semantics unclear for ephemeral tasks** → Using the first column name is a pragmatic default. Since ephemeral tasks never appear on the board, the value is inert.
- **[Trade-off] No promotion to real task** → Explicitly a non-goal for now. The flag makes future promotion trivial (just flip `ephemeral = 0`).
