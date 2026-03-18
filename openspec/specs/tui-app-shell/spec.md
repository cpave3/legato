## ADDED Requirements

### Requirement: Root Bubbletea Model

The application SHALL have a root Bubbletea model (`internal/tui/app.go`) that owns the application lifecycle, composes sub-models, and manages view routing.

#### Scenario: Application startup

- **WHEN** the TUI application is launched
- **THEN** the root model SHALL initialize with the board view as the active view, create sub-models for the board and status bar, and return an initial `tea.Cmd` that begins listening for EventBus messages

#### Scenario: View routing to active view

- **WHEN** a key message is received
- **THEN** the root model SHALL dispatch the key message only to the currently active view's sub-model and SHALL always update the status bar sub-model

### Requirement: Window Resize Handling

The root model SHALL handle `tea.WindowSizeMsg` and propagate the new dimensions to all sub-models.

#### Scenario: Terminal window is resized

- **WHEN** a `tea.WindowSizeMsg` is received
- **THEN** the root model SHALL store the new width and height, calculate the available content area (total height minus status bar height), and propagate the dimensions to the board and status bar sub-models

#### Scenario: Initial window size

- **WHEN** the application starts
- **THEN** Bubbletea SHALL send an initial `tea.WindowSizeMsg` before user input, and the root model SHALL use it to set up layout dimensions

### Requirement: Keyboard Dispatch

The root model SHALL route keyboard input to the active view and handle global keybindings itself.

#### Scenario: Global quit keybinding

- **WHEN** the user presses `q` while the board view is active
- **THEN** the root model SHALL return `tea.Quit` to exit the application

#### Scenario: View-specific keybindings

- **WHEN** the user presses a navigation key (h, j, k, l, g, G, 1-5)
- **THEN** the root model SHALL forward the key message to the active view's `Update` method and use the returned model and command

### Requirement: EventBus Bridge

The root model SHALL bridge EventBus channel events into Bubbletea messages via `tea.Cmd`.

#### Scenario: Sync event received from EventBus

- **WHEN** a sync event is published on the EventBus
- **THEN** the bridge command SHALL wrap it in a custom `tea.Msg` and deliver it to the root model's `Update`, which SHALL forward it to the status bar sub-model

#### Scenario: EventBus subscription lifecycle

- **WHEN** the application exits
- **THEN** the EventBus subscription SHALL be cleaned up to avoid goroutine leaks

### Requirement: View Composition

The root model's `View` method SHALL compose sub-model views into a single terminal output.

#### Scenario: Rendering the board view

- **WHEN** the board view is the active view
- **THEN** the `View` method SHALL render the board sub-model's view above the status bar sub-model's view, joined vertically using Lipgloss
