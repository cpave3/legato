## ADDED Requirements

### Requirement: Subcommand dispatch
The `legato` binary SHALL support subcommand dispatch. When invoked with no arguments, it SHALL launch the TUI (existing behavior). When invoked with a recognized subcommand, it SHALL execute that subcommand and exit.

#### Scenario: No arguments launches TUI
- **WHEN** `legato` is invoked with no arguments
- **THEN** it SHALL launch the Bubbletea TUI application as it does today

#### Scenario: Unknown subcommand shows error
- **WHEN** `legato` is invoked with an unrecognized subcommand
- **THEN** it SHALL print an error message to stderr and exit with code 1

### Requirement: task update subcommand
The system SHALL provide a `legato task update <task-id> --status <status>` subcommand that updates a task's column/status.

#### Scenario: Updating a task status
- **WHEN** `legato task update abc123 --status done` is executed
- **THEN** the system SHALL look up the column mapped to status `done`
- **AND** the system SHALL move the task to that column in the SQLite database
- **AND** the system SHALL send an IPC message to any running Legato instance to trigger a board refresh
- **AND** the command SHALL exit with code 0

#### Scenario: Updating a nonexistent task
- **WHEN** `legato task update nonexistent --status done` is executed
- **AND** no task with ID `nonexistent` exists
- **THEN** the command SHALL print an error to stderr and exit with code 1

#### Scenario: Using an invalid status
- **WHEN** `legato task update abc123 --status invalid` is executed
- **AND** `invalid` does not map to any configured column
- **THEN** the command SHALL print an error listing valid statuses and exit with code 1

### Requirement: task note subcommand
The system SHALL provide a `legato task note <task-id> <message>` subcommand that appends a timestamped note to a task.

#### Scenario: Adding a note to a task
- **WHEN** `legato task note abc123 "Fixed the auth bug"` is executed
- **THEN** the system SHALL append the note to the task's description or a notes field
- **AND** the system SHALL send an IPC message to trigger a board refresh
- **AND** the command SHALL exit with code 0

### Requirement: hooks install subcommand
The system SHALL provide a `legato hooks install --tool <name>` subcommand that installs AI tool hooks for the current project.

#### Scenario: Installing Claude Code hooks
- **WHEN** `legato hooks install --tool claude-code` is executed from a project directory
- **THEN** the system SHALL look up the `claude-code` adapter from the registry
- **AND** the system SHALL call `InstallHooks` with the current working directory
- **AND** the command SHALL print a success message and exit with code 0

#### Scenario: Installing hooks for unknown tool
- **WHEN** `legato hooks install --tool unknown` is executed
- **THEN** the command SHALL print an error listing available tools and exit with code 1

#### Scenario: Default tool is claude-code
- **WHEN** `legato hooks install` is executed without `--tool`
- **THEN** the system SHALL default to `--tool claude-code`

### Requirement: hooks uninstall subcommand
The system SHALL provide a `legato hooks uninstall --tool <name>` subcommand that removes previously installed hooks.

#### Scenario: Uninstalling Claude Code hooks
- **WHEN** `legato hooks uninstall --tool claude-code` is executed
- **THEN** the system SHALL call `UninstallHooks` on the Claude Code adapter
- **AND** the command SHALL print a success message and exit with code 0

### Requirement: CLI loads minimal dependencies
CLI subcommands SHALL load only the dependencies they need (config, store, IPC client). They SHALL NOT initialize the TUI, event bus, sync service, or tmux manager.

#### Scenario: CLI subcommand runs without tmux
- **WHEN** a CLI subcommand is executed on a system without tmux installed
- **THEN** the command SHALL execute successfully (tmux is only needed for the TUI's agent features)
