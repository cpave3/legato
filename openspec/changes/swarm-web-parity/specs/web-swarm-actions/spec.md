## ADDED Requirements

### Requirement: Start-swarm modal

The web UI SHALL provide a button on the agents view that opens a modal collecting the working directory for a new swarm. On submit, the modal SHALL call `POST /api/swarm/start` with the selected parent task ID and the entered working directory.

#### Scenario: Open the modal

- **WHEN** the user clicks the "Start swarm" button on the agents view
- **AND** a parent task is selected (or selectable via a picker in the modal)
- **THEN** the modal SHALL open with a working-directory input field

#### Scenario: Pre-fill from workspace path

- **WHEN** the selected parent task belongs to a workspace that has `path` configured in `config.yaml`
- **THEN** the working-directory input SHALL pre-fill with that path
- **AND** the user MAY edit it before submitting

#### Scenario: Validation

- **WHEN** the user submits with an empty working directory
- **THEN** the modal SHALL surface an inline error and SHALL NOT call the API

#### Scenario: Server-side rejection

- **WHEN** the user submits a working directory that the server rejects (path doesn't exist, parent already has a conductor, etc.)
- **THEN** the modal SHALL display the server's error message inline and remain open

#### Scenario: Successful start

- **WHEN** the API returns `201 Created`
- **THEN** the modal SHALL close
- **AND** a toast SHALL confirm the swarm has started
- **AND** the conductor SHALL appear in the agents sidebar as soon as the next `agents_changed` WebSocket broadcast arrives

### Requirement: Per-worker action menu

Each agent entry in the `AgentSidebar` that's part of a swarm SHALL expose an overflow menu (`⋯`) with role-appropriate actions.

#### Scenario: Worker actions

- **WHEN** the user clicks the overflow menu on a worker row (role != "conductor")
- **THEN** the menu SHALL show "Send message" and "Close worker"

#### Scenario: Conductor actions

- **WHEN** the user clicks the overflow menu on a conductor row
- **THEN** the menu SHALL show "Send message" and "Finish swarm"

#### Scenario: Send message

- **WHEN** the user selects "Send message" on any swarm participant
- **THEN** an inline input or modal SHALL prompt for the message text
- **AND** on submit, the client SHALL call `POST /api/swarm/message` with the participant's sub-task ID (workers) or the parent ID (conductor) and the entered text
- **AND** SHALL display a toast on success or the server's error message on failure

#### Scenario: Close worker

- **WHEN** the user selects "Close worker" on a worker row
- **THEN** the client SHALL show a confirmation prompt
- **AND** on confirm, SHALL call `POST /api/swarm/close` with the sub-task ID
- **AND** SHALL update the agent list when the next `swarm_changed` or `agents_changed` broadcast arrives

#### Scenario: Finish swarm

- **WHEN** the user selects "Finish swarm" on the conductor row
- **THEN** the client SHALL prompt for a summary text (multi-line input)
- **AND** on submit, SHALL call `POST /api/swarm/finish` with the parent ID and summary
- **AND** SHALL surface a confirmation toast and the conductor SHALL remain visible (it's not killed by `finish`)

### Requirement: Swarm event log panel

The agent split-view SHALL render a "Swarm events" panel below or alongside the terminal panel when the focused agent is part of a swarm. The panel SHALL display the parent's unacked events with a Drain button.

#### Scenario: Panel visible for swarm participants

- **WHEN** the focused agent has a non-empty `parent_task_id`
- **THEN** the swarm event log panel SHALL render

#### Scenario: Panel hidden for solo agents

- **WHEN** the focused agent has no `parent_task_id`
- **THEN** the panel SHALL be hidden

#### Scenario: Event entries

- **WHEN** the panel renders with N unacked events
- **THEN** each event SHALL appear as one line: `#<id> [<kind>] <worker-title> — <one-line preview>`
- **AND** clicking an entry SHALL expand it to show the full payload

#### Scenario: Drain action

- **WHEN** the user clicks the Drain button
- **THEN** the client SHALL call `GET /api/swarm/inbox/<parent-id>` (which acks events server-side)
- **AND** the panel SHALL clear all listed events
- **AND** subsequent unacked events SHALL appear via `swarm_changed` triggered re-fetches

#### Scenario: Live update on new events

- **WHEN** a `swarm_changed` WebSocket message arrives for the focused parent
- **THEN** the client SHALL re-fetch the inbox via `GET /api/swarm/inbox/<parent-id>` *without* draining (i.e. the fetch happens on a different endpoint that doesn't ack — see Open Questions in design)
- **AND** the panel SHALL re-render with any new events
