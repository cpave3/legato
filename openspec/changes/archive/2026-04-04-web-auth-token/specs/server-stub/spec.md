## MODIFIED Requirements

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
