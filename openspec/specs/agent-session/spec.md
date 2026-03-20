## ADDED Requirements

### Requirement: Tmux session spawning

The system SHALL spawn a new tmux session tied to a specific card when the user requests an agent. The session SHALL use the naming convention `legato-<TICKET_ID>` and start in the project's working directory.

#### Scenario: Spawning an agent on a card

- **WHEN** the user initiates agent spawn for card "REX-1238"
- **THEN** the system SHALL create a tmux session named `legato-REX-1238`, insert a row into `agent_sessions` with status `running`, and the tmux session SHALL run a shell

#### Scenario: Spawning when tmux is not installed

- **WHEN** the user attempts to spawn an agent and tmux is not found on PATH
- **THEN** the system SHALL return an error indicating tmux is required and SHALL NOT create a database record

#### Scenario: Spawning a duplicate session

- **WHEN** the user attempts to spawn an agent for a card that already has a running agent session
- **THEN** the system SHALL return an error indicating an agent is already active for that card

### Requirement: Tmux session tracking in SQLite

The system SHALL persist agent session metadata in an `agent_sessions` table so sessions can be reassociated after legato restarts.

#### Scenario: Database schema

- **WHEN** the database is initialized
- **THEN** the `agent_sessions` table SHALL exist with columns: `id` (primary key), `ticket_id` (foreign key to tickets), `tmux_session` (unique), `command`, `status`, `started_at`, `ended_at`

#### Scenario: Reconciliation on startup

- **WHEN** legato starts and the `agent_sessions` table has rows with status `running`
- **THEN** the system SHALL check each session against `tmux list-sessions` and mark any sessions whose tmux session no longer exists as `dead`

#### Scenario: Discovering orphaned tmux sessions

- **WHEN** legato starts and there are `legato-*` prefixed tmux sessions not tracked in the database
- **THEN** the system SHALL leave them alone (no auto-tracking of externally created sessions)

### Requirement: Tmux session termination

The system SHALL allow killing an agent session, which destroys the tmux session and updates the database.

#### Scenario: Killing a running agent

- **WHEN** the user kills agent session for card "REX-1238"
- **THEN** the system SHALL run `tmux kill-session -t legato-REX-1238`, update the `agent_sessions` row to status `dead` with `ended_at` set, and remove it from the active agents list

#### Scenario: Killing an already-dead session

- **WHEN** the user kills an agent session whose tmux session no longer exists
- **THEN** the system SHALL update the database record to status `dead` without erroring

### Requirement: Terminal output capture

The system SHALL capture the current terminal content of a tmux session for display in the TUI.

#### Scenario: Capturing pane content

- **WHEN** the system requests the terminal output for session `legato-REX-1238`
- **THEN** it SHALL execute `tmux capture-pane -t legato-REX-1238 -p` and return the output as a string

#### Scenario: Capturing from a dead session

- **WHEN** the system requests terminal output for a session that no longer exists
- **THEN** it SHALL return an error indicating the session is not available

### Requirement: Session attachment for interaction

The system SHALL allow the user to attach to a tmux session for direct terminal interaction, with a configurable escape key to detach and return to legato.

#### Scenario: Attaching to a session

- **WHEN** the user requests to interact with agent session `legato-REX-1238`
- **THEN** the system SHALL suspend Bubbletea's alt-screen and attach to the tmux session, giving the user full terminal control

#### Scenario: Detaching with escape key

- **WHEN** the user presses `Ctrl+]` (or the configured escape key) while attached to a tmux session
- **THEN** the tmux client SHALL detach and legato SHALL resume its alt-screen TUI

#### Scenario: Custom escape key configuration

- **WHEN** the config file contains `agents.escape_key: ctrl+\\`
- **THEN** the system SHALL use `Ctrl+\` as the detach key instead of the default `Ctrl+]`

### Requirement: Agent session listing

The system SHALL provide a list of all tracked agent sessions with their current status.

#### Scenario: Listing active sessions

- **WHEN** the system queries for agent sessions
- **THEN** it SHALL return all sessions from the database with their ticket ID, tmux session name, status, command, and elapsed time since `started_at`

#### Scenario: Listing after reconciliation

- **WHEN** the system lists sessions after startup reconciliation
- **THEN** sessions whose tmux process died SHALL show status `dead`, and sessions still running in tmux SHALL show status `running`
