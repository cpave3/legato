## ADDED Requirements

### Requirement: Existing-session reflection interview
The system SHALL provide an `agent.interview` workflow executor that sends one versioned structured reflection request to the active agent for a target task through the adapter's existing interaction transport.

#### Scenario: Reachable Chimera task agent
- **WHEN** an interview step targets a task with a reachable Chimera session
- **THEN** Legato SHALL create a durable reflection request and deliver its prompt through the Chimera interaction endpoint

#### Scenario: Reachable non-Chimera task agent
- **WHEN** an interview step targets another reachable agent
- **THEN** Legato SHALL deliver the request through the existing tmux interaction path

### Requirement: Typed reflection callback
The interviewed agent SHALL submit a versioned bounded reflection document through the Legato CLI/API using the durable request ID. Conversational pane output SHALL NOT count as submission.

#### Scenario: Valid reflection submission
- **WHEN** the agent runs `legato learn reflection submit` with the active request and valid observations
- **THEN** Legato SHALL persist the observations as attributable evidence and advance the waiting workflow step

#### Scenario: Malformed or stale submission
- **WHEN** a callback has invalid schema, exceeds bounds, or targets a stale request
- **THEN** Legato SHALL reject it with an actionable error and SHALL NOT advance or partially persist it

### Requirement: Borrowed session ownership
An interview workflow SHALL borrow the target task session without owning its lifecycle. Completing, failing, retrying, or cancelling the interview SHALL NOT finish, delete, rename, or kill the task agent session.

#### Scenario: Interview completes
- **WHEN** a borrowed-session interview completes successfully
- **THEN** the original agent session SHALL remain running and associated with its original task

### Requirement: Unavailable session behavior
A missing or unreachable target session SHALL produce a typed unavailable reflection result rather than launch a replacement, read a full transcript, or fail the task transition.

#### Scenario: Task enters Review after its agent exited
- **WHEN** the learning workflow interviews that task
- **THEN** the reflection step SHALL complete as unavailable with a visible reason and downstream curation SHALL remain able to process durable feedback

### Requirement: Non-blocking reflection trigger
Starting or failing session reflection SHALL NOT block or reverse the local task transition that triggered learning.

#### Scenario: Reflection delivery fails
- **WHEN** a task enters a configured learning column and reflection delivery fails
- **THEN** the task SHALL remain in the destination column and the workflow failure SHALL be retryable through normal workflow/Attention surfaces
