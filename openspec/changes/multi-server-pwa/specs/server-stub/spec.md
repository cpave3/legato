## ADDED Requirements

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
