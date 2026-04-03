## ADDED Requirements

### Requirement: Health check endpoint

The server stub SHALL expose a `GET /health` endpoint that returns the current board state as JSON. The response MUST include all columns and their cards. The health endpoint SHALL be exempt from auth token validation to allow unauthenticated monitoring.

#### Scenario: Health check returns board state

- **WHEN** a GET request is made to `/health`
- **THEN** the server responds with HTTP 200 and a JSON body containing the list of columns, each with their cards (ticket key, summary, status)

#### Scenario: Health check with empty board

- **WHEN** a GET request is made to `/health` and no tickets exist in the database
- **THEN** the server responds with HTTP 200 and a JSON body containing empty column lists

#### Scenario: Health check without auth token

- **WHEN** a GET request is made to `/health` without an Authorization header
- **THEN** the server SHALL respond normally with HTTP 200 (health is exempt from auth)

### Requirement: Server wraps BoardService

The server stub SHALL consume the `BoardService` interface for data access. It MUST NOT import any TUI packages or access the database directly.

#### Scenario: Server uses service layer

- **WHEN** the server handles a `/health` request
- **THEN** it calls `BoardService.ListColumns` and `BoardService.ListCards` to assemble the response, not direct database queries

### Requirement: Server is independently startable

The server SHALL be startable as a standalone process or from a test, binding to a configurable address and port. It MUST NOT require the TUI to be running.

#### Scenario: Start server in test

- **WHEN** a test creates a server instance with a `BoardService` implementation and starts it
- **THEN** the server binds to the configured address and responds to HTTP requests

#### Scenario: Server runs without TUI

- **WHEN** the server is started without any TUI components initialized
- **THEN** it starts successfully and serves requests using only the service layer

### Requirement: JSON response format

The `/health` endpoint response MUST use a well-defined JSON structure with `status`, `columns`, and `synced_at` fields.

#### Scenario: Response structure

- **WHEN** a GET request is made to `/health`
- **THEN** the JSON response contains a `"status"` field (string, e.g. "ok"), a `"columns"` field (array of column objects with `name` and `cards`), and a `"synced_at"` field (ISO 8601 timestamp or null)

### Requirement: CORS headers on all responses

The server SHALL include CORS headers on all HTTP responses to allow the PWA to make cross-origin requests from other legato instances. The `Access-Control-Allow-Origin` header SHALL be set to `*`. The `Access-Control-Allow-Methods` header SHALL include `GET, POST, OPTIONS`. The `Access-Control-Allow-Headers` header SHALL include `Content-Type`.

#### Scenario: Preflight OPTIONS request

- **WHEN** a browser sends an OPTIONS preflight request to any endpoint
- **THEN** the server SHALL respond with HTTP 204, the CORS headers, and an empty body

#### Scenario: Cross-origin GET request

- **WHEN** the PWA at `https://desktop:3080` makes a GET request to `https://laptop:3080/api/agents`
- **THEN** the response SHALL include `Access-Control-Allow-Origin: *` and the normal JSON body

#### Scenario: WebSocket upgrade is unaffected

- **WHEN** a WebSocket upgrade request arrives
- **THEN** the existing `InsecureSkipVerify: true` on `websocket.Accept` SHALL continue to allow any origin. CORS middleware SHALL not interfere with the upgrade.
