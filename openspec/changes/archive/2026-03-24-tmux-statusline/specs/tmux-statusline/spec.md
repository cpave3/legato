## ADDED Requirements

### Requirement: Agent summary CLI subcommand
The system SHALL provide a `legato agent summary` CLI subcommand that outputs a single-line tmux-formatted summary of active agent session counts grouped by activity state (working, waiting, idle).

#### Scenario: Multiple agents in mixed states
- **WHEN** there are 2 agents with activity "working", 1 with activity "waiting", and 1 with activity "" (idle)
- **THEN** the output SHALL contain tmux style markup showing "2 working", "1 waiting", "1 idle" with appropriate colors (green for working, yellow for waiting, dim for idle)

#### Scenario: No active agents
- **WHEN** there are no running agent sessions
- **THEN** the output SHALL be a single line showing "0 working · 0 waiting · 0 idle" (or equivalent minimal representation)

#### Scenario: Exclude current task
- **WHEN** the `--exclude <task-id>` flag is provided
- **THEN** the agent session for that task SHALL be excluded from the counts

#### Scenario: Zero-count states omitted
- **WHEN** a state has zero sessions (e.g., 0 waiting)
- **THEN** that state MAY be omitted from output to keep the line compact, showing only non-zero counts plus idle

### Requirement: Automatic tmux status line injection
The system SHALL configure a custom tmux status bar on every legato-spawned tmux session that displays the agent summary output.

#### Scenario: Session spawned with status line
- **WHEN** an agent session is spawned via `AgentService.SpawnAgent`
- **THEN** the tmux session SHALL have `status-right` set to invoke `legato agent summary --exclude <task-id>` using the absolute path to the legato binary
- **AND** `status-interval` SHALL be set to 5 seconds
- **AND** these options SHALL be applied before user-configured `tmux_options`

#### Scenario: User tmux_options override
- **WHEN** the user has `status-right` configured in `agents.tmux_options`
- **THEN** the user's value SHALL override legato's default status-right (user config wins)

#### Scenario: Binary path resolution
- **WHEN** the status-right command is configured
- **THEN** the legato binary SHALL be referenced by absolute path (resolved via `os.Executable()`) to ensure `#()` works regardless of the shell's PATH

### Requirement: Task ID baked into status-right at spawn time
The system SHALL embed the task ID directly into the `status-right` string at spawn time rather than relying on environment variable expansion in the `#()` context.

#### Scenario: Task ID embedded in command
- **WHEN** a session is spawned for task "abc12345"
- **THEN** the `status-right` option SHALL be set to something like `#(/path/to/legato agent summary --exclude abc12345)` with the task ID as a literal string

### Requirement: Summary output uses tmux style markup
The `legato agent summary` command SHALL output tmux-native style markup (using `#[fg=colour]` syntax) so colors render correctly in the tmux status bar.

#### Scenario: Colored output
- **WHEN** there are working and waiting agents
- **THEN** the output SHALL use green for working count, yellow for waiting count, and a dim color for idle count

#### Scenario: Output is a single line
- **WHEN** the command executes successfully
- **THEN** the output SHALL be exactly one line with no trailing newline beyond what tmux expects

### Requirement: CLI loads only minimal dependencies
The `legato agent summary` subcommand SHALL follow the existing CLI pattern of loading only config + store — no TUI, event bus, tmux manager, or sync service.

#### Scenario: Lightweight execution
- **WHEN** `legato agent summary` is invoked
- **THEN** it SHALL open the SQLite database, run a single aggregate query, format the output, and exit
- **AND** execution time SHALL be under 50ms for typical usage
