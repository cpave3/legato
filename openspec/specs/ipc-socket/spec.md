## ADDED Requirements

### Requirement: Unix domain socket server
The system SHALL start a Unix domain socket server when the TUI launches. The server SHALL listen for incoming JSON messages from CLI clients and translate them into event bus publications.

#### Scenario: TUI starts socket server on launch
- **WHEN** the Legato TUI starts
- **THEN** it SHALL create a Unix domain socket at a deterministic path based on the database location
- **AND** it SHALL begin accepting connections

#### Scenario: TUI cleans up socket on exit
- **WHEN** the Legato TUI exits normally
- **THEN** it SHALL remove the socket file from the filesystem

#### Scenario: Stale socket from crashed instance
- **WHEN** the TUI starts
- **AND** a socket file already exists at the expected path
- **THEN** it SHALL attempt to connect to the existing socket
- **AND** if the connection is refused (no listener), it SHALL remove the stale socket and create a new one
- **AND** if the connection succeeds (another instance is running), it SHALL log a warning and continue without starting a socket server

### Requirement: Socket path resolution
The system SHALL resolve the socket path using `$XDG_RUNTIME_DIR/legato/legato.sock`. If `$XDG_RUNTIME_DIR` is not set, it SHALL fall back to `/tmp/legato-<uid>/legato.sock`.

#### Scenario: XDG_RUNTIME_DIR is set
- **WHEN** `$XDG_RUNTIME_DIR` is set to `/run/user/1000`
- **THEN** the socket path SHALL be `/run/user/1000/legato/legato.sock`

#### Scenario: XDG_RUNTIME_DIR is not set
- **WHEN** `$XDG_RUNTIME_DIR` is not set
- **THEN** the socket path SHALL be `/tmp/legato-<uid>/legato.sock` where `<uid>` is the current user's numeric UID

### Requirement: Message protocol
The socket SHALL accept newline-delimited JSON messages. Each message SHALL have a `type` field identifying the operation.

#### Scenario: Task update message
- **WHEN** the socket server receives `{"type":"task_update","task_id":"abc123","status":"done"}`
- **THEN** it SHALL publish an `EventCardUpdated` event on the event bus with the task ID as payload
- **AND** the TUI SHALL reactively refresh the board

#### Scenario: Task note message
- **WHEN** the socket server receives `{"type":"task_note","task_id":"abc123","message":"some note"}`
- **THEN** it SHALL publish an `EventCardUpdated` event on the event bus

#### Scenario: Malformed message
- **WHEN** the socket server receives invalid JSON
- **THEN** it SHALL log the error and continue listening (not crash)

### Requirement: IPC client for CLI
The system SHALL provide an IPC client that CLI subcommands use to notify the running TUI instance of state changes.

#### Scenario: CLI sends update to running TUI
- **WHEN** the CLI client sends a message to the socket
- **AND** a TUI instance is listening
- **THEN** the message SHALL be delivered and processed

#### Scenario: CLI sends update with no running TUI
- **WHEN** the CLI client attempts to connect to the socket
- **AND** no TUI instance is running (socket does not exist or connection refused)
- **THEN** the client SHALL return silently without error (the database update already happened; IPC notification is best-effort)

### Requirement: Socket server runs in engine layer
The socket server and client SHALL be implemented in `internal/engine/ipc/` to respect the layered architecture. The server accepts raw JSON messages and calls a callback function; the TUI wires this callback to the event bus.

#### Scenario: Engine layer has no service imports
- **WHEN** the IPC package is compiled
- **THEN** it SHALL not import any packages from `internal/service/` or `internal/tui/`

### Requirement: Concurrent connection handling
The socket server SHALL handle multiple simultaneous CLI connections without blocking.

#### Scenario: Two hooks fire simultaneously
- **WHEN** two hook scripts connect to the socket at the same time
- **AND** each sends a task update message
- **THEN** both messages SHALL be processed without data loss or deadlock
