## MODIFIED Requirements

### Requirement: Card Rendering

Each card SHALL display the task ID, a truncated title, agent status with duration (when applicable), visual indicators for priority, and a workspace tag when in "All" view. Cards with agent data SHALL be taller than cards without.

#### Scenario: Card content display — no agent

- **WHEN** a card is rendered that has no active agent and no duration history
- **THEN** it SHALL show the provider icon and task ID on the first line, the title truncated to fit on the second line, and priority/issue type metadata on the third line

#### Scenario: Card content display — with agent

- **WHEN** a card is rendered that has an active agent or has duration history
- **THEN** it SHALL show the provider icon and task ID on the first line, the title on the second line, the agent status with duration on the third line, and priority/issue type metadata on the fourth line

#### Scenario: Workspace tag in "All" view

- **WHEN** a card is rendered while the "All" workspace view is active and the card has a workspace assigned
- **THEN** the card SHALL display a workspace tag (workspace name in workspace color) on the metadata line

#### Scenario: Workspace tag omitted in workspace view

- **WHEN** a card is rendered while a specific workspace view is active
- **THEN** the card SHALL NOT display a workspace tag (the workspace is implicit from the view)

#### Scenario: Unassigned card in "All" view

- **WHEN** a card is rendered in "All" view with no workspace assigned
- **THEN** the card SHALL NOT display a workspace tag (no tag is better than "Unassigned" clutter)

#### Scenario: Agent status line rendering

- **WHEN** the agent status line is rendered for a card with an active agent
- **THEN** it SHALL display the agent state icon, the state label (RUNNING/WAITING/IDLE), and the cumulative duration for the current state formatted as a human-readable string (e.g., "2h 15m")

#### Scenario: Agent duration display for inactive agent with history

- **WHEN** a card has no active agent but has accumulated duration history
- **THEN** the agent line SHALL display the total working and waiting durations (e.g., "1h 30m working · 20m waiting")

#### Scenario: Priority indicator

- **WHEN** a card has a priority value
- **THEN** the card SHALL display a colored left border matching the priority: red/orange for high, yellow for medium, green for low, and grey for unset

#### Scenario: Title truncation

- **WHEN** a card title exceeds the available column width minus padding
- **THEN** the title SHALL be truncated with an ellipsis to fit within the available space

#### Scenario: Warning indicator placement

- **WHEN** a card has a warning flag set
- **THEN** the warning icon SHALL be displayed on the task ID line after the provider icon, before the key
