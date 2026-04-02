## ADDED Requirements

### Requirement: React app shell with routing
The web UI SHALL be a single-page React application with client-side routing between pages.

#### Scenario: Navigation between pages
- **WHEN** the user clicks "Agents" in the navigation
- **THEN** the browser navigates to `/agents` without a full page reload

#### Scenario: Direct URL access
- **WHEN** the user navigates directly to `/agents` in the browser
- **THEN** the agents page renders (server returns index.html, React router handles the path)

### Requirement: Navigation sidebar or header
The app shell SHALL include a navigation element with links to Agents and Board pages, showing the active page.

#### Scenario: Navigation shows current page
- **WHEN** the user is on the agents page
- **THEN** the "Agents" navigation item is visually highlighted as active

### Requirement: Board page stub
The web UI SHALL include a board page at `/board` that displays a placeholder indicating the kanban board is not yet implemented.

#### Scenario: Board stub page
- **WHEN** the user navigates to `/board`
- **THEN** the page displays a "Board — Coming Soon" placeholder

### Requirement: WebSocket connection status indicator
The app shell SHALL display a connection status indicator showing whether the WebSocket connection to the server is active.

#### Scenario: Connected state
- **WHEN** the WebSocket connection is established
- **THEN** the indicator shows a green "Connected" status

#### Scenario: Disconnected state
- **WHEN** the WebSocket connection drops
- **THEN** the indicator shows a red "Disconnected" status

#### Scenario: Auto-reconnect
- **WHEN** the WebSocket connection drops
- **THEN** the client attempts to reconnect with exponential backoff (1s, 2s, 4s, max 30s)

### Requirement: Default route
The root path `/` SHALL redirect to `/agents`.

#### Scenario: Root redirect
- **WHEN** the user navigates to `/`
- **THEN** they are redirected to `/agents`
