## ADDED Requirements

### Requirement: swarm subcommand group

The `legato` binary SHALL support a `swarm` subcommand group with verbs partitioned by audience: conductor-facing verbs (`propose-plan`, `dispatch`, `message`, `broadcast`, `close`, `finish`, `status`) and worker-facing verbs (`progress`, `question`, `built`).

#### Scenario: Swarm subcommand dispatch

- **WHEN** `legato swarm <verb> ...` is invoked
- **THEN** the binary SHALL dispatch to the swarm verb handler
- **AND** unknown verbs SHALL print usage to stderr and exit with code 1

### Requirement: swarm propose-plan subcommand

The system SHALL provide `legato swarm propose-plan <plan-file>` that submits a plan for human approval. The command SHALL block until a verdict is rendered (or the call is interrupted) and SHALL emit a JSON result on stdout.

#### Scenario: Plan approved

- **WHEN** the user approves the plan via the TUI overlay
- **THEN** the CLI SHALL exit zero with stdout `{"status":"approved","plan_path":"<canonical-path>"}`

#### Scenario: Plan rejected with notes

- **WHEN** the user rejects the plan via the overlay with notes
- **THEN** the CLI SHALL exit zero with stdout `{"status":"rejected","notes":"<text>"}`

#### Scenario: Plan validation failure

- **WHEN** the supplied plan file fails YAML validation (missing fields, malformed globs, exceeds size cap)
- **THEN** the CLI SHALL exit non-zero with stderr describing the first validation error
- **AND** no overlay SHALL be surfaced

#### Scenario: Auto-approve in headless mode

- **WHEN** the call is invoked with `--auto-approve`
- **THEN** the system SHALL skip the overlay and exit zero with `{"status":"approved","plan_path":"<canonical-path>"}`

### Requirement: swarm dispatch subcommand

The system SHALL provide `legato swarm dispatch <subtask-id>` (called by the conductor) that spawns the worker for a queued sub-task.

#### Scenario: Dispatch queued sub-task

- **WHEN** `legato swarm dispatch st-3f9a` is executed for a sub-task in `queued`
- **AND** the swarm is below the concurrent cap
- **THEN** the worker SHALL be spawned, the sub-task SHALL transition to `dispatched`, and the CLI SHALL exit zero

#### Scenario: Dispatch over cap

- **WHEN** the swarm is at the concurrent cap
- **THEN** the sub-task SHALL remain `queued`, the system SHALL emit a `dispatch deferred` event to the conductor, and the CLI SHALL exit zero with stdout `{"status":"deferred"}`

#### Scenario: Dispatch wrong-state sub-task

- **WHEN** `legato swarm dispatch <id>` is called for a sub-task not in `queued`
- **THEN** the CLI SHALL exit non-zero with an error naming the current status

### Requirement: swarm message subcommand

The system SHALL provide `legato swarm message <subtask-id> "<text>"` that delivers text into the worker's tmux pane via `send-keys`.

#### Scenario: Message a live worker

- **WHEN** the conductor invokes `legato swarm message st-3f9a "additional context: ..."` and the worker is alive
- **THEN** the system SHALL run `tmux send-keys -t legato-st-3f9a "additional context: ..." Enter`
- **AND** the CLI SHALL exit zero

#### Scenario: Message a dead worker

- **WHEN** the worker session does not exist
- **THEN** the CLI SHALL exit non-zero with an error indicating the target is not alive

### Requirement: swarm broadcast subcommand

The system SHALL provide `legato swarm broadcast <parent-id> "<text>"` that delivers the message to every live worker in the swarm.

#### Scenario: Broadcast to live workers

- **WHEN** the conductor invokes broadcast and at least one worker is alive
- **THEN** the system SHALL deliver the text to every worker session whose status is `dispatched`, `in_progress`, or `reporting`
- **AND** the CLI SHALL exit zero with stdout reporting the number of recipients

### Requirement: swarm close subcommand

The system SHALL provide `legato swarm close <subtask-id>` that ratifies a worker's completion or terminates a worker mid-flight.

#### Scenario: Close a reporting sub-task

- **WHEN** `legato swarm close st-3f9a` is invoked for a sub-task in `reporting`
- **THEN** the worker session SHALL be terminated, the sub-task SHALL transition to `done`, and the CLI SHALL exit zero

#### Scenario: Close an in-progress sub-task

- **WHEN** `legato swarm close st-3f9a` is invoked for a sub-task in `in_progress` or `dispatched`
- **THEN** the worker session SHALL be terminated, the sub-task SHALL transition to `cancelled`, and the CLI SHALL exit zero

### Requirement: swarm finish subcommand

The system SHALL provide `legato swarm finish <parent-id> "<summary>"` that the conductor calls to declare the swarm complete.

#### Scenario: Finish a swarm

- **WHEN** the conductor invokes `legato swarm finish abc12345 "<summary>"`
- **THEN** all live worker sessions SHALL be terminated, the conductor's session SHALL be terminated, the summary SHALL be appended to the parent task description, and the CLI SHALL exit zero

### Requirement: swarm status subcommand

The system SHALL provide `legato swarm status <parent-id>` that prints the current swarm state as JSON to stdout.

#### Scenario: Print swarm state

- **WHEN** `legato swarm status abc12345` is executed for a swarm
- **THEN** stdout SHALL contain a JSON document with the parent task summary, working directory, and an array of sub-tasks (id, title, role, agent, scope, status, last progress text)

#### Scenario: Status for non-swarm task

- **WHEN** the parent task has no sub-tasks
- **THEN** the JSON SHALL contain an empty `subtasks` array (not an error)

### Requirement: swarm progress subcommand (worker)

The system SHALL provide `legato swarm progress <subtask-id> "<text>"` that workers call to report progress.

#### Scenario: Progress recorded and relayed

- **WHEN** a worker invokes progress
- **THEN** the system SHALL append the text to the sub-task's progress log
- **AND** the system SHALL deliver a debounced notification to the conductor's pane via `send-keys`
- **AND** the CLI SHALL exit zero

### Requirement: swarm question subcommand (worker)

The system SHALL provide `legato swarm question <subtask-id> "<text>"` that workers call to ask the conductor a question. Questions SHALL bypass the progress debounce.

#### Scenario: Question delivered immediately

- **WHEN** a worker invokes question
- **THEN** the conductor SHALL receive the question via `send-keys` without delay
- **AND** the CLI SHALL exit zero

### Requirement: swarm built subcommand (worker)

The system SHALL provide `legato swarm built <subtask-id>` that workers call to signal completion.

#### Scenario: Worker signals completion

- **WHEN** `legato swarm built st-3f9a` is invoked
- **THEN** the sub-task SHALL transition from `in_progress` to `reporting`
- **AND** the conductor SHALL receive a `built` notification via `send-keys`
- **AND** the worker session SHALL remain alive (the conductor terminates it via `close`)

### Requirement: Worker authorization on conductor verbs

The system SHALL refuse worker-side processes from invoking conductor verbs (`dispatch`, `propose-plan`, `message`, `broadcast`, `close`, `finish`).

#### Scenario: Worker invokes dispatch

- **WHEN** a worker (identified by `LEGATO_AGENT_ROLE` set to a non-empty non-conductor value) runs `legato swarm dispatch ...`
- **THEN** the CLI SHALL exit non-zero with an authorization error

## REMOVED Requirements

### Requirement: swarm decompose subcommand

**Reason**: Decomposition is now produced by the conductor agent and submitted via `propose-plan`, not by direct CLI input. The legacy `decompose --from-file` and `decompose --subtask` flags are removed.

**Migration**: Replace any scripted swarm decomposition with a conductor invocation. For headless flows, write the plan YAML directly and call `legato swarm propose-plan <file> --auto-approve`.

### Requirement: swarm review subcommand

**Reason**: Per-sub-task review verdicts are no longer a built-in lifecycle stage. The conductor adjudicates worker completion (or asks the user) and calls `legato swarm close` to ratify done or `legato swarm message` to send corrections.

**Migration**: Replace `legato swarm review <id> --approve` with `legato swarm close <id>`. Replace `legato swarm review <id> --reject --notes "<text>"` with `legato swarm message <id> "<text>"`.

### Requirement: swarm assign subcommand

**Reason**: Manual assign is replaced by `legato swarm dispatch` (called by the conductor) and the auto-dispatch-on-slot-freed behavior under the concurrent cap.

**Migration**: Use `legato swarm dispatch <id>` (when invoked by the conductor) or rely on auto-dispatch.
