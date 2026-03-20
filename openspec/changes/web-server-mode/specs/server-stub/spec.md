## MODIFIED Requirements

### Requirement: Server wraps BoardService

The server SHALL consume the `BoardService` interface for data access and MAY additionally consume `SyncService`, `AgentService`, and `*events.Bus` for extended functionality. It MUST NOT import any TUI packages or access the database directly. Services beyond `BoardService` SHALL be optional (nil-safe).

#### Scenario: Server uses service layer

- **WHEN** the server handles any API request
- **THEN** it calls the appropriate service interface methods, not direct database queries

#### Scenario: Server without optional services

- **WHEN** the server is created with only `BoardService` (sync, agents, bus are nil)
- **THEN** it starts successfully; sync and agent endpoints return 404; WebSocket endpoint returns 404

### Requirement: Server is independently startable

The server SHALL be startable as a standalone process or from a test, binding to a configurable address and port. It MUST NOT require the TUI to be running. The server SHALL accept a `ServerOptions` struct for configuration instead of positional constructor parameters.

#### Scenario: Start server in test

- **WHEN** a test creates a server instance with a `ServerOptions` struct and starts it
- **THEN** the server binds to the configured address and responds to HTTP requests

#### Scenario: Server runs without TUI

- **WHEN** the server is started without any TUI components initialized
- **THEN** it starts successfully and serves requests using only the service layer
