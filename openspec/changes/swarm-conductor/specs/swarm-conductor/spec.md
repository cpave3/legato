## ADDED Requirements

### Requirement: Conductor agent role

The system SHALL spawn a single long-running **conductor** agent per swarm. The conductor is an LLM-driven agent (any configured AI tool) launched with a system prompt that frames it as a project manager: it explores the codebase, drafts a plan, dispatches workers, observes their reports, and decides when the swarm is complete.

#### Scenario: Starting a swarm spawns the conductor

- **WHEN** the user starts a swarm on a parent task by selecting a card and pressing `S`, supplying a working directory
- **THEN** the system SHALL spawn a tmux session for the conductor named `legato-<parent-task-id>`
- **AND** the session SHALL launch the configured AI tool with the conductor's system prompt and an initial brief containing the parent task description and instructions to explore before planning

#### Scenario: One conductor per parent

- **WHEN** the user attempts to start a swarm on a parent task that already has a running agent (regular or conductor)
- **THEN** the system SHALL refuse the start and prompt the user to either kill the existing agent or cancel

#### Scenario: Conductor's working directory

- **WHEN** the conductor is spawned
- **THEN** its tmux session SHALL be created with `-c <working-dir>` so the agent runs in the user-supplied project directory
- **AND** all spawned workers SHALL inherit the same working directory

### Requirement: Conductor lifecycle stages

The conductor SHALL operate in four observable stages: `exploring → planning → dispatching → finished`. The conductor's prompt directs it through these stages, and the system surfaces the current stage in the TUI.

#### Scenario: Exploration before planning

- **WHEN** the conductor receives its initial brief
- **THEN** the conductor SHALL read relevant code (file listings, source files, tests) before drafting a plan
- **AND** the conductor SHALL NOT call `legato swarm propose-plan` until exploration is complete

#### Scenario: Plan submission triggers approval gate

- **WHEN** the conductor calls `legato swarm propose-plan <file>`
- **THEN** the call SHALL block until a human verdict is received
- **AND** the system SHALL transition the swarm's reported stage from `planning` to `dispatching` only after approval

#### Scenario: Conductor declares completion

- **WHEN** the conductor calls `legato swarm finish <parent-id> "<summary>"`
- **THEN** the swarm SHALL transition to `finished`
- **AND** all live worker sessions SHALL be terminated
- **AND** the summary SHALL be appended to the parent task description as a final report

### Requirement: Conductor wake-up via send-keys

The system SHALL deliver swarm state changes to the conductor by typing them into its tmux pane as new user turns. The conductor's role prompt SHALL instruct it to handle these messages as state notifications, not as colloquial user input.

#### Scenario: Worker progress notification

- **WHEN** a worker calls `legato swarm progress <subtask-id> "<text>"`
- **THEN** the system SHALL deliver `[swarm event] worker "<title>" (<subtask-id>) reported progress: > "<text>"` to the conductor's pane via `tmux send-keys`

#### Scenario: Worker completion notification

- **WHEN** a worker calls `legato swarm built <subtask-id>`
- **THEN** the system SHALL deliver `[swarm event] worker "<title>" (<subtask-id>) marked itself built. Run \`legato swarm close <subtask-id>\` to terminate, or send a follow-up.` to the conductor's pane

#### Scenario: All workers idle notification

- **WHEN** every worker in the swarm is in `reporting` status (built but not yet closed) or `queued`
- **THEN** the system SHALL deliver `[swarm event] all workers in this swarm are idle (built or queued). Decide: dispatch more, ask the user, or call \`legato swarm finish\`.` to the conductor's pane

#### Scenario: Worker death notification

- **WHEN** a worker's tmux session dies (explicit kill or external termination) before being closed by the conductor
- **THEN** the system SHALL deliver `[swarm event] worker "<title>" (<subtask-id>) died unexpectedly.` to the conductor's pane
- **AND** the worker's sub-task SHALL transition to `cancelled`

### Requirement: Conductor delegation by goal, not procedure

The conductor's plan SHALL describe sub-tasks by goal and supply a free-form prompt per worker; the system SHALL NOT impose a fixed prompt template. The conductor is responsible for writing each worker's brief.

#### Scenario: Per-worker prompt

- **WHEN** a plan entry includes a `prompt` field
- **THEN** the system SHALL deliver that prompt verbatim as the worker's initial conversational turn after the launch command completes

#### Scenario: Default prompt fallback

- **WHEN** a plan entry omits the `prompt` field
- **THEN** the system SHALL render a default brief from the parent task description, sub-task title, scope (if any), and a completion-instruction footer
- **AND** the default brief SHALL be visible in the plan-approval overlay so the user can sanity-check it

### Requirement: Conductor activity tracking

The conductor SHALL participate in the existing agent activity tracking system (working / waiting / idle indicators) so its state surfaces on the parent card.

#### Scenario: Conductor activity reflects on parent card

- **WHEN** the conductor is processing a turn (working) or idle awaiting events (waiting)
- **THEN** the parent task card SHALL show the corresponding activity badge, identical to a single-task agent
