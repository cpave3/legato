## ADDED Requirements

### Requirement: Creating and revoking integration tokens
`legato integration token create <name> --scope <scopes>` SHALL create a bearer token of the form `lgi_<base32>` from 32 random bytes, print it once, and store only its SHA-256 hash with the name and scopes. The scopes SHALL be a non-empty subset of `owner:read` and `delegate`; an unknown scope SHALL fail creation. A name SHALL match `[a-z0-9-]{1,40}` and be unique among tokens that are not revoked. `legato integration token list` SHALL show names, scopes and dates and never a token. `legato integration token revoke <name>` SHALL make the token stop working at once.

#### Scenario: Token printed once
- **WHEN** the person creates the token `argus` with scopes `owner:read,delegate`
- **THEN** the command SHALL print a token starting `lgi_`, the database SHALL hold only its hash, and `token list` SHALL show `argus` with both scopes and no token

#### Scenario: Unknown scope
- **WHEN** the person creates a token with scope `tasks:write`
- **THEN** the command SHALL fail and no token SHALL be stored

#### Scenario: Revoked token refused
- **WHEN** the token `argus` is revoked and a request then presents it
- **THEN** the request SHALL get 401 `unauthorized`

### Requirement: Where a scoped token is accepted
The web server SHALL accept a valid integration token only on routes under `/api/integrations/`, and only when the token holds the route's scope. Elsewhere it SHALL answer 403 `forbidden`; on an integration route without the scope, 403 `missing_scope`. The WebSocket SHALL NOT accept an integration token. The full web auth token SHALL keep working on every route, the integration routes included. A request authenticated with an integration token SHALL carry the integration's name in its context.

#### Scenario: Scoped token on the board
- **WHEN** a request to `GET /api/board` presents a valid integration token
- **THEN** it SHALL get 403 with `{"error": "forbidden"}`

#### Scenario: Missing scope
- **WHEN** a token with only `owner:read` posts to `/api/integrations/delegations`
- **THEN** it SHALL get 403 with `{"error": "missing_scope"}`

#### Scenario: Full token on an integration route
- **WHEN** a request to `GET /api/integrations/owner` presents the web auth token
- **THEN** it SHALL be served

### Requirement: Integration error shape
Every error from a route under `/api/integrations/`, and every authentication error for one, SHALL be JSON `{"error": "<code>", "message": "<text>"}` with the codes and statuses of the design's error table.

#### Scenario: Error with a code
- **WHEN** a delegation names a repository that is not in the catalog
- **THEN** the answer SHALL be 422 with `error` `unknown_repo` and a message naming the repository
