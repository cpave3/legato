## ADDED Requirements

### Requirement: Events through slog
Legato SHALL log through `log/slog`. A log line's message SHALL be an event name from the catalogue in `docs/ai/observability.md` (dotted, such as `agent.spawned`), and its meaning SHALL be in its fields. Event names SHALL be constants in one package. Production code outside command output SHALL NOT call `log.Printf`, `log.Println` or write to standard error directly; a test SHALL fail when it does. Output of the standard `log` package SHALL be carried into slog as `log.legacy` at `info`.

#### Scenario: A legacy call still arrives structured
- **WHEN** a dependency writes with the standard `log` package
- **THEN** the line SHALL be logged as event `log.legacy` with the text in a `text` field, in the configured format

#### Scenario: Architecture test
- **WHEN** a non-test file under `internal/` calls `log.Printf`
- **THEN** the architecture test SHALL fail and name the file

### Requirement: Output, format and level
`observability.log.level` SHALL be `debug`, `info`, `warn` or `error` (default `info`). `observability.log.format` SHALL be `json`, `text` or `auto` (default `auto`: JSON when standard error is not a terminal). `legato serve` SHALL log to standard error. The TUI SHALL log to `observability.log.file` (default `<XDG state dir>/legato/legato.log`), rotated at 20 MB with 3 old files kept, and SHALL NOT write to the terminal. Short-lived commands (hooks, CLI commands, `mcp serve`, bridges) SHALL log at `warn` and above to standard error. An invalid value SHALL fail configuration loading with a message naming the setting.

#### Scenario: Under systemd
- **WHEN** `legato serve` runs with standard error connected to journald and `format: auto`
- **THEN** each line SHALL be one JSON object with `time`, `level`, `msg` (the event name) and the event's fields

#### Scenario: TUI
- **WHEN** the TUI runs
- **THEN** its events SHALL be written to the log file and nothing SHALL be written to the terminal

#### Scenario: Rotation
- **WHEN** the TUI's log file passes 20 MB
- **THEN** it SHALL be renamed with a suffix, a new file started, and only the 3 newest old files kept

### Requirement: Context fields and request IDs
A logger carried in a context SHALL add the context's fields to every line: `request_id`, `client`, `task_id`, `execution_id`, `agent_kind`, `approval_id`, `workflow_run_id` when set. Every API request and WebSocket connection SHALL get a `request_id` (12 hex characters, or a well-formed `X-Request-ID` sent by the client), returned in the `X-Request-ID` response header. The `client` SHALL say how the request was authenticated (`auth:token`, `device:<id>`, `broker:<execution id>`, `unauthenticated`). The remote address SHALL be the connection's, or the forwarded one when the connection comes from an address in `observability.trusted_proxies`.

#### Scenario: Spawn from the web
- **WHEN** a request to `POST /api/tasks/{id}/agent` starts an agent
- **THEN** the `http.request` line and the `agent.spawned` line SHALL carry the same `request_id`

#### Scenario: Forwarded address from an untrusted peer
- **WHEN** a request carries `X-Forwarded-For` from an address not in `trusted_proxies`
- **THEN** the logged remote address SHALL be the connection's

### Requirement: Redaction
Every line SHALL pass through a redaction handler before it is written. The handler SHALL replace, in the message and every string field, each secret value the process knows (the web auth token, broker tokens, credential values it has read, placeholders' real values, the 1Password service account token) with `[redacted]`. It SHALL replace the value of any field whose key names a secret (`token`, `secret`, `password`, `authorization`, `cookie`). Values SHALL be logged by typed fields, never by formatting a whole structure.

#### Scenario: A known secret in an error
- **WHEN** an error whose text contains the web auth token is logged
- **THEN** the written line SHALL contain `[redacted]` in place of the token

#### Scenario: A field named token
- **WHEN** an event is logged with a field `token`
- **THEN** its value SHALL be written as `[redacted]`
