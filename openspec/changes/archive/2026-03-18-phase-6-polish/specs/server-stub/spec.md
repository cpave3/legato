## ADDED Requirements

### Requirement: Health check endpoint

The server stub SHALL expose a `GET /health` endpoint that returns the current board state as JSON. The response MUST include all columns and their cards.

#### Scenario: Health check returns board state

- **WHEN** a GET request is made to `/health`
- **THEN** the server responds with HTTP 200 and a JSON body containing the list of columns, each with their cards (ticket key, summary, status)

#### Scenario: Health check with empty board

- **WHEN** a GET request is made to `/health` and no tickets exist in the database
- **THEN** the server responds with HTTP 200 and a JSON body containing empty column lists

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
