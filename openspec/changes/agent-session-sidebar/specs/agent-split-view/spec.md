## MODIFIED Requirements

### Requirement: Split-view layout

The agent view SHALL render as a split layout with a 30-character fixed-width sidebar on the left and a terminal output panel filling the remaining width on the right.

#### Scenario: Rendering with active agents

- **WHEN** the agent view is displayed and there are active agent sessions
- **THEN** the left sidebar SHALL show a scrollable list of agent sessions with activity indicators, and the right panel SHALL show the captured terminal output of the currently selected agent

#### Scenario: Rendering with no agents

- **WHEN** the agent view is displayed and there are no agent sessions
- **THEN** the sidebar SHALL show an empty state with spawn instructions and the terminal panel SHALL be blank

#### Scenario: Sidebar width

- **WHEN** the terminal is resized
- **THEN** the sidebar SHALL remain fixed at 30 characters wide and the terminal panel SHALL fill the remaining width

### Requirement: Terminal panel header

The terminal panel SHALL have a header bar showing metadata about the selected agent.

#### Scenario: Header content

- **WHEN** an agent is selected
- **THEN** the header SHALL display the task ID, command type, elapsed time, status indicator, and keybinding hints for terminal actions (enter to attach)

## REMOVED Requirements

### Requirement: Agent status indicators
**Reason**: Replaced by sidebar activity indicators in `agent-sidebar` spec. The sidebar now shows richer activity state (working/waiting/idle/dead) instead of just running/dead status dots.
**Migration**: Activity indicators in `agent-sidebar` spec cover all status display needs.

### Requirement: Keybinding help in sidebar
**Reason**: Moved to `agent-sidebar` spec as "Sidebar keybinding hints" requirement.
**Migration**: See `agent-sidebar` spec.

### Requirement: Agent selection highlighting
**Reason**: Moved to `agent-sidebar` spec as "Sidebar selection state" requirement.
**Migration**: See `agent-sidebar` spec.
