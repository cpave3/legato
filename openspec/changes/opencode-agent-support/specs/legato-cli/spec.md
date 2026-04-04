## MODIFIED Requirements

### Requirement: hooks install subcommand
The system SHALL provide a `legato hooks install --tool <name>` subcommand that installs AI tool hooks for the current project.

#### Scenario: Installing Claude Code hooks
- **WHEN** `legato hooks install --tool claude-code` is executed from a project directory
- **THEN** the system SHALL look up the `claude-code` adapter from the registry
- **AND** the system SHALL call `InstallHooks` with the current working directory
- **AND** the command SHALL print a success message and exit with code 0

#### Scenario: Installing OpenCode plugin
- **WHEN** `legato hooks install --tool opencode` is executed
- **THEN** the system SHALL look up the `opencode` adapter from the registry
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

#### Scenario: Uninstalling OpenCode plugin
- **WHEN** `legato hooks uninstall --tool opencode` is executed
- **THEN** the system SHALL call `UninstallHooks` on the OpenCode adapter
- **AND** the command SHALL print a success message and exit with code 0
