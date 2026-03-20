## ADDED Requirements

### Requirement: Claude Code adapter implements AIToolAdapter
The system SHALL provide a Claude Code adapter that implements the `AIToolAdapter` interface. The adapter SHALL generate hook scripts and configure Claude Code's `.claude/settings.json`.

#### Scenario: Adapter identifies itself
- **WHEN** `Name()` is called on the Claude Code adapter
- **THEN** it SHALL return `"claude-code"`

### Requirement: Hook installation writes scripts and settings
The system SHALL generate executable hook scripts in `.claude/hooks/` and register them in `.claude/settings.json` when installing hooks.

#### Scenario: Installing hooks in a project
- **WHEN** `InstallHooks(projectDir)` is called
- **THEN** the adapter SHALL create `.claude/hooks/legato-stop.sh` with executable permissions
- **AND** the adapter SHALL create `.claude/hooks/legato-task-completed.sh` with executable permissions
- **AND** the adapter SHALL merge hook entries into `.claude/settings.json` under the `hooks` key
- **AND** existing user-defined hooks in settings.json SHALL be preserved

#### Scenario: Installing hooks when .claude directory does not exist
- **WHEN** `InstallHooks(projectDir)` is called
- **AND** the `.claude/` directory does not exist in the project
- **THEN** the adapter SHALL return an error indicating Claude Code is not configured for this project

#### Scenario: Reinstalling hooks overwrites previous installation
- **WHEN** `InstallHooks(projectDir)` is called
- **AND** Legato hooks were previously installed
- **THEN** the adapter SHALL overwrite the existing hook scripts with the current version
- **AND** the adapter SHALL update the settings.json entries

### Requirement: Hook scripts check for Legato context
Each generated hook script SHALL check for the `LEGATO_TASK_ID` environment variable and exit silently (exit 0) if it is not set. This ensures hooks are no-ops outside of Legato-spawned sessions.

#### Scenario: Hook fires inside a Legato tmux session
- **WHEN** a Claude Code hook script executes
- **AND** `LEGATO_TASK_ID` is set
- **THEN** the script SHALL parse stdin JSON and call the `legato` CLI with the appropriate subcommand

#### Scenario: Hook fires outside a Legato tmux session
- **WHEN** a Claude Code hook script executes
- **AND** `LEGATO_TASK_ID` is not set
- **THEN** the script SHALL exit with code 0 and produce no output

### Requirement: Stop hook maps to task activity
The `Stop` hook script SHALL call Legato CLI to signal that the agent has completed a response turn.

#### Scenario: Claude Code Stop event fires
- **WHEN** the `legato-stop.sh` hook executes
- **AND** `LEGATO_TASK_ID` is set
- **THEN** the script SHALL call `legato task update $LEGATO_TASK_ID --status doing`
- **AND** the script SHALL exit with code 0

### Requirement: TaskCompleted hook moves task to done
The `TaskCompleted` hook script SHALL call Legato CLI to move the task to the done column.

#### Scenario: Claude Code TaskCompleted event fires
- **WHEN** the `legato-task-completed.sh` hook executes
- **AND** `LEGATO_TASK_ID` is set
- **THEN** the script SHALL call `legato task update $LEGATO_TASK_ID --status done`
- **AND** the script SHALL exit with code 0

### Requirement: Hook uninstallation removes only Legato hooks
The adapter SHALL remove only Legato-installed hooks during uninstallation, preserving all other hook configuration.

#### Scenario: Uninstalling hooks
- **WHEN** `UninstallHooks(projectDir)` is called
- **THEN** the adapter SHALL remove `.claude/hooks/legato-stop.sh` and `.claude/hooks/legato-task-completed.sh`
- **AND** the adapter SHALL remove Legato hook entries from `.claude/settings.json`
- **AND** other hook entries in settings.json SHALL be preserved

### Requirement: Hook scripts reference legato binary by resolved path
The generated hook scripts SHALL use the absolute path to the `legato` binary (resolved at install time via `os.Executable()`), not rely on `$PATH`.

#### Scenario: Legato binary is not on PATH in tmux session
- **WHEN** a hook script executes in a tmux session where `legato` is not on `$PATH`
- **THEN** the script SHALL still find and execute the legato binary via its embedded absolute path
