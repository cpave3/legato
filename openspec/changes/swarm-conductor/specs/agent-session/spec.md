## ADDED Requirements

### Requirement: Swarm role columns on agent sessions

The `agent_sessions` table SHALL include columns recording the agent's swarm role and parent/sub-task linkage. Non-swarm sessions SHALL have these columns at their zero values.

#### Scenario: Schema migration

- **WHEN** the database is migrated to v1 swarm
- **THEN** `agent_sessions` SHALL include `role TEXT NOT NULL DEFAULT ''`, `parent_task_id TEXT NULL`, `subtask_id TEXT NULL` columns

#### Scenario: Single-task agent leaves columns empty

- **WHEN** a non-swarm agent is spawned for a card
- **THEN** the row SHALL have `role = ''` and `parent_task_id = NULL` and `subtask_id = NULL`

#### Scenario: Conductor agent linked to parent task

- **WHEN** a swarm conductor is spawned for parent task `abc12345`
- **THEN** the row SHALL have `role = 'conductor'` and `parent_task_id = 'abc12345'` and `subtask_id = NULL`

#### Scenario: Worker agent linked to sub-task and parent

- **WHEN** a swarm worker is spawned for sub-task `st-3f9a` under parent `abc12345`
- **THEN** the row SHALL have `role` set to the worker's free-form role label, `parent_task_id = 'abc12345'`, and `subtask_id = 'st-3f9a'`

### Requirement: Kill paths publish EventAgentDied

The system SHALL publish an `EventAgentDied` event whenever an agent session transitions to `dead`, regardless of whether the transition was triggered by an explicit kill or by reconciliation detecting external termination.

#### Scenario: Explicit kill publishes event

- **WHEN** the user kills an agent via the agent view's `K` keybinding or via `legato swarm close`
- **THEN** `agentService.KillAgent` SHALL publish `EventAgentDied` carrying `{TaskID, ParentTaskID, SubtaskID, Role}` to the event bus

#### Scenario: Reconciled death publishes event

- **WHEN** `ReconcileSessions` detects an externally-terminated tmux session
- **THEN** the system SHALL publish `EventAgentDied` for that session with the same payload shape

### Requirement: One conductor per parent task

The system SHALL refuse to spawn a conductor for a parent task that already has any running agent session (conductor or otherwise) for the same `task_id`.

#### Scenario: Conductor spawn refused on existing agent

- **WHEN** the user attempts to start a swarm on a parent task with a running single-task agent
- **THEN** the system SHALL return an error indicating the existing agent must be killed first
- **AND** no new tmux session SHALL be created

## MODIFIED Requirements

### Requirement: Tmux session spawning

The system SHALL spawn a new tmux session tied to a specific card when the user requests an agent. The session SHALL use the naming convention `legato-<TASK_ID>` and start in the swarm working directory (for swarm participants) or the user's current directory (for single-task agents). After session creation, the system SHALL invoke the configured AI tool adapter's launch command via `tmux send-keys` so the AI tool starts running automatically.

#### Scenario: Spawning a single-task agent on a card

- **WHEN** the user initiates agent spawn for card "REX-1238"
- **THEN** the system SHALL create a tmux session named `legato-REX-1238`, insert a row into `agent_sessions` with status `running`
- **AND** the system SHALL invoke the configured adapter's `LaunchCommand` via `send-keys` so the AI tool launches inside the session

#### Scenario: Spawning when tmux is not installed

- **WHEN** the user attempts to spawn an agent and tmux is not found on PATH
- **THEN** the system SHALL return an error indicating tmux is required and SHALL NOT create a database record

#### Scenario: Spawning a duplicate session

- **WHEN** the user attempts to spawn an agent for a card that already has a running agent session
- **THEN** the system SHALL return an error indicating an agent is already active for that card

#### Scenario: Adapter without launch command falls back to shell

- **WHEN** the configured adapter does not implement `LaunchCommand` or returns empty
- **THEN** the system SHALL skip the auto-launch step and the tmux session SHALL be left at a shell prompt with env vars set
