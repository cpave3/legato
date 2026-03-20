## ADDED Requirements

### Requirement: Serve subcommand

The `legato serve` subcommand SHALL start the HTTP server without launching the TUI. It SHALL load configuration, initialize the database, create services (board, sync, agent), start the IPC listener, and bind the HTTP server.

#### Scenario: Start server

- **WHEN** a user runs `legato serve`
- **THEN** the server starts on the default address (`:8080`) and logs the listening address to stdout

#### Scenario: Custom bind address

- **WHEN** a user runs `legato serve --addr :3000`
- **THEN** the server binds to port 3000

#### Scenario: Address from config

- **WHEN** `server.addr` is set to `:9090` in config.yaml and no `--addr` flag is provided
- **THEN** the server binds to port 9090

### Requirement: Service wiring matches TUI

The serve command SHALL initialize the same services as the TUI: `BoardService`, `SyncService` (if Jira configured), `AgentService` (if tmux available), and the event bus. The sync scheduler SHALL run if a provider is configured.

#### Scenario: Serve with Jira configured

- **WHEN** `legato serve` is run with Jira credentials in config
- **THEN** the sync service starts, runs an initial sync, and begins the periodic scheduler

#### Scenario: Serve without Jira

- **WHEN** `legato serve` is run without Jira configuration
- **THEN** the server starts without sync capabilities; sync-related API endpoints return 404

### Requirement: IPC reception

The serve command SHALL create an IPC socket (same pattern as TUI) and listen for messages from CLI commands. IPC messages SHALL be published to the event bus, which forwards them to WebSocket clients.

#### Scenario: CLI hook triggers web update

- **WHEN** `legato agent state <task-id> --activity working` is run while the web server is running
- **THEN** the IPC message is received, published to the event bus, and forwarded to all WebSocket clients

### Requirement: Graceful shutdown

The serve command SHALL handle `SIGINT` and `SIGTERM` signals by gracefully shutting down the HTTP server (draining connections), stopping the sync scheduler, closing the IPC socket, and closing the database.

#### Scenario: Ctrl+C shutdown

- **WHEN** a user presses Ctrl+C while `legato serve` is running
- **THEN** the server logs "shutting down...", drains active connections (5s timeout), and exits cleanly

### Requirement: Server configuration section

The config file SHALL support a `server` section with fields: `addr` (string, default `:8080`), `auth_token` (string, optional), `cors_origin` (string, default `*`).

#### Scenario: Full server config

- **WHEN** config.yaml contains `server: { addr: ":3000", auth_token: "${LEGATO_AUTH_TOKEN}", cors_origin: "http://localhost:5173" }`
- **THEN** the server uses these values (with env var expansion for auth_token)
