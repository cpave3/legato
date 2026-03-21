## MODIFIED Requirements

### Requirement: Split-view layout

The agent view SHALL render as a split layout with an agent list sidebar on the left and a terminal output panel on the right.

#### Scenario: Rendering with active agents

- **WHEN** the agent view is displayed and there are active agent sessions
- **THEN** the left panel SHALL show a scrollable list of agent sessions (ticket ID, task title, command, status) and the right panel SHALL show the captured terminal output of the currently selected agent

#### Scenario: Rendering with no agents

- **WHEN** the agent view is displayed and there are no agent sessions
- **THEN** the view SHALL show an empty state message with instructions on how to spawn an agent (e.g., "Press 's' to spawn an agent on a card")

#### Scenario: Sidebar width

- **WHEN** the terminal is resized
- **THEN** the sidebar SHALL maintain a fixed width of 220 characters (or 30% of terminal width, whichever is smaller) and the terminal panel SHALL fill the remaining width

### Requirement: Agent selection highlighting

The currently selected agent in the sidebar SHALL be visually distinguished.

#### Scenario: Selected agent appearance

- **WHEN** an agent is selected in the sidebar
- **THEN** it SHALL be rendered with a highlighted background (accent color), a left border indicator, its ticket ID in bold, and a truncated task title

#### Scenario: Unselected agent appearance

- **WHEN** an agent is not selected
- **THEN** it SHALL be rendered with a muted background, standard text color, and a dim task title

## ADDED Requirements

### Requirement: Task title in sidebar entries

Each agent entry in the sidebar SHALL display the task title below the status/ID line.

#### Scenario: Title displayed and truncated

- **WHEN** an agent entry is rendered and the task title exceeds the sidebar card width
- **THEN** the title SHALL be truncated with an ellipsis (`…`) to fit within the card content width

#### Scenario: Title displayed in full

- **WHEN** an agent entry is rendered and the task title fits within the sidebar card width
- **THEN** the full title SHALL be displayed without truncation

#### Scenario: Missing title

- **WHEN** an agent's task has no title (e.g., task was deleted)
- **THEN** the entry SHALL display only the status/ID and command lines (no empty title line)

### Requirement: Task title in terminal header

The terminal panel header SHALL display the task title alongside the task ID for the selected agent.

#### Scenario: Header with title

- **WHEN** an agent is selected and has a task title
- **THEN** the terminal header SHALL display the task ID followed by the task title, truncated to fit the header width

#### Scenario: Header without title

- **WHEN** an agent is selected but has no task title
- **THEN** the terminal header SHALL display only the task ID (existing behavior)
