## ADDED Requirements

### Requirement: Fuzzy search activation

The system SHALL open a search overlay when the user presses `/` from the board view. The overlay MUST display a text input field at the top and a scrollable results list below it.

#### Scenario: Open search overlay

- **WHEN** the user presses `/` on the board view
- **THEN** a search overlay appears centered on a dimmed board background with a text input field focused and ready for typing

#### Scenario: Search overlay blocks board input

- **WHEN** the search overlay is active
- **THEN** all keyboard input (except `esc`) is routed to the search overlay, not the board

### Requirement: Real-time search filtering

The system SHALL filter tickets in real time as the user types in the search input. Matching MUST be case-insensitive substring matching across the ticket key, summary, and description fields.

#### Scenario: Filter by ticket key

- **WHEN** the user types "REX-12" into the search input
- **THEN** the results list shows only tickets whose key, summary, or description contain "REX-12"

#### Scenario: Filter by summary text

- **WHEN** the user types "refactor" into the search input
- **THEN** the results list shows all tickets whose key, summary, or description contain "refactor" (case-insensitive)

#### Scenario: Empty query shows no results

- **WHEN** the search input is empty
- **THEN** the results list is empty or shows a placeholder message

#### Scenario: No matches found

- **WHEN** the user types a query that matches no tickets
- **THEN** the results list shows a "no results" message

### Requirement: Search result selection

The system SHALL allow the user to navigate search results with `j`/`k` (or arrow keys) and select a result with `enter`. Selecting a result MUST navigate to that ticket on the board (moving the cursor to the ticket's column and position).

#### Scenario: Navigate and select a search result

- **WHEN** the user presses `j`/`k` to highlight a result and then presses `enter`
- **THEN** the search overlay closes, and the board cursor moves to the selected ticket's column and row

### Requirement: Search dismissal

The system SHALL close the search overlay and return to the board view when the user presses `esc`. The board state MUST remain unchanged.

#### Scenario: Dismiss search with escape

- **WHEN** the user presses `esc` while the search overlay is active
- **THEN** the overlay closes, the board is restored to its previous state, and no navigation occurs

### Requirement: Search uses BoardService

The search overlay SHALL call `BoardService.SearchCards` for filtering. The overlay MUST NOT query the database or Jira directly.

#### Scenario: Service layer integration

- **WHEN** the user types a query into the search input
- **THEN** the overlay calls `BoardService.SearchCards` with the query string and renders the returned results
