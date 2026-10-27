## ADDED Requirements

### Requirement: Broker socket
A host Legato process (TUI or `legato serve`) SHALL listen on a Unix domain socket at `<runtime dir>/legato/broker-<pid>.sock` with mode `0600`. It SHALL accept newline-delimited JSON requests `{id, token, op, args}` and answer each with `{id, result}` or `{id, error: {code, message}}` on the same connection.

#### Scenario: Request and reply
- **WHEN** a client sends `{"id":1,"token":"<valid>","op":"task.context","args":{}}`
- **THEN** the broker SHALL reply with `{"id":1,"result":{...}}` on the same connection

#### Scenario: Malformed request
- **WHEN** a line is not valid JSON or lacks `op`
- **THEN** the broker SHALL reply with an error with code `bad_request` when an `id` can be read, and SHALL keep serving other connections

#### Scenario: Stale socket
- **WHEN** the broker starts and a socket file exists at its path with no listener
- **THEN** it SHALL remove the file and listen

### Requirement: Capability token per execution
When Legato spawns an agent whose execution may use the broker, it SHALL generate a random 32-byte token, store only its SHA-256 hash with the execution, and pass the token and socket path to the agent's environment as `LEGATO_BROKER_TOKEN` and `LEGATO_BROKER_SOCKET`. The broker SHALL resolve a token to exactly one task and execution and SHALL act only as that identity.

#### Scenario: Valid token
- **WHEN** a request carries the token of a running execution
- **THEN** the broker SHALL perform the operation as that execution's task and execution

#### Scenario: Request names another identity
- **WHEN** a request's arguments name a task ID or execution ID other than the token's
- **THEN** the broker SHALL reject it with code `forbidden` and SHALL perform nothing

#### Scenario: Token of a terminal execution
- **WHEN** a request carries the token of an execution that is `succeeded`, `failed`, `cancelled` or `lost`
- **THEN** the broker SHALL reject it with code `unauthorized`

#### Scenario: Token never stored in plain form
- **WHEN** the database is inspected after a spawn
- **THEN** it SHALL contain the token's hash and SHALL NOT contain the token

### Requirement: Broker operations
The broker SHALL offer operations that cover everything agent-side Legato processes do today, implemented by calling the same services those processes use: `agent.state`, `agent.stopped`, `agent.session_created`, `agent.permission_resolved`, `endpoint.bind`, `endpoint.unbind`, `resume.save`, `ui.show`, `task.context`, `task.note`, `peers.list`, `peers.message`, `peers.broadcast`, and the git operations of `git-broker-tools`.

#### Scenario: Activity through the broker
- **WHEN** a hook calls `agent.state` with activity `working`
- **THEN** the agent session's activity and execution activity SHALL change exactly as `legato agent state` changes them today
- **AND** running Legato instances SHALL be notified as today

#### Scenario: Showing an image file
- **WHEN** `ui.show` carries an image as inline content of at most 25 MiB
- **THEN** the broker SHALL store the item and its image copy under the same checks as `AgentUIService`

#### Scenario: Unknown operation
- **WHEN** `op` names no operation
- **THEN** the broker SHALL reply with code `unknown_op`

### Requirement: Agent-side processes choose broker or store
The activity hook commands, `legato mcp serve`, `legato agent bridge` and `legato agent codex-bridge` SHALL use the broker when `LEGATO_BROKER_SOCKET` and `LEGATO_BROKER_TOKEN` are both set, and SHALL then neither open the store nor read the configuration file. When either is unset they SHALL behave as before this change.

#### Scenario: Sandboxed session
- **WHEN** `legato agent state` runs with both broker variables set
- **THEN** it SHALL send `agent.state` to the broker
- **AND** SHALL NOT open the database file or read `config.yaml`

#### Scenario: Unsandboxed session
- **WHEN** the broker variables are unset
- **THEN** the command SHALL open the store as it does today

#### Scenario: Broker unreachable from a hook
- **WHEN** a hook command cannot reach the broker within 2 seconds
- **THEN** it SHALL write a diagnostic to stderr and exit 0, so the agent tool's turn is not failed

#### Scenario: Broker unreachable from an MCP tool
- **WHEN** an MCP tool call cannot reach the broker
- **THEN** the tool SHALL return a tool error that says Legato cannot be reached
