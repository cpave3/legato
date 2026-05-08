## ADDED Requirements

### Requirement: Agent role tagging

`agent_sessions` rows SHALL carry an optional `role` column with values from `{coordinator, builder, scout, reviewer, ""}` and an optional `parent_task_id` column linking the session to a swarm parent.

#### Scenario: Role column on schema

- **WHEN** the database is migrated
- **THEN** the `agent_sessions` table SHALL include `role TEXT NOT NULL DEFAULT ''` and `parent_task_id TEXT NULL` columns

#### Scenario: Non-swarm agent

- **WHEN** an agent is spawned without swarm context (existing single-task spawn)
- **THEN** the row SHALL have `role = ''` and `parent_task_id = NULL`

#### Scenario: Swarm agent

- **WHEN** an agent is spawned for a sub-task with role `builder` parented to task `abc12345`
- **THEN** the row SHALL have `role = 'builder'` and `parent_task_id = 'abc12345'`

### Requirement: Swarm-aware spawn

`AgentService.SpawnAgent` SHALL accept optional swarm parameters (`role`, `parent_task_id`, `scope_globs`) and SHALL refuse to spawn when the scope conflicts with an active sibling.

#### Scenario: Spawn with role and scope

- **WHEN** `SpawnAgent(ctx, taskID, AgentSpawnOptions{Role: "builder", ParentTaskID: "abc", Scope: []string{"api/**"}})` is called
- **THEN** the tmux session SHALL be created with `LEGATO_AGENT_ROLE=builder` and `LEGATO_PARENT_TASK_ID=abc` env vars set
- **AND** the agent_sessions row SHALL persist `role` and `parent_task_id`

#### Scenario: Spawn refused on scope conflict

- **WHEN** `SpawnAgent` is called for a sub-task whose scope overlaps an active sibling
- **THEN** the call SHALL return an error referencing the conflicting sibling's task ID and SHALL NOT create a tmux session or DB row

### Requirement: Reconciliation preserves role

`ReconcileSessions` SHALL preserve the `role` and `parent_task_id` columns when transitioning sessions to `dead`.

#### Scenario: Dead swarm session

- **WHEN** a swarm builder agent's tmux session is killed externally
- **AND** `ReconcileSessions` runs
- **THEN** the row SHALL be marked `status='dead'` while `role='builder'` and `parent_task_id` remain set
