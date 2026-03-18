## ADDED Requirements

### Requirement: Spawn agent from board

The user SHALL be able to spawn an agent on the currently selected card directly from the board view.

#### Scenario: Spawning via keybinding

- **WHEN** the user presses `a` while a card is selected on the board
- **THEN** the board SHALL emit a message requesting an agent spawn for the selected card's ticket ID, and the root app SHALL handle spawning the agent session

#### Scenario: Spawn on card with existing agent

- **WHEN** the user presses `a` on a card that already has a running agent
- **THEN** the system SHALL switch to the agent view with that agent selected instead of spawning a duplicate

#### Scenario: Agent indicator on board cards

- **WHEN** a card has an active agent session
- **THEN** the card SHALL display a small indicator (e.g., `▶` prefix) to show an agent is running on it
