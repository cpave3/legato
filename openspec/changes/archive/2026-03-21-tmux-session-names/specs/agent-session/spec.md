## ADDED Requirements

### Requirement: Pane command querying

The system SHALL provide the ability to query the current foreground process name for tmux sessions.

#### Scenario: Querying pane command for all legato sessions

- **WHEN** the system queries pane commands
- **THEN** it SHALL execute a single `tmux list-panes` call filtered to `legato-*` sessions and return a map of session name to foreground process name

#### Scenario: No legato sessions running

- **WHEN** the system queries pane commands and no legato-prefixed tmux sessions exist
- **THEN** it SHALL return an empty map without error

#### Scenario: Tmux not available

- **WHEN** the system queries pane commands and tmux is not installed
- **THEN** it SHALL return an error

## MODIFIED Requirements

### Requirement: Agent session listing

The system SHALL provide a list of all tracked agent sessions with their current status.

#### Scenario: Listing active sessions

- **WHEN** the system queries for agent sessions
- **THEN** it SHALL return all sessions from the database with their ticket ID, tmux session name, status, and elapsed time since `started_at`. For running sessions, the `Command` field SHALL reflect the current foreground process name queried from tmux. For dead sessions, the `Command` field SHALL use the stored database value.

#### Scenario: Listing after reconciliation

- **WHEN** the system lists sessions after startup reconciliation
- **THEN** sessions whose tmux process died SHALL show status `dead`, and sessions still running in tmux SHALL show status `running`
