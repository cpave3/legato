## MODIFIED Requirements

### Requirement: Root Bubbletea Model

The application SHALL have a root Bubbletea model (`internal/tui/app.go`) that owns the application lifecycle, composes sub-models, and manages view routing.

#### Scenario: Application startup

- **WHEN** the TUI application is launched
- **THEN** the root model SHALL initialize with the board view as the active view, create sub-models for the board, status bar, and agent view, and return an initial `tea.Cmd` that begins listening for EventBus messages

#### Scenario: View routing to active view

- **WHEN** a key message is received
- **THEN** the root model SHALL dispatch the key message only to the currently active view's sub-model (board, detail, or agents) and SHALL always update the status bar sub-model

### Requirement: Keyboard Dispatch

The root model SHALL route keyboard input to the active view and handle global keybindings itself.

#### Scenario: Global quit keybinding

- **WHEN** the user presses `q` while the board view is active
- **THEN** the root model SHALL return `tea.Quit` to exit the application

#### Scenario: Toggle to agent view

- **WHEN** the user presses `A` while the board view is active
- **THEN** the root model SHALL switch the active view to `viewAgents`, trigger session reconciliation, and begin terminal output polling

#### Scenario: Return from agent view

- **WHEN** the active view is `viewAgents` and the view signals a return (via esc)
- **THEN** the root model SHALL switch back to `viewBoard` and stop terminal output polling

#### Scenario: View-specific keybindings

- **WHEN** the user presses a navigation key (h, j, k, l, g, G, 1-5)
- **THEN** the root model SHALL forward the key message to the active view's `Update` method and use the returned model and command

### Requirement: View Composition

The root model's `View` method SHALL compose sub-model views into a single terminal output.

#### Scenario: Rendering the board view

- **WHEN** the board view is the active view
- **THEN** the `View` method SHALL render the board sub-model's view above the status bar sub-model's view, joined vertically using Lipgloss

#### Scenario: Rendering the agent view

- **WHEN** the agent view is the active view
- **THEN** the `View` method SHALL render the agent split-view sub-model's view above the status bar sub-model's view, joined vertically using Lipgloss

## ADDED Requirements

### Requirement: Agent view type

The view type enum SHALL include an agent view mode.

#### Scenario: View type values

- **WHEN** the application defines view types
- **THEN** the enum SHALL include `viewBoard`, `viewDetail`, and `viewAgents`

### Requirement: Tmux session exec integration

The root model SHALL support suspending Bubbletea to attach to a tmux session.

#### Scenario: Attaching to tmux session

- **WHEN** the agent view sends an `AttachSessionMsg` with a tmux session name
- **THEN** the root model SHALL use `tea.ExecProcess` to suspend the TUI and run the tmux attach command, resuming the TUI when the user detaches
