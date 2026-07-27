## MODIFIED Requirements

### Requirement: Connection error feedback

The system SHALL show a visible error state when the active server is unreachable, with a hint about TLS trust if the connection fails. In the web client, this error state SHALL use the persistent offline dialog and retain a disconnected navigation indicator after the dialog is dismissed.

#### Scenario: Remote server unreachable

- **WHEN** the WebSocket to the active server fails to connect
- **THEN** the offline dialog SHALL appear after its delay. If the server is not the origin, the error message SHALL include a suggestion to verify the server is running and TLS is trusted.

#### Scenario: Offline dialog dismissed

- **WHEN** the user dismisses the offline dialog while the active server remains unreachable
- **THEN** the current page SHALL remain visible and a disconnected indicator SHALL remain in the available navigation
