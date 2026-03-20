## ADDED Requirements

### Requirement: Sidebar component layout

The agent view SHALL render a persistent sidebar on the left side of the screen, fixed at 30 characters wide, with a right border separating it from the terminal panel.

#### Scenario: Sidebar renders with agents

- **WHEN** the agent view is displayed and there are active or dead agent sessions
- **THEN** the sidebar SHALL list each session as a single-line entry showing: activity icon, task ID (truncated to fit with ellipsis if needed), and activity label

#### Scenario: Sidebar renders with no agents

- **WHEN** the agent view is displayed and there are no agent sessions
- **THEN** the sidebar SHALL show an empty state message with spawn instructions (e.g., "s to spawn")

#### Scenario: Sidebar width is constant

- **WHEN** the terminal is resized
- **THEN** the sidebar SHALL remain fixed at 30 characters wide, and the terminal panel SHALL absorb the width change

### Requirement: Sidebar activity indicators

Each sidebar entry SHALL display an activity indicator matching the board's visual language.

#### Scenario: Working agent indicator

- **WHEN** an agent has activity state `"working"`
- **THEN** the sidebar entry SHALL display a green icon and "WORKING" label

#### Scenario: Waiting agent indicator

- **WHEN** an agent has activity state `"waiting"`
- **THEN** the sidebar entry SHALL display a blue icon and "waiting" label

#### Scenario: Idle agent indicator

- **WHEN** an agent has activity state `""` (empty) and status `"running"`
- **THEN** the sidebar entry SHALL display a dim icon and "idle" label

#### Scenario: Dead agent indicator

- **WHEN** an agent has status `"dead"`
- **THEN** the sidebar entry SHALL display a red icon and "dead" label

### Requirement: Sidebar selection state

The currently selected agent in the sidebar SHALL be visually distinguished from unselected entries.

#### Scenario: Selected entry appearance

- **WHEN** an agent entry is selected
- **THEN** it SHALL render with a highlighted background and bold task ID

#### Scenario: Unselected entry appearance

- **WHEN** an agent entry is not selected
- **THEN** it SHALL render with standard muted styling

### Requirement: Sidebar keybinding hints

The sidebar SHALL display condensed keybinding hints at the bottom.

#### Scenario: Hints content

- **WHEN** the agent view is displayed
- **THEN** the bottom of the sidebar SHALL show: j/k (select), s (spawn), X (kill), enter (attach), esc (board)

#### Scenario: Hints do not overlap entries

- **WHEN** the agent list is long enough to fill the sidebar height
- **THEN** the keybinding hints SHALL remain visible and the agent list SHALL scroll, not overlap the hints
