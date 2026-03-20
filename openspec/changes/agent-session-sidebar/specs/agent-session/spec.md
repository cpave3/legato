## MODIFIED Requirements

### Requirement: Tmux session spawning

The system SHALL spawn a new tmux session tied to a specific card when the user requests an agent. The session SHALL use the naming convention `legato-<TASK_ID>`, start in the project's working directory, and be created with explicit width and height dimensions matching the terminal panel size.

#### Scenario: Spawning an agent on a card

- **WHEN** the user initiates agent spawn for card "REX-1238" and the terminal panel is 90 columns wide and 40 rows tall
- **THEN** the system SHALL create a tmux session named `legato-REX-1238` with `-x 90 -y 40`, insert a row into `agent_sessions` with status `running`, and the tmux session SHALL run a shell

#### Scenario: Spawning when tmux is not installed

- **WHEN** the user attempts to spawn an agent and tmux is not found on PATH
- **THEN** the system SHALL return an error indicating tmux is required and SHALL NOT create a database record

#### Scenario: Spawning a duplicate session

- **WHEN** the user attempts to spawn an agent for a card that already has a running agent session
- **THEN** the system SHALL return an error indicating an agent is already active for that card
