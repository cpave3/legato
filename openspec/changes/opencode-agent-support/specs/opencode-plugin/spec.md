## ADDED Requirements

### Requirement: OpenCode adapter implements AIToolAdapter
The system SHALL provide an OpenCode adapter in `internal/engine/hooks/` that implements the `AIToolAdapter` interface. The adapter SHALL generate a TypeScript plugin file for OpenCode's plugin system.

#### Scenario: Adapter identifies itself
- **WHEN** `Name()` is called on the OpenCode adapter
- **THEN** it SHALL return `"opencode"`

### Requirement: Plugin installation writes TypeScript file to global plugins directory
The adapter SHALL generate and write a `legato.ts` plugin file to OpenCode's global plugins directory when installing hooks.

#### Scenario: Installing plugin
- **WHEN** `InstallHooks(projectDir)` is called
- **THEN** the adapter SHALL create `$XDG_CONFIG_HOME/opencode/plugins/legato.ts` (or `~/.config/opencode/plugins/legato.ts` if `$XDG_CONFIG_HOME` is not set)
- **AND** the file SHALL have standard file permissions (0644)
- **AND** the adapter SHALL create the `plugins/` directory if it does not exist

#### Scenario: Reinstalling plugin overwrites previous installation
- **WHEN** `InstallHooks(projectDir)` is called
- **AND** a `legato.ts` plugin was previously installed
- **THEN** the adapter SHALL overwrite the existing plugin file with the current version

#### Scenario: projectDir parameter is not used
- **WHEN** `InstallHooks(projectDir)` is called with any project directory
- **THEN** the adapter SHALL install the plugin to the global OpenCode plugins directory regardless of the projectDir value

### Requirement: Generated plugin tracks agent activity via session.status events
The generated TypeScript plugin SHALL listen for OpenCode lifecycle events and call the Legato CLI to update agent activity state.

#### Scenario: Plugin checks for Legato context on load
- **WHEN** the plugin is loaded by OpenCode
- **AND** `LEGATO_TASK_ID` is not set in the environment
- **THEN** the plugin SHALL return an empty hooks object (no-op)

#### Scenario: OpenCode session becomes busy
- **WHEN** the plugin receives a `session.status` event with `status.type === "busy"`
- **AND** `LEGATO_TASK_ID` is set
- **THEN** the plugin SHALL execute `<legato-bin> agent state <task-id> --activity working`

#### Scenario: OpenCode session becomes idle
- **WHEN** the plugin receives a `session.status` event with `status.type === "idle"`
- **AND** `LEGATO_TASK_ID` is set
- **THEN** the plugin SHALL execute `<legato-bin> agent state <task-id> --activity waiting`

#### Scenario: OpenCode session is deleted
- **WHEN** the plugin receives a `session.deleted` event
- **AND** `LEGATO_TASK_ID` is set
- **THEN** the plugin SHALL execute `<legato-bin> agent state <task-id> --activity ""`

#### Scenario: CLI invocation fails silently
- **WHEN** the plugin executes a `legato agent state` command
- **AND** the command fails (non-zero exit, binary not found, etc.)
- **THEN** the plugin SHALL catch the error and continue without crashing

### Requirement: Generated plugin embeds absolute legato binary path
The generated plugin file SHALL contain the absolute path to the `legato` binary, resolved at install time.

#### Scenario: Legato binary path is embedded
- **WHEN** the adapter generates the plugin file
- **THEN** the plugin source SHALL contain the absolute path to the legato binary (not rely on `$PATH` resolution)

### Requirement: Plugin uninstallation removes only Legato plugin
The adapter SHALL remove only the Legato-installed plugin file during uninstallation.

#### Scenario: Uninstalling plugin
- **WHEN** `UninstallHooks(projectDir)` is called
- **THEN** the adapter SHALL remove `$XDG_CONFIG_HOME/opencode/plugins/legato.ts`
- **AND** other files in the plugins directory SHALL not be affected

#### Scenario: Uninstalling when plugin does not exist
- **WHEN** `UninstallHooks(projectDir)` is called
- **AND** `legato.ts` does not exist in the plugins directory
- **THEN** the adapter SHALL return nil (no error)

### Requirement: Adapter provides environment variables for tmux sessions
The adapter SHALL return the standard Legato environment variables for injection into tmux sessions.

#### Scenario: EnvVars returns LEGATO_TASK_ID
- **WHEN** `EnvVars(taskID, socketPath)` is called
- **THEN** the adapter SHALL return a map containing `LEGATO_TASK_ID` set to the provided taskID
