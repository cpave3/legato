## Why

Legato today spawns one tmux-backed agent per task — a single Claude/OpenCode session works the whole ticket end-to-end. For larger tasks, users want to fan out work across multiple agents that operate in parallel without stepping on each other (one builds the API, one builds the UI, one reviews). Doing this manually requires hand-decomposing the task, manually scoping each agent, and praying for no merge conflicts. We can do better: legato already owns task state, agent state, tmux orchestration, and IPC — adding role-based sub-agents with file-ownership scoping is mostly composition over existing infrastructure.

## What Changes

- New "swarm" task type: a parent task can be decomposed into sub-tasks, each owned by a single agent with an explicit file-ownership scope (glob patterns).
- New agent roles tagged on `agent_sessions`: `coordinator`, `builder`, `scout`, `reviewer`. Default for non-swarm sessions is empty (existing behavior).
- File-ownership manifest stored in SQLite — sub-task spawn refuses to start if its scope overlaps an active sibling's scope. Sequencing is automatic (overlapping sub-tasks queue behind earlier ones).
- Coordination surface: a SQL-view-backed JSON snapshot of the swarm state (parent + sub-tasks + owners + scopes + status) exposed via the existing CLI/IPC surface so agents can self-orient.
- Review gate: sub-tasks transition to a `review` state after the builder marks done, which auto-spawns a reviewer agent on the same scope. Reviewer approval moves the sub-task to `done`; reject moves it back to `building` with notes.
- Role-specific system prompt injection via the existing `AIToolAdapter` — adapters gain an optional `RoleSystemPrompt(role)` capability so each agent boots with its role's prompt.
- TUI: swarm overlay (`s` key) for decomposing a task into sub-tasks; swarm view in detail panel showing the sub-task graph with status; coordination surface visible in agent split view.
- CLI: `legato swarm decompose <task-id>`, `legato swarm status <task-id>`, `legato swarm review <subtask-id> --approve|--reject`.

## Capabilities

### New Capabilities

- `swarm-orchestration`: parent/sub-task data model, swarm lifecycle (decompose → build → review → done), role tagging on agent sessions, role-specific prompt injection, coordination surface.
- `file-ownership`: per-sub-task file scope as glob patterns, overlap detection at spawn time, automatic sequencing when scopes conflict.
- `swarm-overlay`: TUI overlay for decomposing tasks into sub-tasks, picking roles, assigning scopes; swarm graph view in task detail.

### Modified Capabilities

- `agent-session`: add `role` and `parent_task_id` columns to `agent_sessions`; spawn refuses on file-scope conflict with active siblings.
- `ai-tool-adapter`: optional `RoleSystemPrompt(role) string` method on adapter interface; tmux env vars include `LEGATO_AGENT_ROLE` and `LEGATO_PARENT_TASK_ID`.
- `legato-cli`: `swarm` subcommand group (`decompose`, `status`, `review`, `assign`).

## Impact

- **New tables**: `swarm_subtasks` (id, parent_task_id, title, scope_globs, role, status, builder_agent_id, reviewer_agent_id, created_at, completed_at), plus migration `011_swarm.sql`.
- **Modified**: `agent_sessions` table — `role TEXT`, `parent_task_id TEXT NULL` columns (migration `012_agent_role.sql`).
- **New code**:
  - `internal/engine/swarm/` — sub-task store, scope-overlap detection, lifecycle state machine.
  - `internal/service/swarm.go` — `SwarmService` orchestrating decomposition, builder/reviewer spawn, review gate transitions.
  - `internal/tui/overlay/swarm.go` — decomposition overlay.
  - `internal/cli/swarm.go` — CLI subcommand handlers.
- **Modified**:
  - `internal/service/agent.go` — `SpawnAgent` accepts optional role + parent_task_id, refuses on scope conflict.
  - `internal/engine/hooks/{claude_code,opencode,chimera}.go` — implement `RoleSystemPrompt` (returns built-in role prompts; can be overridden via config).
  - `internal/tui/board/` — render swarm parent cards distinctly (small badge with sub-task count + role legend).
  - `internal/tui/detail/` — swarm graph section in detail header for parent tasks.
- **No new third-party deps** — uses existing SQLite/tmux/IPC infrastructure.
- **No web UI changes in v1** — surface in TUI only; web UI can follow in a later change.
- **Compatibility**: Non-swarm tasks keep existing behavior (role = empty). Existing agent sessions continue to work without modification.
