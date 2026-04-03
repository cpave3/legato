## ADDED Requirements

### Requirement: Auto-generated auth token
The system SHALL generate a cryptographically random auth token on first server start if one does not already exist. The token SHALL be 32 bytes from `crypto/rand`, hex-encoded to 64 characters, and stored at `<dataDir>/auth-token`.

#### Scenario: First run generates token
- **WHEN** the server starts and `<dataDir>/auth-token` does not exist
- **THEN** the system SHALL generate a new random token, write it to `<dataDir>/auth-token`, and use it for authentication

#### Scenario: Subsequent runs reuse token
- **WHEN** the server starts and `<dataDir>/auth-token` already exists
- **THEN** the system SHALL read and use the existing token

### Requirement: Server-side auth middleware
The server SHALL reject all HTTP requests that do not include a valid auth token. The token MUST be provided via `Authorization: Bearer <token>` header for REST requests, or `?token=<token>` query parameter for WebSocket upgrade requests.

#### Scenario: Valid token on REST request
- **WHEN** a request includes `Authorization: Bearer <valid-token>`
- **THEN** the server SHALL process the request normally

#### Scenario: Missing token on REST request
- **WHEN** a request to a protected endpoint has no Authorization header
- **THEN** the server SHALL respond with HTTP 401 Unauthorized

#### Scenario: Invalid token on REST request
- **WHEN** a request includes `Authorization: Bearer <wrong-token>`
- **THEN** the server SHALL respond with HTTP 401 Unauthorized

#### Scenario: Valid token on WebSocket upgrade
- **WHEN** a WebSocket upgrade request includes `?token=<valid-token>` in the URL
- **THEN** the server SHALL accept the upgrade and establish the connection

#### Scenario: Missing or invalid token on WebSocket upgrade
- **WHEN** a WebSocket upgrade request has no token or an invalid token
- **THEN** the server SHALL reject the upgrade with HTTP 401

#### Scenario: Health endpoint is exempt
- **WHEN** a GET request is made to `/health` without a token
- **THEN** the server SHALL respond normally (health check is public for monitoring)

#### Scenario: OPTIONS preflight is exempt
- **WHEN** an OPTIONS request is made without a token
- **THEN** the server SHALL respond with CORS headers (preflight is unauthenticated)

### Requirement: Client-side token storage and prompt
The web UI SHALL store auth tokens per server in localStorage under `legato:token:<serverUrl>`. When a server responds with 401, the UI SHALL display a token input prompt. On successful authentication, the token SHALL be stored and included in all subsequent requests.

#### Scenario: First connection to unauthenticated server
- **WHEN** the web UI connects to a server and receives a 401 response
- **THEN** the UI SHALL display a modal prompting the user to enter the auth token

#### Scenario: Token submitted successfully
- **WHEN** the user enters a valid token in the prompt
- **THEN** the token SHALL be stored in `legato:token:<serverUrl>` and the UI SHALL retry the connection with the token

#### Scenario: Stored token used on subsequent visits
- **WHEN** the web UI connects to a server that has a stored token
- **THEN** all requests SHALL include the stored token automatically without prompting

#### Scenario: Stored token becomes invalid
- **WHEN** a request with a stored token receives a 401 response
- **THEN** the UI SHALL clear the stored token and re-display the token prompt

### Requirement: QR code pairing via CLI
The system SHALL provide a `legato pair` CLI command that renders a QR code in the terminal. The QR code SHALL encode a URI containing the server URL and auth token: `legato://pair?url=<serverUrl>&token=<token>`. The server URL SHALL be derived from the web server's configured address and TLS settings.

#### Scenario: Pair command renders QR
- **WHEN** the user runs `legato pair`
- **THEN** the terminal SHALL display a QR code encoding the pair URI, plus the raw token as text below for copy-paste fallback

#### Scenario: Pair command with custom port
- **WHEN** the user runs `legato pair --port 3080`
- **THEN** the QR code SHALL encode the server URL with the specified port

#### Scenario: Pair command detects TLS
- **WHEN** the server is configured with TLS
- **THEN** the QR code URL SHALL use the `https` scheme

### Requirement: QR code scanning in PWA
The web UI SHALL provide a "Scan QR" option in the add-server flow that opens the device camera and decodes QR codes. When a valid `legato://pair` URI is scanned, the server SHALL be added to the server list and authenticated in one step.

#### Scenario: Successful QR scan
- **WHEN** the user scans a valid `legato://pair?url=...&token=...` QR code
- **THEN** the server SHALL be added to the server list with the URL and name derived from the hostname, and the token SHALL be stored in `legato:token:<url>`

#### Scenario: Camera permission denied
- **WHEN** the user denies camera access
- **THEN** the UI SHALL fall back to the manual token entry flow with a message explaining the alternative

#### Scenario: Invalid QR code scanned
- **WHEN** a QR code is scanned that does not contain a valid `legato://pair` URI
- **THEN** the UI SHALL display an error message and continue scanning

### Requirement: Token display and regeneration via CLI
The system SHALL provide CLI commands to display and regenerate the auth token.

#### Scenario: Display current token
- **WHEN** the user runs `legato auth token`
- **THEN** the raw token SHALL be printed to stdout

#### Scenario: Regenerate token
- **WHEN** the user runs `legato auth regenerate`
- **THEN** a new random token SHALL be generated, written to `<dataDir>/auth-token`, and the old token SHALL be immediately invalid. All previously paired devices MUST re-authenticate.
