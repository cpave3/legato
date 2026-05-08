## ADDED Requirements

### Requirement: Swarm sub-task data model

The system SHALL persist swarm sub-tasks in a `swarm_subtasks` table where each row represents a unit of work owned by a single agent and parented to an existing task.

#### Scenario: Sub-task schema

- **WHEN** the database is migrated
- **THEN** a `swarm_subtasks` table SHALL exist with columns: `id` (TEXT PK), `parent_task_id` (TEXT FK to tasks), `title` (TEXT), `description` (TEXT), `scope_globs` (TEXT, JSON array of globs), `role` (TEXT, one of `coordinator|builder|scout|reviewer`), `status` (TEXT, one of `queued|building|review|done|rejected`), `builder_agent_id` (INTEGER NULL FK), `reviewer_agent_id` (INTEGER NULL FK), `created_at` (DATETIME), `started_at` (DATETIME NULL), `completed_at` (DATETIME NULL)

#### Scenario: Parent task is queryable as a swarm

- **WHEN** any sub-task exists for `parent_task_id`
- **THEN** the parent task SHALL be considered a swarm parent and the system SHALL return its sub-task list via `SwarmService.ListSubtasks(parentID)`

### Requirement: Swarm decomposition

The `SwarmService` SHALL provide a `Decompose(parentTaskID, subtasks []SubtaskSpec) error` method that creates sub-tasks for an existing task in a single transaction.

#### Scenario: Decompose a task into multiple sub-tasks

- **WHEN** `Decompose("abc12345", [{title:"API", scope:["api/**"], role:"builder"}, {title:"UI", scope:["web/**"], role:"builder"}])` is called
- **THEN** two `swarm_subtasks` rows SHALL be inserted with status `queued` and the parent task SHALL be flagged as a swarm

#### Scenario: Decompose with overlapping scopes

- **WHEN** decomposition includes two sub-tasks whose `scope_globs` resolve to overlapping file sets
- **THEN** the operation SHALL succeed but the system SHALL serialize their execution (later sub-task waits for earlier one to leave `building` state)

#### Scenario: Decompose a remote task

- **WHEN** `Decompose` is called for a task with a non-null `provider`
- **THEN** the operation SHALL succeed (swarm is a local construct; the parent task's remote tracking is unaffected)

### Requirement: Swarm lifecycle state machine

A swarm sub-task SHALL transition through states `queued → building → review → done` (or `→ rejected`) and the system SHALL enforce valid transitions.

#### Scenario: Builder marks sub-task done

- **WHEN** a sub-task in `building` state has its agent terminate normally OR `SwarmService.MarkBuilt(subtaskID)` is called
- **THEN** the sub-task SHALL transition to `review` and a reviewer agent SHALL be auto-spawned for the same scope

#### Scenario: Reviewer approves

- **WHEN** `SwarmService.Review(subtaskID, approve=true, notes="")` is called
- **THEN** the sub-task SHALL transition to `done`, `completed_at` SHALL be set, and the reviewer agent SHALL be terminated

#### Scenario: Reviewer rejects

- **WHEN** `SwarmService.Review(subtaskID, approve=false, notes="missing tests")` is called
- **THEN** the sub-task SHALL transition back to `building`, the rejection note SHALL be appended to the sub-task description, and the original builder agent SHALL be respawned (or kept if still alive)

#### Scenario: Invalid transition

- **WHEN** `SwarmService.Review` is called on a sub-task in `queued` state
- **THEN** the call SHALL return an error indicating the sub-task is not awaiting review

### Requirement: Coordination surface

The system SHALL expose a JSON snapshot of swarm state suitable for agents to consume — listing parent task, all sub-tasks, owners, scopes, and statuses.

#### Scenario: Fetching the coordination surface

- **WHEN** `SwarmService.Snapshot(parentTaskID)` is called
- **THEN** it SHALL return a JSON document containing `{parent: {id, title, status}, subtasks: [{id, title, role, scope_globs, status, agent_id, started_at}]}`

#### Scenario: Snapshot via CLI

- **WHEN** `legato swarm status <parent-id>` is executed
- **THEN** it SHALL print the JSON snapshot to stdout for consumption by external tools or agent prompts

### Requirement: Role-specific system prompt injection

When spawning a swarm agent, the system SHALL inject a role-specific system prompt sourced from the configured `AIToolAdapter`'s `RoleSystemPrompt(role)` method.

#### Scenario: Spawning a builder

- **WHEN** an agent is spawned for a sub-task with `role=builder` and the adapter is Claude Code
- **THEN** the tmux session SHALL launch with the builder system prompt (passed via the adapter's launch command, e.g. `claude --system-prompt "$(cat .legato/swarm/prompt-builder.md)"`)

#### Scenario: Adapter without role prompts

- **WHEN** an agent is spawned for a sub-task and the adapter does not implement `RoleSystemPrompt`
- **THEN** the agent SHALL still spawn with the role tag but without an injected prompt; the role context is conveyed only via env vars

### Requirement: Reviewer auto-spawn

When a sub-task transitions from `building` to `review`, the system SHALL automatically spawn a reviewer agent in a new tmux pane scoped to the same files.

#### Scenario: Auto-spawn on build completion

- **WHEN** a sub-task transitions to `review`
- **THEN** the system SHALL spawn an agent with `role=reviewer`, the same scope_globs, and a system prompt directing it to review the diff produced by the builder against the sub-task description

#### Scenario: Reviewer agent decision broadcast

- **WHEN** a reviewer agent calls `legato swarm review <subtask-id> --approve` or `--reject` from within its tmux session
- **THEN** the lifecycle transition SHALL fire and the IPC broadcast SHALL notify all running TUI instances

### Requirement: Swarm parent card display

The board SHALL render swarm parent cards with a visual indicator showing sub-task count and aggregate status.

#### Scenario: Parent with active swarm

- **WHEN** a card on the board has 3 active sub-tasks (1 done, 1 building, 1 queued)
- **THEN** the card SHALL display a swarm badge showing `1/3` (completed/total) and an icon indicating swarm mode
