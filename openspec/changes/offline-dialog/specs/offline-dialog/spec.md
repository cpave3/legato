## ADDED Requirements

### Requirement: Persistent offline dialog

The web client SHALL show a persistent dialog when its WebSocket connection remains disconnected for two seconds. The authenticated application content SHALL remain rendered behind the dialog.

#### Scenario: Delayed offline feedback

- **WHEN** the WebSocket remains disconnected for two seconds
- **THEN** the offline dialog SHALL appear over the current authenticated page without replacing that page

#### Scenario: Slow initial connection succeeds

- **WHEN** the WebSocket connects before the two-second delay expires
- **THEN** the offline dialog SHALL not appear

#### Scenario: Connection recovers

- **WHEN** the offline dialog is visible and the WebSocket reconnects
- **THEN** the dialog SHALL close automatically

### Requirement: Dismissible offline feedback

The offline dialog SHALL be dismissible by its close control or Escape key. Dismissal SHALL preserve a visible disconnected indicator on desktop and mobile navigation, and the dialog SHALL reappear after a later disconnect cycle.

#### Scenario: Inspect cached application state

- **WHEN** the user dismisses the offline dialog while still disconnected
- **THEN** the current page SHALL remain usable for inspection and a red disconnected indicator SHALL remain visible

#### Scenario: Later disconnection

- **WHEN** the user dismissed a previous offline dialog, reconnects, and later disconnects again
- **THEN** the offline dialog SHALL appear after the normal delay

### Requirement: Offline recovery actions

The offline dialog SHALL provide retry, server-management, and configured-server switching actions. Remote connection failures SHALL include a TLS trust hint.

#### Scenario: Switch active server

- **WHEN** the user selects Local or another configured server in the offline dialog
- **THEN** the client SHALL make that server active

#### Scenario: Retry connection

- **WHEN** the user selects Retry
- **THEN** the client SHALL reload the page

#### Scenario: Remote server failure

- **WHEN** the active server is remote and disconnected
- **THEN** the dialog SHALL suggest checking that the server is running and its TLS certificate is trusted

### Requirement: Accessible modal interaction

While visible, the offline dialog SHALL identify itself as a modal dialog, receive keyboard focus, contain tab focus, support Escape dismissal, and restore the previously focused element when it closes.

#### Scenario: Keyboard dismissal

- **WHEN** the offline dialog has opened and the user presses Escape
- **THEN** the dialog SHALL close and focus SHALL return to the previously focused control
