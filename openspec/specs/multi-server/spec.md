## ADDED Requirements

### Requirement: Server registry persistence
The system SHALL store a list of named servers in `localStorage` under the key `legato:servers` as a JSON array of `{name: string, url: string}` objects. The active server URL SHALL be stored under `legato:active-server`. An empty or absent active server value SHALL default to the PWA's origin.

#### Scenario: No servers configured
- **WHEN** `legato:servers` is absent or empty
- **THEN** the app SHALL use the origin URL for all API and WebSocket connections

#### Scenario: User adds a server
- **WHEN** user enters a name and URL in the server management UI and confirms
- **THEN** the entry SHALL be appended to the `legato:servers` array in localStorage

#### Scenario: User removes a server
- **WHEN** user deletes a server entry
- **THEN** the entry SHALL be removed from `legato:servers`. If the deleted server was active, the active server SHALL revert to the origin.

### Requirement: Active server selection
The system SHALL allow the user to switch the active server. Switching SHALL close the current WebSocket connection, update `legato:active-server`, and open a new WebSocket to the selected server. All REST API calls SHALL use the active server's base URL.

#### Scenario: Switch from origin to remote server
- **WHEN** user selects a remote server from the server list
- **THEN** the WebSocket SHALL disconnect from the origin and reconnect to the remote server's `/ws` endpoint. The agent list SHALL refresh from the remote server's `/api/agents`.

#### Scenario: Switch back to origin
- **WHEN** user selects "Local" (origin) from the server list
- **THEN** the WebSocket SHALL reconnect to the origin. API calls SHALL revert to relative URLs.

### Requirement: Dynamic base URL for all API calls
All `fetch()` calls and the WebSocket connection URL SHALL be derived from the active server's base URL. When the active server is the origin, relative URLs SHALL be used (preserving current behavior).

#### Scenario: REST call to remote server
- **WHEN** active server is `https://laptop.local:3080`
- **THEN** `fetch("/api/agents")` SHALL become `fetch("https://laptop.local:3080/api/agents")`

#### Scenario: WebSocket to remote server
- **WHEN** active server is `https://laptop.local:3080`
- **THEN** the WebSocket SHALL connect to `wss://laptop.local:3080/ws`

### Requirement: Server switcher UI
The system SHALL provide a server switcher accessible from the main sidebar (desktop) and from the Settings page. The sidebar SHALL show the active server name with a clickable indicator. The Settings page SHALL allow full CRUD operations on the server list.

#### Scenario: Sidebar switcher on desktop
- **WHEN** user clicks the active server indicator in the sidebar footer
- **THEN** a popover SHALL appear listing all configured servers plus "Local" (origin), with the active one highlighted

#### Scenario: Settings server management
- **WHEN** user navigates to Settings
- **THEN** a "Servers" section SHALL display all configured servers with name, URL, and delete option, plus an "Add server" form

### Requirement: Connection error feedback
The system SHALL show a visible error state when the active server is unreachable, with a hint about TLS trust if the connection fails.

#### Scenario: Remote server unreachable
- **WHEN** the WebSocket to the active server fails to connect
- **THEN** the offline overlay SHALL appear. If the server is not the origin, the error message SHALL include a suggestion to verify the server is running and TLS is trusted.
