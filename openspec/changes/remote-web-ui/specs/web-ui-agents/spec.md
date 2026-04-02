## ADDED Requirements

### Requirement: Agent list sidebar
The agents page SHALL display a sidebar listing all active agent sessions with their task ID, task title, activity state (working/waiting/idle), and current command.

#### Scenario: Display agent list
- **WHEN** the agents page loads and there are 3 active agents
- **THEN** the sidebar shows 3 entries, each with task ID, title, activity badge, and command

#### Scenario: Agent list updates in real-time
- **WHEN** an `agents_changed` WebSocket message is received
- **THEN** the agent list refreshes without full page reload

#### Scenario: Empty state
- **WHEN** there are no active agents
- **THEN** the sidebar shows an empty state message indicating no agents are running

### Requirement: Agent terminal output panel
The agents page SHALL display a terminal output panel showing the captured pane content for the selected agent, rendered with ANSI color support via xterm.js.

#### Scenario: Select agent shows output
- **WHEN** the user clicks an agent in the sidebar
- **THEN** the terminal panel subscribes to that agent's output via WebSocket and displays the full pane content

#### Scenario: Real-time output streaming
- **WHEN** the selected agent's terminal output changes
- **THEN** the terminal panel updates with new content within 200ms

#### Scenario: Switch between agents
- **WHEN** the user selects a different agent
- **THEN** the terminal panel unsubscribes from the previous agent and subscribes to the new one, showing the new agent's full output

### Requirement: Prompt detection UI controls
The agents page SHALL display contextual input controls based on the detected prompt state of the selected agent.

#### Scenario: Tool approval prompt detected
- **WHEN** the prompt detector identifies a tool approval prompt (e.g., "Allow tool: Edit")
- **THEN** the UI displays "Yes", "No", and "Yes, always" buttons

#### Scenario: Plan approval prompt detected
- **WHEN** the prompt detector identifies a plan approval prompt
- **THEN** the UI displays "Accept" and "Reject" buttons

#### Scenario: Free text input detected
- **WHEN** the prompt detector identifies a free text input prompt
- **THEN** the UI displays a text input field with a send button

#### Scenario: Agent working (no prompt)
- **WHEN** the prompt detector identifies the agent is actively working
- **THEN** the input area shows a "Working..." indicator with no input controls

#### Scenario: Approval button sends keys
- **WHEN** the user clicks the "Yes" button on a tool approval prompt
- **THEN** a `send_keys` WebSocket message is sent with `keys: "y\n"`

#### Scenario: Free text input sends keys
- **WHEN** the user types text and presses send (or Enter)
- **THEN** a `send_keys` WebSocket message is sent with the typed text followed by newline

### Requirement: Mobile-friendly layout
The agents page SHALL be responsive, with the sidebar collapsing to a top-level selector on narrow viewports and the terminal panel filling the available width.

#### Scenario: Wide viewport
- **WHEN** the viewport is wider than 768px
- **THEN** the sidebar and terminal panel display side by side

#### Scenario: Narrow viewport
- **WHEN** the viewport is 768px or narrower
- **THEN** the sidebar collapses to a dropdown/selector and the terminal panel fills the width
