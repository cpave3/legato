## ADDED Requirements

### Requirement: send-keys message delivery

The system SHALL deliver inter-agent messages by invoking `tmux send-keys` against the receiving agent's session. Each delivered message SHALL appear in the receiver's pane as a new user-conversational turn that the AI tool processes when it next reads input.

#### Scenario: Conductor messages a specific worker

- **WHEN** the conductor calls `legato swarm message <subtask-id> "<text>"`
- **THEN** the system SHALL run `tmux send-keys -t legato-<subtask-id> "<text>" Enter` against that worker's session
- **AND** the call SHALL return success once the keys have been sent

#### Scenario: Conductor broadcasts to all workers

- **WHEN** the conductor calls `legato swarm broadcast <parent-id> "<text>"`
- **THEN** the system SHALL deliver the text via `send-keys` to every worker session in that swarm whose status is `dispatched`, `in_progress`, or `reporting`

#### Scenario: Multi-line and special-character payloads

- **WHEN** a delivered message contains newlines, quotes, or other shell-significant characters
- **THEN** the system SHALL base64-encode the payload and deliver it as `[swarm event b64:<encoded>]`
- **AND** the receiving agent's role prompt SHALL include instructions to decode any `b64:` payload before processing

### Requirement: Worker-to-conductor reporting

Workers SHALL report progress, ask questions, and signal completion via legato CLI commands. The system SHALL relay these to the conductor's pane via `send-keys`.

#### Scenario: Progress report

- **WHEN** a worker calls `legato swarm progress <subtask-id> "<text>"`
- **THEN** the system SHALL append the text to the sub-task's progress log
- **AND** the system SHALL deliver a formatted notification to the conductor's pane (debounced — see below)

#### Scenario: Worker question

- **WHEN** a worker calls `legato swarm question <subtask-id> "<text>"`
- **THEN** the system SHALL deliver `[swarm event] worker "<title>" (<subtask-id>) asks: > "<text>"` to the conductor's pane immediately, bypassing any debounce

#### Scenario: Completion signal

- **WHEN** a worker calls `legato swarm built <subtask-id>`
- **THEN** the sub-task SHALL transition from `in_progress` to `reporting`
- **AND** the system SHALL deliver `[swarm event] worker "<title>" (<subtask-id>) marked itself built` to the conductor's pane immediately

### Requirement: Progress event debouncing

To prevent the conductor's pane from filling with rapid-fire progress reports, the system SHALL debounce multiple progress events from the same worker within a short window. `built`, `question`, and death events SHALL bypass debouncing.

#### Scenario: Multiple rapid progress reports collapse

- **WHEN** a worker calls `legato swarm progress` three times within 1 second
- **THEN** the conductor SHALL receive at most one delivered notification within that window, containing the most recent progress text

#### Scenario: Bypass on completion

- **WHEN** a worker calls `legato swarm progress` and immediately follows with `legato swarm built`
- **THEN** the `built` notification SHALL be delivered to the conductor without delay regardless of the progress debounce window

### Requirement: Send-keys reliability and best-effort semantics

`send-keys` delivery SHALL be best-effort. The system SHALL NOT block waiting for the receiver to process a delivered message, and SHALL NOT retry on transient failure beyond one attempt.

#### Scenario: Receiver pane has died

- **WHEN** the system attempts to send-keys to a session that no longer exists
- **THEN** the call SHALL log the failure and return without raising an error to the caller
- **AND** the failure SHALL trigger the standard "agent died" event flow if not already in progress

#### Scenario: Receiver mid-turn

- **WHEN** a message is delivered while the receiving agent is mid-turn (still generating output)
- **THEN** the system SHALL deliver the keys (tmux queues them at the prompt) and the receiver SHALL process them when it next reads input

### Requirement: Conductor as the only delegator in v1

The system SHALL accept worker commands (`progress`, `question`, `built`) only from sub-task agents and SHALL reject worker attempts to invoke conductor commands (`dispatch`, `propose-plan`, `message`, `broadcast`, `close`, `finish`).

#### Scenario: Worker attempts to dispatch

- **WHEN** a worker (identified by `LEGATO_AGENT_ROLE` set to a non-conductor role) invokes `legato swarm dispatch`
- **THEN** the CLI SHALL exit non-zero with a message indicating only the conductor may dispatch sub-tasks

#### Scenario: Worker attempts to broadcast

- **WHEN** a worker invokes `legato swarm broadcast`
- **THEN** the CLI SHALL exit non-zero with the same authorization message
