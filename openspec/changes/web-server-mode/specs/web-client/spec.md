## ADDED Requirements

### Requirement: Embedded SPA serving

The server SHALL serve a single-page application from embedded static assets (`web/dist/`) at the root path `/`. All paths not matching `/api/`, `/ws`, or `/health` SHALL serve `index.html` for client-side routing.

#### Scenario: Load web client

- **WHEN** a browser navigates to `http://localhost:8080/`
- **THEN** the server responds with the SPA's `index.html`

#### Scenario: Client-side routing fallback

- **WHEN** a browser navigates to `http://localhost:8080/board`
- **THEN** the server responds with `index.html` (not 404), allowing the SPA router to handle the path

#### Scenario: Static assets

- **WHEN** a browser requests `/app.js` or `/style.css`
- **THEN** the server responds with the correct file from the embedded assets with appropriate Content-Type

### Requirement: Kanban board view

The web client SHALL display a kanban board with columns and cards matching the TUI layout. Columns SHALL be displayed horizontally with their cards listed vertically. Cards SHALL show title, priority indicator, provider icon, and agent state.

#### Scenario: Board renders columns and cards

- **WHEN** the web client loads and fetches `/api/v1/board`
- **THEN** it renders all columns with their cards in a horizontal layout

#### Scenario: Empty column

- **WHEN** a column has no cards
- **THEN** the column header is displayed with an empty card area

### Requirement: Card detail view

The web client SHALL support viewing card details by clicking a card. The detail view SHALL display the card title, rendered markdown description, metadata (provider, remote ID, priority, timestamps), and remote metadata fields.

#### Scenario: Open card detail

- **WHEN** a user clicks on a card in the board view
- **THEN** the client fetches `/api/v1/cards/:id` and displays the detail view with rendered markdown

#### Scenario: Close card detail

- **WHEN** a user presses Escape or clicks a close button in the detail view
- **THEN** the client returns to the board view

### Requirement: Move card interaction

The web client SHALL support moving cards between columns via a move dialog (mirroring the TUI's move overlay). The user SHALL be able to select a target column from a list.

#### Scenario: Move card via dialog

- **WHEN** a user opens the move dialog for a card and selects "Done"
- **THEN** the client sends `PUT /api/v1/cards/:id/move` with `{"column": "Done"}` and the board updates

### Requirement: Create task interaction

The web client SHALL support creating new tasks via a creation form with title (required), column selector, and priority selector.

#### Scenario: Create task

- **WHEN** a user fills in "Fix login bug" as title, selects "Doing" column, and submits
- **THEN** the client sends `POST /api/v1/cards` and the new card appears on the board

### Requirement: Delete task interaction

The web client SHALL support deleting tasks with a confirmation dialog. For remote-tracking tasks, the dialog SHALL indicate that only the local reference is removed.

#### Scenario: Delete local task

- **WHEN** a user confirms deletion of a local task
- **THEN** the client sends `DELETE /api/v1/cards/:id` and the card is removed from the board

### Requirement: Search functionality

The web client SHALL support searching cards via a search input. Results SHALL be displayed as a filtered list with the ability to navigate to a result.

#### Scenario: Search cards

- **WHEN** a user types "auth" in the search input
- **THEN** the client fetches `/api/v1/cards/search?q=auth` and displays matching cards

### Requirement: Real-time updates via WebSocket

The web client SHALL connect to the `/ws` endpoint on load and update the board in real-time when events are received. On receiving a data-change event (card.moved, card.updated, cards.refreshed, sync.completed), the client SHALL re-fetch the board state.

#### Scenario: Board updates on card move

- **WHEN** another client (TUI or CLI) moves a card and a WebSocket event is received
- **THEN** the web client re-fetches the board and updates the display without a page reload

#### Scenario: WebSocket reconnection

- **WHEN** the WebSocket connection drops
- **THEN** the client automatically reconnects with exponential backoff and re-fetches the board state

### Requirement: Agent status display

The web client SHALL display agent status indicators on cards, mirroring the TUI's visual states: working (active indicator), waiting (idle indicator), and no activity.

#### Scenario: Agent working on task

- **WHEN** the board data shows a card with agent state "working"
- **THEN** the card displays a working indicator (e.g., pulsing dot or spinner)

### Requirement: Technology constraints

The web client SHALL be built with Preact and HTM tagged templates. The production build SHALL use esbuild for bundling. The total bundled size SHALL be under 200KB (uncompressed).

#### Scenario: Build produces valid assets

- **WHEN** the web client is built via `task build:web`
- **THEN** the output in `web/dist/` contains `index.html`, bundled JS, and CSS under 200KB total
