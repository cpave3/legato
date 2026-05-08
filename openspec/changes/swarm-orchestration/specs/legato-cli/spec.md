## ADDED Requirements

### Requirement: swarm decompose subcommand

The system SHALL provide `legato swarm decompose <parent-task-id>` to create sub-tasks for an existing task. The command SHALL accept sub-task definitions either via `--from-file <path>` (JSON or YAML) or via repeated `--subtask "title:scope:role"` flags.

#### Scenario: Decompose from file

- **WHEN** `legato swarm decompose abc12345 --from-file plan.yaml` is executed
- **AND** `plan.yaml` contains a list of sub-tasks
- **THEN** the system SHALL create the sub-tasks and broadcast IPC to running instances

#### Scenario: Decompose from flags

- **WHEN** `legato swarm decompose abc12345 --subtask "API:api/**:builder" --subtask "UI:web/**:builder"` is executed
- **THEN** the system SHALL create two sub-tasks with the given titles, scopes, and roles

#### Scenario: Decompose missing parent

- **WHEN** the parent task ID does not exist
- **THEN** the command SHALL exit with code 1 and an error message

### Requirement: swarm status subcommand

The system SHALL provide `legato swarm status <parent-task-id>` that prints the JSON coordination surface for the given swarm.

#### Scenario: Print snapshot

- **WHEN** `legato swarm status abc12345` is executed
- **THEN** stdout SHALL contain a JSON document matching `SwarmService.Snapshot` output
- **AND** the command SHALL exit with code 0

#### Scenario: Non-swarm task

- **WHEN** `legato swarm status xyz` is executed for a task with no sub-tasks
- **THEN** the command SHALL print an empty `subtasks: []` snapshot (not an error)

### Requirement: swarm review subcommand

The system SHALL provide `legato swarm review <subtask-id> [--approve|--reject] [--notes <text>]` that applies a review verdict.

#### Scenario: Approve sub-task

- **WHEN** `legato swarm review sub-1 --approve` is executed
- **THEN** the sub-task SHALL transition to `done`, IPC SHALL broadcast, and the command SHALL exit with code 0

#### Scenario: Reject with notes

- **WHEN** `legato swarm review sub-1 --reject --notes "missing tests"` is executed
- **THEN** the sub-task SHALL transition back to `building`, the notes SHALL be appended to the description, and the builder agent SHALL be respawned (or kept alive if still running)

#### Scenario: Review without verdict

- **WHEN** `legato swarm review sub-1` is executed without `--approve` or `--reject`
- **THEN** the command SHALL exit with code 1 and an error indicating one of the flags is required

### Requirement: swarm assign subcommand

The system SHALL provide `legato swarm assign <subtask-id>` that manually starts a queued sub-task by spawning its agent.

#### Scenario: Assign queued sub-task

- **WHEN** `legato swarm assign sub-2` is executed and the sub-task is `queued` with no scope conflict
- **THEN** the system SHALL spawn the agent and transition the sub-task to `building`

#### Scenario: Assign with conflict

- **WHEN** the sub-task's scope overlaps an active sibling
- **THEN** the command SHALL exit with code 1 naming the conflicting sibling
