## ADDED Requirements

### Requirement: AI tool adapter interface
The system SHALL define an `AIToolAdapter` interface in the service layer that abstracts the integration between Legato and external AI tools. The interface SHALL support hook installation/uninstallation, environment variable provisioning, and adapter identification.

#### Scenario: Adapter provides its name
- **WHEN** `Name()` is called on any adapter implementation
- **THEN** it SHALL return a stable string identifier (e.g., `"claude-code"`, `"aider"`)

#### Scenario: Adapter installs hooks for a project
- **WHEN** `InstallHooks(projectDir)` is called
- **THEN** the adapter SHALL configure the external tool's hook system within the given project directory
- **AND** the adapter SHALL return an error if the external tool is not detected or installation fails

#### Scenario: Adapter uninstalls hooks
- **WHEN** `UninstallHooks(projectDir)` is called
- **THEN** the adapter SHALL remove any hook configuration it previously installed
- **AND** the adapter SHALL not remove hook configuration belonging to other tools or user-defined hooks

#### Scenario: Adapter provides environment variables
- **WHEN** `EnvVars(taskID, socketPath)` is called
- **THEN** the adapter SHALL return a map of environment variable names to values that the external tool's hooks need to communicate back to Legato

### Requirement: Adapter registry
The system SHALL maintain a registry of available AI tool adapters. Adapters SHALL be registered at startup and looked up by name.

#### Scenario: Looking up a registered adapter
- **WHEN** an adapter with name `"claude-code"` has been registered
- **AND** `GetAdapter("claude-code")` is called
- **THEN** the registry SHALL return the Claude Code adapter

#### Scenario: Looking up an unregistered adapter
- **WHEN** `GetAdapter("unknown-tool")` is called
- **AND** no adapter with that name has been registered
- **THEN** the registry SHALL return an error indicating the adapter is not available

### Requirement: Environment variable injection into tmux sessions
The system SHALL inject adapter-provided environment variables into tmux sessions when spawning agents. The variables MUST be available to all processes started within the session.

#### Scenario: Spawning an agent with adapter configured
- **WHEN** `AgentService.SpawnAgent` is called for a task
- **AND** an AI tool adapter is configured
- **THEN** the tmux session SHALL have `LEGATO_TASK_ID` set to the task ID
- **AND** the tmux session SHALL have `LEGATO_SOCKET` set to the IPC socket path

#### Scenario: Spawning an agent without adapter configured
- **WHEN** `AgentService.SpawnAgent` is called for a task
- **AND** no AI tool adapter is configured
- **THEN** the tmux session SHALL be spawned normally without additional environment variables
