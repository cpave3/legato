## ADDED Requirements

### Requirement: WebSocket endpoint

The server SHALL expose a `GET /ws` endpoint that upgrades to a WebSocket connection. The server SHALL use `nhooyr.io/websocket` for the WebSocket implementation.

#### Scenario: Successful upgrade

- **WHEN** a client sends a WebSocket upgrade request to `/ws`
- **THEN** the server upgrades the connection and begins sending event messages

#### Scenario: Non-WebSocket request

- **WHEN** a regular HTTP GET request is made to `/ws`
- **THEN** the server responds with HTTP 400

### Requirement: Event bus bridge

The WebSocket handler SHALL subscribe to all event types on the `events.Bus` and forward them as JSON messages to all connected clients. Each message SHALL include `type` (event name) and `payload` (event data) fields, plus a `timestamp`.

#### Scenario: Card moved event

- **WHEN** a card is moved via the API or TUI and a WebSocket client is connected
- **THEN** the client receives `{"type": "card.moved", "payload": {...}, "timestamp": "..."}`

#### Scenario: Sync completed event

- **WHEN** a sync completes and a WebSocket client is connected
- **THEN** the client receives `{"type": "sync.completed", "payload": {...}, "timestamp": "..."}`

#### Scenario: IPC-triggered event

- **WHEN** a CLI hook (e.g., `legato agent state`) broadcasts via IPC and the web server receives it
- **THEN** all connected WebSocket clients receive the corresponding event

### Requirement: Multiple concurrent clients

The WebSocket hub SHALL support multiple simultaneous client connections. Each client SHALL have its own event bus subscription. Slow clients SHALL NOT block event delivery to other clients.

#### Scenario: Two clients connected

- **WHEN** two WebSocket clients are connected and a card is moved
- **THEN** both clients receive the card.moved event independently

#### Scenario: Slow client does not block

- **WHEN** one client stops reading messages and another is connected
- **THEN** the responsive client continues receiving events without delay

### Requirement: Client disconnect cleanup

When a WebSocket client disconnects, the server SHALL unsubscribe its event bus channels and clean up all associated resources.

#### Scenario: Client disconnects

- **WHEN** a WebSocket client closes the connection
- **THEN** the server calls `Bus.Unsubscribe()` for all subscribed channels and the goroutine exits

### Requirement: Reconnection support

The WebSocket endpoint SHALL support clients reconnecting after disconnection. On reconnection, the client receives events from that point forward (no replay). The client SHALL re-fetch full board state on reconnect to reconcile missed events.

#### Scenario: Client reconnects

- **WHEN** a client disconnects and reconnects to `/ws`
- **THEN** the server accepts the new connection and begins forwarding events from the current moment

### Requirement: WebSocket authentication

When bearer token auth is configured, the WebSocket upgrade request SHALL require the token via `Authorization` header or `token` query parameter (for browser clients that cannot set headers on WebSocket).

#### Scenario: Auth via query parameter

- **WHEN** a WebSocket client connects to `/ws?token=correct-token`
- **THEN** the connection is accepted

#### Scenario: Missing auth on WebSocket

- **WHEN** a WebSocket client connects to `/ws` without a token and auth is configured
- **THEN** the server rejects the upgrade with HTTP 401
