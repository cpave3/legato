## ADDED Requirements

### Requirement: Swarm HTTP endpoints

The web server SHALL expose HTTP endpoints that mirror the legato swarm CLI verbs needed for interactive control. Each endpoint sits behind the existing bearer-token auth middleware and returns JSON.

#### Scenario: Start a swarm

- **WHEN** the client sends `POST /api/swarm/start` with body `{"parent_task_id":"abc12345","working_dir":"/home/me/Projects/foo"}`
- **THEN** the server SHALL call `SwarmService.StartSwarm` with those values
- **AND** respond `201 Created` with `{"status":"ok"}` on success
- **AND** respond `4xx` with `{"error":"<message>"}` when the parent doesn't exist, already has a running agent, or the working directory is not a valid directory

#### Scenario: Send a message to a worker

- **WHEN** the client sends `POST /api/swarm/message` with body `{"subtask_id":"st-3f9a","text":"add tests"}`
- **THEN** the server SHALL call `SwarmService.Message`
- **AND** respond `200 OK` with `{"status":"ok"}` on success
- **AND** respond `4xx` when the sub-task or worker session is not found

#### Scenario: Close a worker

- **WHEN** the client sends `POST /api/swarm/close` with body `{"subtask_id":"st-3f9a"}`
- **THEN** the server SHALL call `SwarmService.Close`
- **AND** respond `200 OK` on success

#### Scenario: Finish a swarm

- **WHEN** the client sends `POST /api/swarm/finish` with body `{"parent_task_id":"abc12345","summary":"<text>"}`
- **THEN** the server SHALL call `SwarmService.Finish`
- **AND** respond `200 OK` on success

#### Scenario: Manual dispatch

- **WHEN** the client sends `POST /api/swarm/dispatch` with body `{"subtask_id":"st-3f9a"}`
- **THEN** the server SHALL call `SwarmService.Dispatch`
- **AND** respond `200 OK` on success

#### Scenario: Read swarm status

- **WHEN** the client sends `GET /api/swarm/status/<parent-id>`
- **THEN** the server SHALL respond `200 OK` with the JSON snapshot from `SwarmService.Snapshot`

#### Scenario: Drain inbox

- **WHEN** the client sends `GET /api/swarm/inbox/<parent-id>`
- **THEN** the server SHALL call `SwarmService.FetchInbox` (which marks events acked) and respond `200 OK` with `{"events": [...]}` containing the unacked events at the time of the call

#### Scenario: Pending plan lookup

- **WHEN** the client sends `GET /api/swarm/pending-plan/<parent-id>`
- **THEN** the server SHALL respond `200 OK` with `{"plan_path":"<canonical-path>","reply_socket":"<path>"}` if a plan was recently proposed and not yet verdicted, or `404 Not Found` if no proposal is pending
- **AND** the response SHALL allow a freshly-connected client to render the approval modal without missing the IPC event

### Requirement: Auth and CORS parity

All swarm endpoints SHALL be protected by the same bearer-token middleware that guards `/api/agents/*` and SHALL return CORS headers consistent with the rest of the web API.

#### Scenario: Unauthenticated request rejected

- **WHEN** the client sends `POST /api/swarm/start` without a valid `Authorization: Bearer <token>` header
- **THEN** the server SHALL respond `401 Unauthorized` and SHALL NOT invoke `SwarmService`

#### Scenario: Preflight CORS

- **WHEN** the client sends `OPTIONS /api/swarm/start`
- **THEN** the server SHALL respond with `Access-Control-Allow-Origin: *`, `Access-Control-Allow-Methods` including `POST`, and `Access-Control-Allow-Headers` including `Authorization` and `Content-Type`

### Requirement: WebSocket plan-proposed broadcast

The web server SHALL subscribe to `events.EventPlanProposed` at startup and broadcast a `plan_proposed` WebSocket message to every connected client whenever the event fires.

#### Scenario: Plan proposal reaches all clients

- **WHEN** any conductor calls `legato swarm propose-plan` (which publishes `EventPlanProposed`)
- **THEN** every connected WebSocket client SHALL receive a message with `type: "plan_proposed"`, `parent_task_id`, `plan_path`, and `reply_socket` fields

#### Scenario: Verdict via WebSocket

- **WHEN** a client sends a WebSocket message `{"type":"plan_verdict","parent_task_id":"abc","status":"approved","plan_path":"<path>"}` (or `"rejected"` with `notes`)
- **THEN** the server SHALL forward the verdict to the conductor's reply socket via `ipc.Send`
- **AND** SHALL log but not surface failures (e.g. reply socket already closed because another client verdicted first)

### Requirement: WebSocket swarm-changed broadcast

The web server SHALL subscribe to `events.EventSwarmChanged` and broadcast a `swarm_changed` WebSocket message to every connected client whenever the event fires.

#### Scenario: Swarm transition reaches all clients

- **WHEN** any sub-task transitions state (dispatch, in_progress, reporting, done, cancelled, finished)
- **THEN** every connected WebSocket client SHALL receive a message with `type: "swarm_changed"`, `parent_task_id`, `subtask_id` (when applicable), and `new_status`

### Requirement: Error response schema

All swarm endpoint error responses SHALL use a consistent JSON shape so the web UI can render them uniformly.

#### Scenario: User error shape

- **WHEN** any swarm endpoint returns a `4xx` status
- **THEN** the response body SHALL be `{"error":"<human-readable message>"}`

#### Scenario: Server error shape

- **WHEN** any swarm endpoint returns a `5xx` status
- **THEN** the response body SHALL be `{"error":"<human-readable message>"}` with the same shape as user errors
