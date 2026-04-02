## MODIFIED Requirements

### Requirement: Spawn agent from agent view

The user SHALL be able to spawn a new agent from the agent view, either for an existing board card or as an ephemeral session with a user-provided title.

#### Scenario: Spawning an ephemeral agent via keybinding

- **WHEN** the user presses `s` in the agent view
- **THEN** the system SHALL open a title input overlay prompting for a session name

#### Scenario: Submitting ephemeral agent title

- **WHEN** the user types a title in the ephemeral spawn overlay and presses enter
- **THEN** the system SHALL create an ephemeral task with the given title and spawn an agent session for it

#### Scenario: Cancelling ephemeral agent spawn

- **WHEN** the user presses `esc` in the ephemeral spawn overlay
- **THEN** the overlay SHALL dismiss without creating a task or spawning an agent

#### Scenario: Empty title submission

- **WHEN** the user presses enter with an empty title in the ephemeral spawn overlay
- **THEN** the system SHALL use a default title of "Ephemeral session"

#### Scenario: Spawn feedback

- **WHEN** an ephemeral agent is successfully spawned
- **THEN** the new agent SHALL appear in the sidebar list as the selected agent and the terminal panel SHALL begin showing its output

### Requirement: Rendering with no agents

The agent view SHALL show appropriate empty state messaging.

#### Scenario: Rendering with no agents

- **WHEN** the agent view is displayed and there are no agent sessions
- **THEN** the view SHALL show an empty state message with instructions including "Press 's' to spawn an ephemeral session"
