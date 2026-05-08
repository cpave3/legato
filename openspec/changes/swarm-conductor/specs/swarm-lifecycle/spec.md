## ADDED Requirements

### Requirement: Sub-task data model

The system SHALL persist swarm sub-tasks in a `swarm_subtasks` table where each row represents a unit of work owned by a single worker agent and parented to an existing task.

#### Scenario: Sub-task schema

- **WHEN** the database is migrated
- **THEN** a `swarm_subtasks` table SHALL exist with columns: `id` (TEXT PK), `parent_task_id` (TEXT FK to tasks), `title` (TEXT), `prompt` (TEXT), `role` (TEXT, free-form label), `agent_kind` (TEXT, e.g. `claude-code`, `chimera`, or empty for default), `scope_globs` (TEXT, JSON array), `status` (TEXT, one of `queued|dispatched|in_progress|reporting|done|cancelled`), `worker_agent_id` (INTEGER NULL), `created_at` (DATETIME), `dispatched_at` (DATETIME NULL), `completed_at` (DATETIME NULL)

#### Scenario: Parent task swarm metadata

- **WHEN** the database is migrated
- **THEN** the `tasks` table SHALL include `swarm_working_dir TEXT NULL` to record the working directory the swarm operates in
- **AND** non-swarm tasks SHALL have this column NULL

### Requirement: Sub-task lifecycle state machine

A sub-task SHALL transition through `queued → dispatched → in_progress → reporting → done` (or `→ cancelled` from any prior state). The system SHALL enforce valid transitions.

#### Scenario: Dispatch transitions queued to dispatched

- **WHEN** the conductor calls `legato swarm dispatch <subtask-id>` for a sub-task in `queued`
- **THEN** the sub-task SHALL transition to `dispatched`
- **AND** `dispatched_at` SHALL be set to the current time
- **AND** the system SHALL spawn the worker agent

#### Scenario: First worker activity transitions to in_progress

- **WHEN** a dispatched worker emits any output (observed via tmux pane state) OR calls `legato swarm progress` for the first time
- **THEN** the sub-task SHALL transition from `dispatched` to `in_progress`

#### Scenario: Worker built transitions to reporting

- **WHEN** a worker calls `legato swarm built <subtask-id>` while in `in_progress`
- **THEN** the sub-task SHALL transition to `reporting`
- **AND** the worker session SHALL remain alive until the conductor closes it

#### Scenario: Conductor close transitions to done

- **WHEN** the conductor calls `legato swarm close <subtask-id>` for a sub-task in `reporting` or `in_progress`
- **THEN** the sub-task SHALL transition to `done`
- **AND** `completed_at` SHALL be set
- **AND** the worker's tmux session SHALL be terminated

#### Scenario: Cancellation from any state

- **WHEN** the conductor or user kills a worker session before completion
- **THEN** the sub-task SHALL transition to `cancelled`
- **AND** `completed_at` SHALL be set

#### Scenario: Invalid transitions rejected

- **WHEN** any code path attempts to transition a sub-task in `done` or `cancelled` to any other state
- **THEN** the operation SHALL return an error and the row SHALL remain unchanged

### Requirement: Per-swarm working directory

Each swarm SHALL have an associated working directory. Workers SHALL spawn in that directory and scope globs SHALL be interpreted relative to it.

#### Scenario: Working directory captured at swarm start

- **WHEN** the user starts a swarm via the swarm-init overlay
- **THEN** the supplied working directory SHALL be persisted on the parent task as `swarm_working_dir`
- **AND** the conductor's tmux session SHALL be created with `-c <working-dir>`

#### Scenario: Workers inherit working directory

- **WHEN** a worker is dispatched
- **THEN** the worker's tmux session SHALL be created with `-c <swarm-working-dir>`
- **AND** scope-conflict checks SHALL walk that directory tree, not the legato process working directory

#### Scenario: Working directory must exist

- **WHEN** the user supplies a working directory path
- **THEN** the swarm-init overlay SHALL validate the path exists and is a directory before spawning the conductor
- **AND** invalid paths SHALL produce an inline error in the overlay

### Requirement: Per-sub-task agent picker

A plan entry MAY specify the AI tool used for that worker. When unspecified, the system SHALL use the default from `cfg.Swarm.DefaultAgent`.

#### Scenario: Plan specifies agent

- **WHEN** a plan entry has `agent: chimera`
- **THEN** the worker for that sub-task SHALL be launched using the Chimera adapter regardless of `cfg.Swarm.DefaultAgent`

#### Scenario: Plan omits agent

- **WHEN** a plan entry omits the `agent` field
- **THEN** the worker SHALL be launched using `cfg.Swarm.DefaultAgent`
- **AND** if `cfg.Swarm.DefaultAgent` is unset, the system SHALL fall back to the first registered adapter

#### Scenario: Unknown agent rejected at validation

- **WHEN** a plan entry specifies an `agent` value that does not match any registered adapter
- **THEN** the plan SHALL be rejected at `propose-plan` validation time with an error naming the unknown agent

### Requirement: Concurrent worker cap

The system SHALL cap concurrent live workers per swarm at `cfg.Swarm.MaxConcurrentAgents` (default 4). When the cap is reached, dispatch attempts SHALL be queued.

#### Scenario: Dispatch within cap

- **WHEN** the swarm has fewer live workers (status `dispatched`, `in_progress`, or `reporting`) than the cap
- **AND** the conductor calls `legato swarm dispatch <subtask-id>`
- **THEN** the worker SHALL be spawned immediately

#### Scenario: Dispatch at cap

- **WHEN** the swarm is at the cap
- **AND** the conductor calls `legato swarm dispatch <subtask-id>`
- **THEN** the sub-task SHALL remain `queued`
- **AND** the system SHALL deliver `[swarm event] dispatch deferred (cap reached)` to the conductor's pane

#### Scenario: Auto-dispatch on slot freed

- **WHEN** a live worker transitions to `done` or `cancelled`
- **AND** at least one queued sub-task exists for the same parent
- **THEN** the system SHALL spawn the next queued sub-task in plan order
