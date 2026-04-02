## ADDED Requirements

### Requirement: serve subcommand
The system SHALL provide a `legato serve [--port <port>]` subcommand that starts the web server without launching the TUI.

#### Scenario: Start server with default port
- **WHEN** `legato serve` is executed
- **THEN** the system SHALL start the HTTP/WebSocket server on port 3000, print the URL to stdout, and block until interrupted

#### Scenario: Start server with custom port
- **WHEN** `legato serve --port 8080` is executed
- **THEN** the system SHALL start the server on port 8080

#### Scenario: Port already in use
- **WHEN** `legato serve` is executed and port 3000 is already in use
- **THEN** the system SHALL print an error to stderr and exit with code 1

#### Scenario: Graceful shutdown
- **WHEN** the server is running and receives SIGINT or SIGTERM
- **THEN** the system SHALL gracefully shut down the HTTP server and exit with code 0
