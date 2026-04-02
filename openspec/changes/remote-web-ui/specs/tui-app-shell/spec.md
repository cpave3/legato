## ADDED Requirements

### Requirement: Web server toggle from TUI
The TUI SHALL support starting and stopping the embedded web server via the `S` keyboard shortcut.

#### Scenario: Start web server from TUI
- **WHEN** the user presses `S` and the web server is not running
- **THEN** the TUI SHALL start the web server in the background on the configured port and display the URL in the status bar

#### Scenario: Stop web server from TUI
- **WHEN** the user presses `S` and the web server is running
- **THEN** the TUI SHALL stop the web server and clear the status bar indicator

#### Scenario: Status bar shows server state
- **WHEN** the web server is running
- **THEN** the status bar SHALL display "Web: :3000" (or the configured port) as a persistent indicator
