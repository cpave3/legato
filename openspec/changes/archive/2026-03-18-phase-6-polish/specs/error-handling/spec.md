## ADDED Requirements

### Requirement: Offline mode on startup

When the network is unavailable at startup, the system SHALL load board state from SQLite and display "offline" in the status bar. The system MUST retry sync on the configured interval.

#### Scenario: No network on startup

- **WHEN** Legato starts with no network connectivity
- **THEN** the board loads from SQLite, the status bar shows "offline", and sync retries automatically on the configured interval

#### Scenario: Network recovers after offline startup

- **WHEN** the network becomes available after an offline startup
- **THEN** the next sync interval succeeds, the status bar updates to show sync status, and the board refreshes with any new data from Jira

### Requirement: Jira authentication failure handling

When Jira returns an authentication error (401/403), the system SHALL display a persistent error in the status bar suggesting the user check their config. The system MUST NOT crash.

#### Scenario: Auth failure on sync

- **WHEN** a Jira sync attempt returns a 401 or 403 response
- **THEN** the status bar shows an authentication error message, the board continues to display locally cached data, and the application remains responsive

### Requirement: Jira transition failure handling

When a Jira transition fails, the system SHALL keep the card in its locally chosen column, display a warning indicator on the card, show the error in the status bar, and log the failure to the `sync_log` table.

#### Scenario: Transition returns an error

- **WHEN** a Jira transition API call fails (network error, invalid transition, server error)
- **THEN** the card remains in the target column locally, a warning icon appears on the card, the status bar shows the error detail, and a `push_fail` entry is written to `sync_log`

#### Scenario: Failed transition retries on manual sync

- **WHEN** the user presses `r` to force sync after a transition failure
- **THEN** the system retries the failed transition along with the normal sync cycle

### Requirement: Invalid transition ID handling

When a transition fails due to an invalid transition ID in the config, the system SHALL log the specific failure and suggest running the setup wizard again.

#### Scenario: Invalid transition ID

- **WHEN** a transition fails because the configured transition ID is not valid for the ticket's current state
- **THEN** the error message in the status bar includes guidance to re-run the setup wizard, and the failure is logged with the specific transition ID and ticket state

### Requirement: Jira rate limiting handling

When Jira returns a 429 (rate limited) response, the system SHALL apply exponential backoff to subsequent requests and display "rate limited" in the status bar.

#### Scenario: Rate limit response received

- **WHEN** the Jira API returns a 429 response
- **THEN** the status bar shows "rate limited", subsequent requests use exponential backoff, and normal request frequency resumes after the backoff period expires

### Requirement: Error events via event bus

All Jira error conditions SHALL be published as events through the EventBus. The TUI status bar MUST subscribe to these events for display.

#### Scenario: Error event published on sync failure

- **WHEN** any Jira operation fails
- **THEN** an error event is published on the EventBus containing the error type, message, and affected ticket (if applicable)

#### Scenario: Status bar subscribes to error events

- **WHEN** an error event is published on the EventBus
- **THEN** the status bar component receives the event and displays the error message

### Requirement: Card warning indicators

Cards with pending sync failures SHALL display a visual warning indicator (such as a `!` prefix) that distinguishes them from successfully synced cards.

#### Scenario: Warning indicator on failed card

- **WHEN** a card has a failed Jira transition that has not been resolved
- **THEN** the card renders with a warning indicator visible on the board view

#### Scenario: Warning indicator clears after successful sync

- **WHEN** a previously failed transition succeeds on retry
- **THEN** the warning indicator is removed from the card
