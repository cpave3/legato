## ADDED Requirements

### Requirement: Plan approval modal

The web UI SHALL render a modal when a `plan_proposed` WebSocket message arrives or when a freshly-connected client discovers a pending plan via `GET /api/swarm/pending-plan/<parent-id>`. The modal SHALL display the plan summary, working directory, and a list of sub-tasks (title, role, agent, scope, prompt preview).

#### Scenario: Modal opens on incoming proposal

- **WHEN** the WebSocket delivers a `plan_proposed` message for a swarm with a known parent task
- **THEN** the web client SHALL fetch the plan content from the server (or load from a cached path), parse the YAML, and render the modal

#### Scenario: Plan summary visible

- **WHEN** the modal renders
- **THEN** the user SHALL see (a) the parent task ID and title, (b) the working directory, (c) the conductor's plan summary as markdown, (d) the per-sub-task rows with title, role, agent, scope globs, and a one-line prompt preview

#### Scenario: Prompt expandable

- **WHEN** the user clicks a sub-task's prompt preview
- **THEN** the full prompt content SHALL render inline, expanded; clicking again collapses it

### Requirement: Verdict actions

The plan approval modal SHALL offer three explicit actions: approve, reject with notes, and dismiss without verdict.

#### Scenario: Approve

- **WHEN** the user clicks the Approve button
- **THEN** the web client SHALL send a WebSocket `plan_verdict` message with `status: "approved"` and the original `plan_path`
- **AND** the modal SHALL close
- **AND** the server SHALL forward the verdict to the conductor's reply socket

#### Scenario: Reject with notes

- **WHEN** the user clicks Reject and types rejection notes into the input
- **AND** the user submits the notes
- **THEN** the web client SHALL send a WebSocket `plan_verdict` message with `status: "rejected"` and the notes string
- **AND** the modal SHALL close

#### Scenario: Dismiss without verdict

- **WHEN** the user clicks Close (or presses Escape) without rendering a verdict
- **THEN** the modal SHALL close
- **AND** no `plan_verdict` SHALL be sent
- **AND** the conductor SHALL remain blocked on its `propose-plan` call

#### Scenario: Verdict already received

- **WHEN** the user clicks Approve or Reject and the server reports that the conductor's reply socket is closed (because another client verdicted first)
- **THEN** the web client SHALL display an inline message ("This plan has already been verdicted by another client") and close the modal

### Requirement: Pending-plan re-discovery on reconnect

When a web client connects (or reconnects) to legato, it SHALL check for any pending plan via `GET /api/swarm/pending-plan/<parent-id>` for parents with active swarms and surface the modal if one is found.

#### Scenario: Pending plan after reconnect

- **WHEN** a web client reconnects after the WebSocket dropped while a plan was pending
- **THEN** the client SHALL discover the pending plan via the GET endpoint and re-open the modal

#### Scenario: No pending plan

- **WHEN** the GET endpoint returns `404 Not Found` for the parent
- **THEN** the client SHALL NOT render the modal

### Requirement: No in-browser editor in v1

The web plan approval modal SHALL NOT include an in-browser YAML editor. Users wanting to edit a plan SHALL either reject it with descriptive notes (so the conductor revises) or fall back to the TUI overlay's `e` keybinding.

#### Scenario: Edit affordance absent

- **WHEN** the modal renders
- **THEN** there SHALL be no Edit button or in-browser text editor for the plan YAML

#### Scenario: Reject-with-notes is the substitute

- **WHEN** the user wants the plan changed
- **THEN** the modal documentation SHALL direct them toward Reject with notes as the canonical path
