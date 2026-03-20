## ADDED Requirements

### Requirement: Board state endpoint

The server SHALL expose a `GET /api/v1/board` endpoint that returns all columns with their cards. The response MUST include column names, sort orders, and nested card arrays with id, title, priority, status, provider, issue type, warning state, and agent state.

#### Scenario: Full board state

- **WHEN** a GET request is made to `/api/v1/board`
- **THEN** the server responds with HTTP 200 and a JSON body containing all columns, each with their cards sorted by sort_order

#### Scenario: Board with no cards

- **WHEN** a GET request is made to `/api/v1/board` and no tasks exist
- **THEN** the server responds with HTTP 200 and columns with empty card arrays

### Requirement: Card detail endpoint

The server SHALL expose a `GET /api/v1/cards/:id` endpoint that returns full card detail including description markdown, remote metadata, timestamps, and all fields from `CardDetail`.

#### Scenario: Get existing card

- **WHEN** a GET request is made to `/api/v1/cards/abc12345`
- **THEN** the server responds with HTTP 200 and the full card detail JSON

#### Scenario: Get non-existent card

- **WHEN** a GET request is made to `/api/v1/cards/nonexistent`
- **THEN** the server responds with HTTP 404 and `{"error": "card not found"}`

### Requirement: Create task endpoint

The server SHALL expose a `POST /api/v1/cards` endpoint that creates a local task. The request body MUST include `title` and MAY include `column` and `priority`. The response MUST return the created card.

#### Scenario: Create task with all fields

- **WHEN** a POST request is made to `/api/v1/cards` with body `{"title": "Fix bug", "column": "Doing", "priority": "high"}`
- **THEN** the server responds with HTTP 201 and the created card JSON including a generated 8-char ID

#### Scenario: Create task with title only

- **WHEN** a POST request is made to `/api/v1/cards` with body `{"title": "New task"}`
- **THEN** the server responds with HTTP 201, using default column and no priority

#### Scenario: Create task without title

- **WHEN** a POST request is made to `/api/v1/cards` with body `{}`
- **THEN** the server responds with HTTP 400 and `{"error": "title is required"}`

### Requirement: Delete task endpoint

The server SHALL expose a `DELETE /api/v1/cards/:id` endpoint that deletes a task. For remote-tracking tasks, this SHALL remove the local reference only.

#### Scenario: Delete existing task

- **WHEN** a DELETE request is made to `/api/v1/cards/abc12345`
- **THEN** the server responds with HTTP 204 and the task is removed from the store

#### Scenario: Delete non-existent task

- **WHEN** a DELETE request is made to `/api/v1/cards/nonexistent`
- **THEN** the server responds with HTTP 404

### Requirement: Move card endpoint

The server SHALL expose a `PUT /api/v1/cards/:id/move` endpoint that moves a card to a target column. The request body MUST include `column`.

#### Scenario: Move card to valid column

- **WHEN** a PUT request is made to `/api/v1/cards/abc12345/move` with body `{"column": "Done"}`
- **THEN** the server responds with HTTP 200 and the card is moved to the Done column

#### Scenario: Move card to invalid column

- **WHEN** a PUT request is made to `/api/v1/cards/abc12345/move` with body `{"column": "Nonexistent"}`
- **THEN** the server responds with HTTP 400 with an error message

### Requirement: Reorder card endpoint

The server SHALL expose a `PUT /api/v1/cards/:id/reorder` endpoint that sets a card's position within its column. The request body MUST include `position` (0-indexed).

#### Scenario: Reorder card

- **WHEN** a PUT request is made to `/api/v1/cards/abc12345/reorder` with body `{"position": 0}`
- **THEN** the server responds with HTTP 200 and the card is moved to position 0 in its column

### Requirement: Search cards endpoint

The server SHALL expose a `GET /api/v1/cards/search?q=<query>` endpoint that searches cards by title/content using `BoardService.SearchCards`.

#### Scenario: Search with results

- **WHEN** a GET request is made to `/api/v1/cards/search?q=bug`
- **THEN** the server responds with HTTP 200 and an array of matching cards

#### Scenario: Search with no results

- **WHEN** a GET request is made to `/api/v1/cards/search?q=xyznonexistent`
- **THEN** the server responds with HTTP 200 and an empty array

### Requirement: Trigger sync endpoint

The server SHALL expose a `POST /api/v1/sync` endpoint that triggers an immediate sync. This endpoint SHALL return 404 if no sync service is configured.

#### Scenario: Trigger sync with provider configured

- **WHEN** a POST request is made to `/api/v1/sync` and a sync service is available
- **THEN** the server triggers a sync and responds with HTTP 200 and the sync result

#### Scenario: Trigger sync without provider

- **WHEN** a POST request is made to `/api/v1/sync` and no sync service is configured
- **THEN** the server responds with HTTP 404 and `{"error": "sync not configured"}`

### Requirement: Sync status endpoint

The server SHALL expose a `GET /api/v1/sync/status` endpoint that returns the current sync state (in-progress, last sync time).

#### Scenario: Get sync status

- **WHEN** a GET request is made to `/api/v1/sync/status`
- **THEN** the server responds with HTTP 200 and `{"in_progress": false, "last_sync": "2026-03-21T10:00:00Z"}`

### Requirement: Search remote endpoint

The server SHALL expose a `GET /api/v1/sync/search?q=<query>` endpoint that searches the remote provider. Minimum 2-character query.

#### Scenario: Search remote tickets

- **WHEN** a GET request is made to `/api/v1/sync/search?q=auth`
- **THEN** the server responds with HTTP 200 and an array of remote search results

#### Scenario: Search with short query

- **WHEN** a GET request is made to `/api/v1/sync/search?q=a`
- **THEN** the server responds with HTTP 400 and `{"error": "query must be at least 2 characters"}`

### Requirement: Import remote task endpoint

The server SHALL expose a `POST /api/v1/sync/import/:id` endpoint that imports a remote ticket as a local task.

#### Scenario: Import remote ticket

- **WHEN** a POST request is made to `/api/v1/sync/import/REX-1234`
- **THEN** the server responds with HTTP 201 and the imported card JSON

### Requirement: List agents endpoint

The server SHALL expose a `GET /api/v1/agents` endpoint that returns all agent sessions with their task ID, activity state, and alive status. This endpoint is read-only.

#### Scenario: List agents

- **WHEN** a GET request is made to `/api/v1/agents`
- **THEN** the server responds with HTTP 200 and an array of agent session objects

### Requirement: JSON error responses

All API error responses SHALL use the format `{"error": "<message>"}` with appropriate HTTP status codes (400 for bad requests, 401 for auth failures, 404 for not found, 500 for server errors).

#### Scenario: Malformed JSON body

- **WHEN** a POST request is made with an invalid JSON body
- **THEN** the server responds with HTTP 400 and `{"error": "invalid request body"}`

### Requirement: Bearer token authentication

When `server.auth_token` is configured, all `/api/v1/*` and `/ws` endpoints SHALL require an `Authorization: Bearer <token>` header. The `/health` endpoint SHALL remain unauthenticated.

#### Scenario: Valid token

- **WHEN** a request includes `Authorization: Bearer correct-token` and the token matches config
- **THEN** the request proceeds normally

#### Scenario: Missing token when required

- **WHEN** a request to `/api/v1/board` omits the Authorization header and auth is configured
- **THEN** the server responds with HTTP 401 and `{"error": "unauthorized"}`

#### Scenario: No auth configured

- **WHEN** `server.auth_token` is empty or not set
- **THEN** all requests proceed without authentication checks

### Requirement: CORS headers

The server SHALL include CORS headers allowing cross-origin requests. The `Access-Control-Allow-Origin` header SHALL be configurable via `server.cors_origin` (default `*`).

#### Scenario: Preflight request

- **WHEN** an OPTIONS request is made to any API endpoint
- **THEN** the server responds with HTTP 204 and appropriate CORS headers (Allow-Origin, Allow-Methods, Allow-Headers)

#### Scenario: Regular request includes CORS

- **WHEN** a GET request is made to `/api/v1/board`
- **THEN** the response includes `Access-Control-Allow-Origin` header
