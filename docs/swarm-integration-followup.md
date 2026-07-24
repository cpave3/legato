# Swarm integration follow-up

The swarm-conductor change ([commit `b1a518f`](https://github.com/cpave3/legato/commit/b1a518f)) shipped working internals — conductor lifecycle, plan approval, send-keys IPC, agent grouping in TUI/web — but the swarm sits a bit apart from the rest of legato. This document tracks the integration work that would close that gap.

Items are grouped by leverage. Within each group the order is roughly "do these first."

## Highest leverage

### 1. ~~Web UI parity for swarm controls~~ ✅ *Done in `swarm-web-parity`.*

Web now supports starting a swarm, plan approval/rejection/dismissal via modal, per-worker messaging, closing workers, and finishing the swarm. See `docs/ai/web-ui.md` § Swarm controls.

### 2. Reporting view swarm metrics

The reporting TUI screen currently aggregates agent durations + workspace breakdown. Adding a "Swarms" section is a natural extension:

- Swarm count, status breakdown (active / completed / cancelled).
- Average sub-tasks per swarm; distribution of swarm sizes.
- Time-in-state per role label across all swarms (how much time `backend` workers spend `working` vs `waiting`).
- Throughput comparison: tasks completed via swarm vs. solo agent.
- Conductor-time vs worker-time ratio — sanity check that conductors aren't burning more tokens than the workers they coordinate.

Data source: the `swarm_subtasks` and `state_intervals` tables already have what's needed. Aggregation queries belong in `internal/service/report.go`.

### 3. Detail view: live swarm event log

Detail view currently renders a static sub-task list. The `swarm_events` table already records every progress / question / built / died / scope_warning event with timestamps and worker titles. Surfacing the recent N events under the sub-task list would give a real-time audit trail — useful both during the swarm ("what's happening?") and after ("what did the swarm actually do?").

Implementation: `SwarmService.RecentEvents(parentID, limit) ([]SwarmEvent, error)`, called from `Detail.SetEvents`, rendered as a list with timestamp + worker + payload. Tail the events on `EventSwarmChanged` for live updates.

### 4. Status-bar pending-plan indicator

When a conductor has called `propose-plan` and the user hasn't yet verdict'd it, surface that as a status-bar nudge ("⚠ plan pending: parent-id") or a board-level badge on the parent card. Right now if the user `esc`s the overlay or wasn't on the board when the IPC fired, it's easy to miss.

Implementation: pending-plan state lives in `App` already (the IPC payload arrives even if the overlay isn't opened). Render it in the status bar; clicking / pressing a binding re-opens the overlay.

## Medium leverage

### 5. PR-tracking aggregation per swarm

Each worker is a task and already tracks its own branch/PR via the existing PR-tracking infrastructure. There's no rolled-up view of "this swarm's PRs." Under the parent task's detail view, render: "5 PRs from this swarm, 3 merged, 2 open with CI status." Lets you do a clean post-swarm review without bouncing between sub-tasks.

Data: just `SELECT pr_meta FROM tasks WHERE id IN (SELECT id FROM swarm_subtasks WHERE parent_task_id = ?)`.

### 6. Sync (Jira) integration

When a Jira-synced task has a swarm, optionally:

- On `swarm finish`, post the summary as a Jira comment (one-liner per role + the conductor's natural-language summary).
- On worker `built`, optionally a "sub-task done: <title>" comment.

Makes the swarm visible to teammates who don't run legato. Trade-off is comment noise; should be opt-in via `swarm.sync_to_jira: true` config (default false).

### 7. Tmux status line role awareness

Each spawned agent currently shows the global `2 working · 1 waiting · 0 idle` summary. For swarm participants, showing swarm-local counts (`swarm: 2/3 working · this conductor: idle`) would be more useful — agents could tell whether they're the laggard. Belongs in `legato agent summary` with a `--swarm <parent-id>` flag.

### 8. Ephemeral task cleanup

Sub-tasks need backing rows in `tasks` (FK reasons: `agent_sessions.task_id` references them). Those rows are flagged `ephemeral=1` but never deleted. Over time the DB accumulates dead sub-task rows.

Fix options:
- Sweep on `swarm finish` — delete ephemeral task rows for sub-tasks transitioning to `done` / `cancelled`.
- Add `legato gc` command that cleans up orphaned ephemerals across all swarms.
- Cascade on swarm_subtasks deletion (changes the schema).

Sweep-on-finish is the simplest and most surgical.

### 9. Search across swarm content

The search overlay currently hits tasks/cards. Extend to:
- Find sub-tasks by title (across all swarms or scoped to the current parent).
- Find events in the log ("when did the backend worker say 'API is done'?").

Useful for multi-day swarms where the user is reconstructing context.

## Lower leverage / can wait

### 10. Workspace pre-fill plumbing

`workspaces[].path` is in config and the swarm-init overlay accepts a `suggested` dir, but `main.go` still passes `os.Getwd()` rather than reading the selected card's workspace path. One-screen wiring fix; already on the swarm-conductor iteration backlog (`tasks.md` item 3).

### 11. First-run / help discoverability

The help overlay covers swarm bindings, but there's no first-run wizard mention. Worth adding a one-line "Press S on a task to spawn a swarm conductor" hint in the status bar or empty-state on the agents view, at least until users have spawned their first swarm.

### 12. CLI shortcuts

`legato task swarm-start <id> <dir>` and `legato task swarm-status <id>` are minor ergonomics; current verbs (`legato swarm ...`) work fine but require remembering they live under `swarm` not `task`. Aliases under `task` would help muscle memory.

## Real gotchas worth addressing

These aren't new features but issues with the current state:

### Conductor exclusivity vs single-task agents

While a conductor is alive on `legato-<parent-id>`, the user can't spawn a regular single-task agent for the same card — same tmux session name. `StartSwarm` refuses double-spawn cleanly, but the agent view's normal `spawn` action would also fail. Confirm error paths surface clearly to the user (e.g. "task already has a conductor; kill it to spawn a regular agent").

### `.legato/` in working dirs

This is now resolved. Swarm runtime files live in `~/.legato/` outside the working tree, and `SwarmService.Finish` cleans up the per-agent directories (`~/.legato/agents/<task-id>/`) and matching plan files (`~/.legato/plans/<parent>-*.yaml`) on completion.

### Worker `legato` binary on $PATH

If a swarm worker runs in a sandbox or in an environment where `legato` isn't on `$PATH`, `legato swarm progress` etc. fail silently. Hook scripts already handle this by embedding the absolute path at install time, but swarm CLI calls *from* the worker have no such protection.

Fix: pass `LEGATO_BIN` env var pointing at the binary (`os.Executable()` at startup), document that workers should prefer `$LEGATO_BIN swarm progress` over `legato swarm progress`. Update `worker.md` and the chimera mode files accordingly.

## Suggested first three

If picking only a few to do first, I'd start with the items where the current state is observably broken or incomplete from the user's perspective:

1. **Web parity for plan approval (#1)** — the only way to verdict a plan today is via the TUI overlay, which means a phone user can watch a swarm but can't approve its plan. That's the biggest UX gap.
2. **Detail-view event log (#3)** — the event data exists; surfacing it turns the detail view from a static sub-task list into a real audit trail.
3. **Ephemeral cleanup (#8)** — the only one that's actively wrong (DB grows without bound). Small change, important hygiene.

The others are quality-of-life improvements to a working system.
