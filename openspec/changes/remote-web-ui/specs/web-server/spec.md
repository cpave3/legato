## ADDED Requirements

### Requirement: HTTP server serves REST API and embedded SPA
The web server SHALL serve the React SPA from `embed.FS` at the root path and expose REST API endpoints under `/api/`. All non-API, non-WebSocket requests for paths not matching a static file SHALL return the SPA's `index.html` for client-side routing.

#### Scenario: Serve SPA index
- **WHEN** a browser requests `GET /`
- **THEN** the server returns the embedded `index.html` with status 200

#### Scenario: Serve SPA static assets
- **WHEN** a browser requests `GET /assets/main.js`
- **THEN** the server returns the embedded JavaScript file with appropriate content type

#### Scenario: Client-side route fallback
- **WHEN** a browser requests `GET /agents` (a client-side route)
- **THEN** the server returns `index.html` (not 404) so the React router handles it

#### Scenario: API route precedence
- **WHEN** a request is made to `GET /api/agents`
- **THEN** the server returns JSON API response, not the SPA

### Requirement: REST endpoint for listing agents
The server SHALL expose `GET /api/agents` returning a JSON array of active agent sessions with task context.

#### Scenario: List agents with active sessions
- **WHEN** `GET /api/agents` is requested and there are 2 active agent sessions
- **THEN** the response is a JSON array of 2 agent objects, each containing `id`, `task_id`, `task_title`, `status`, `command`, `activity`, and `started_at`

#### Scenario: List agents with no sessions
- **WHEN** `GET /api/agents` is requested and there are no active sessions
- **THEN** the response is an empty JSON array `[]`

### Requirement: REST endpoint for listing tasks
The server SHALL expose `GET /api/tasks` returning a JSON array of tasks grouped by column status.

#### Scenario: List tasks
- **WHEN** `GET /api/tasks` is requested
- **THEN** the response is a JSON object with column names as keys and arrays of task objects as values

### Requirement: WebSocket endpoint for real-time communication
The server SHALL expose `GET /ws` as a WebSocket endpoint supporting bidirectional JSON messages for agent output streaming, agent list updates, prompt state changes, and input forwarding.

#### Scenario: WebSocket connection established
- **WHEN** a client connects to `GET /ws`
- **THEN** the connection is upgraded to WebSocket and the client receives an initial `agent_list` message

#### Scenario: Agent list broadcast on change
- **WHEN** an IPC message is received indicating agent state change
- **THEN** all connected WebSocket clients receive an `agents_changed` message

### Requirement: WebSocket agent output subscription
The server SHALL support per-agent output subscriptions via WebSocket. Subscribing to an agent starts a capture-pane polling goroutine that streams terminal output to the client.

#### Scenario: Subscribe to agent output
- **WHEN** a client sends `{type: "subscribe_agent", agent_id: "abc123"}`
- **THEN** the server begins polling `tmux capture-pane` for that agent at 200ms intervals and sends `agent_output` messages with content

#### Scenario: Initial full output on subscribe
- **WHEN** a client first subscribes to an agent
- **THEN** the first `agent_output` message has `full: true` and contains the complete current pane content

#### Scenario: Incremental updates after subscribe
- **WHEN** pane content changes between captures
- **THEN** the server sends an `agent_output` message with `full: false` containing only new content

#### Scenario: No message when content unchanged
- **WHEN** pane content has not changed since last capture
- **THEN** no `agent_output` message is sent

#### Scenario: Unsubscribe from agent output
- **WHEN** a client sends `{type: "unsubscribe_agent", agent_id: "abc123"}`
- **THEN** the server stops the capture-pane polling goroutine for that agent for this client

### Requirement: WebSocket send-keys forwarding
The server SHALL accept `send_keys` messages over WebSocket and forward them to the target tmux session.

#### Scenario: Send keys to agent
- **WHEN** a client sends `{type: "send_keys", agent_id: "abc123", keys: "y\n"}`
- **THEN** the server calls `tmux send-keys` on the corresponding session with the provided keys

#### Scenario: Send keys to non-existent agent
- **WHEN** a client sends `send_keys` for an agent that is not alive
- **THEN** the server responds with an error message over WebSocket

### Requirement: Server accepts service dependencies via constructor
The server constructor SHALL accept `BoardService`, `AgentService`, and `TmuxManager` interfaces to access task data, agent data, and tmux operations.

#### Scenario: Server created with all dependencies
- **WHEN** `New()` is called with board service, agent service, and tmux manager
- **THEN** the server is ready to handle all API and WebSocket requests

#### Scenario: Server created without agent service
- **WHEN** `New()` is called with nil agent service
- **THEN** agent-related endpoints return empty results and WebSocket agent subscriptions are rejected gracefully
