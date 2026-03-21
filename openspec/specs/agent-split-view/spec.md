## ADDED Requirements

### Requirement: Split-view layout

The agent view SHALL render as a split layout with an agent list sidebar on the left and a terminal output panel on the right.

#### Scenario: Rendering with active agents

- **WHEN** the agent view is displayed and there are active agent sessions
- **THEN** the left panel SHALL show a scrollable list of agent sessions (ticket ID, description, status, elapsed time) and the right panel SHALL show the captured terminal output of the currently selected agent

#### Scenario: Rendering with no agents

- **WHEN** the agent view is displayed and there are no agent sessions
- **THEN** the view SHALL show an empty state message with instructions on how to spawn an agent (e.g., "Press 's' to spawn an agent on a card")

#### Scenario: Sidebar width

- **WHEN** the terminal is resized
- **THEN** the sidebar SHALL maintain a fixed width of 220 characters (or 30% of terminal width, whichever is smaller) and the terminal panel SHALL fill the remaining width

### Requirement: Agent list navigation

The user SHALL be able to navigate the agent list in the sidebar using vim-style keys.

#### Scenario: Moving between agents

- **WHEN** the user presses `j` or `k` in the agent view
- **THEN** the selection SHALL move down or up in the agent list, and the terminal panel SHALL update to show the newly selected agent's output

#### Scenario: Single agent selected

- **WHEN** there is only one agent in the list
- **THEN** `j` and `k` SHALL have no effect and the single agent SHALL remain selected

### Requirement: Agent selection highlighting

The currently selected agent in the sidebar SHALL be visually distinguished.

#### Scenario: Selected agent appearance

- **WHEN** an agent is selected in the sidebar
- **THEN** it SHALL be rendered with a highlighted background (accent color), a left border indicator, and its ticket ID in bold

#### Scenario: Unselected agent appearance

- **WHEN** an agent is not selected
- **THEN** it SHALL be rendered with a muted background and standard text color

### Requirement: Terminal output panel

The right panel SHALL display a snapshot of the selected agent's tmux terminal output.

#### Scenario: Output polling

- **WHEN** an agent is selected and the agent view is active
- **THEN** the system SHALL poll `capture-pane` at a regular interval (200ms) and update the terminal panel with the latest content

#### Scenario: Output scrolling

- **WHEN** the terminal output exceeds the panel height
- **THEN** the panel SHALL show the most recent output (bottom of the buffer), matching typical terminal behavior

#### Scenario: No agent selected

- **WHEN** no agent is selected (empty list)
- **THEN** the terminal panel SHALL be blank or show a placeholder message

### Requirement: Spawn agent from agent view

The user SHALL be able to spawn a new agent from the agent view.

#### Scenario: Spawning via keybinding

- **WHEN** the user presses `s` in the agent view
- **THEN** the system SHALL show a card picker (or use the last-selected board card) to choose which card to spawn an agent on, then spawn the agent session

#### Scenario: Spawn feedback

- **WHEN** an agent is successfully spawned
- **THEN** the new agent SHALL appear in the sidebar list as the selected agent and the terminal panel SHALL begin showing its output

### Requirement: Kill agent from agent view

The user SHALL be able to kill the selected agent from the agent view.

#### Scenario: Killing via keybinding

- **WHEN** the user presses `X` (shift+x) with an agent selected
- **THEN** the system SHALL kill the tmux session and remove the agent from the active list

#### Scenario: Kill confirmation

- **WHEN** the user presses `X` to kill an agent
- **THEN** the system SHALL immediately kill without confirmation (agents are cheap to respawn)

### Requirement: Attach to agent session

The user SHALL be able to drop into the selected agent's tmux session for direct interaction.

#### Scenario: Attaching via keybinding

- **WHEN** the user presses `enter` or `tab` with an agent selected
- **THEN** Bubbletea SHALL suspend and the user SHALL be attached to the tmux session with full terminal control

#### Scenario: Returning from attachment

- **WHEN** the user presses the escape key combo (`Ctrl+]` by default) while attached
- **THEN** the tmux session SHALL detach and legato SHALL resume, returning to the agent view with the same agent still selected

### Requirement: Return to board view

The user SHALL be able to return to the board view from the agent view.

#### Scenario: Escape to board

- **WHEN** the user presses `esc` in the agent view (with no overlay active)
- **THEN** the view SHALL switch back to the board view, preserving the board's cursor position

### Requirement: Agent status indicators

Each agent in the sidebar SHALL show a status indicator.

#### Scenario: Running agent indicator

- **WHEN** an agent's tmux session is alive
- **THEN** it SHALL display a green dot (●) next to the ticket ID

#### Scenario: Dead agent indicator

- **WHEN** an agent's tmux session has died
- **THEN** it SHALL display a red dot (●) next to the ticket ID and the status label SHALL show "dead"

### Requirement: Terminal panel header

The terminal panel SHALL have a header bar showing metadata about the selected agent.

#### Scenario: Header content

- **WHEN** an agent is selected
- **THEN** the header SHALL display the ticket ID, ticket summary, the live foreground process name, elapsed time, and a live/dead status indicator

### Requirement: Keybinding help in sidebar

The agent view sidebar SHALL display available keybindings at the bottom.

#### Scenario: Help text display

- **WHEN** the agent view is displayed
- **THEN** the bottom of the sidebar SHALL show a condensed keybinding reference: j/k (select), s (spawn), X (kill), enter (attach), esc (back to board)
