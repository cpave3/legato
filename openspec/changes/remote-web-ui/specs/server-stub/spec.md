## MODIFIED Requirements

### Requirement: Server wraps BoardService

The server stub SHALL consume the `BoardService`, `AgentService`, and `TmuxManager` interfaces for data access. It MUST NOT import any TUI packages or access the database directly. `AgentService` and `TmuxManager` are optional (nil-safe) — agent endpoints return empty results when unavailable.

#### Scenario: Server uses service layer
- **WHEN** the server handles a `/health` request
- **THEN** it calls `BoardService.ListColumns` and `BoardService.ListCards` to assemble the response, not direct database queries

#### Scenario: Server uses agent service
- **WHEN** the server handles a `/api/agents` request
- **THEN** it calls `AgentService.ListAgents` to assemble the response

#### Scenario: Server without agent service
- **WHEN** the server is created with nil agent service
- **THEN** agent endpoints return empty arrays and WebSocket agent subscriptions are rejected with an error message

### Requirement: Server is independently startable

The server SHALL be startable as a standalone process or from a test, binding to a configurable address and port. It MUST NOT require the TUI to be running. When started standalone, it SHALL create its own IPC server socket to receive events from CLI commands.

#### Scenario: Start server in test
- **WHEN** a test creates a server instance with a `BoardService` implementation and starts it
- **THEN** the server binds to the configured address and responds to HTTP requests

#### Scenario: Server runs without TUI
- **WHEN** the server is started without any TUI components initialized
- **THEN** it starts successfully and serves requests using only the service layer

#### Scenario: Standalone server receives IPC
- **WHEN** the server is started via `legato serve` and a CLI command broadcasts an IPC message
- **THEN** the server receives the message and fans it out to connected WebSocket clients
