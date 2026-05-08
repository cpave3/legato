## ADDED Requirements

### Requirement: Optional role system prompt

`AIToolAdapter` implementations MAY implement an optional `RoleSystemPrompt(role string) string` method. When implemented, the adapter SHALL return a system prompt appropriate for the given role label. The agent service SHALL use interface assertion to detect support and SHALL skip prompt resolution when not implemented.

#### Scenario: Adapter implements role prompts

- **WHEN** `RoleSystemPrompt("conductor")` is called on an adapter that implements the interface
- **THEN** the adapter SHALL return the system prompt for the conductor role (built-in default or user override)

#### Scenario: Unknown role returns empty

- **WHEN** `RoleSystemPrompt` is called with a role label that has no built-in or override mapping
- **THEN** the adapter SHALL return an empty string
- **AND** the system SHALL skip setting `LEGATO_ROLE_PROMPT` for that spawn

#### Scenario: User overrides take precedence

- **WHEN** `cfg.Swarm.Prompts[<role>][<adapter-name>]` is set to a non-empty string
- **THEN** the adapter SHALL return that override in preference to any built-in prompt

### Requirement: Adapter launch command

`AIToolAdapter` implementations MAY implement an optional `LaunchCommand(env map[string]string, brief string) string` method. When implemented, the adapter SHALL return a single-line shell command that, when executed via `tmux send-keys`, starts the AI tool with the role system prompt applied and the brief delivered as the initial conversational turn.

#### Scenario: Claude Code launch command

- **WHEN** `LaunchCommand` is called on the Claude Code adapter
- **THEN** the returned command SHALL invoke `claude` with `--append-system-prompt "$LEGATO_ROLE_PROMPT"` (or equivalent)
- **AND** the brief SHALL be delivered as the first user message via the tool's flag conventions

#### Scenario: Chimera launch command

- **WHEN** `LaunchCommand` is called on the Chimera adapter
- **THEN** the returned command SHALL invoke `chimera --prompt "<combined-prompt>"` where the combined prompt fuses the role system prompt and the initial brief

#### Scenario: Adapter without launch command

- **WHEN** an adapter does not implement `LaunchCommand` (interface assertion fails) or returns an empty string
- **THEN** the agent service SHALL leave the tmux session at a shell prompt with env vars set, preserving the legacy spawn behavior

### Requirement: Swarm-context environment variables

`AIToolAdapter.EnvVars` SHALL be supplemented at spawn time with swarm-specific variables when the agent is part of a swarm. The supplementation is performed by the agent service, not by the adapter, so adapters do not need swarm awareness.

#### Scenario: Conductor spawn env vars

- **WHEN** a conductor is spawned for parent `abc12345`
- **THEN** the tmux session SHALL have `LEGATO_TASK_ID=abc12345`, `LEGATO_AGENT_ROLE=conductor`, `LEGATO_PARENT_TASK_ID=abc12345`, `LEGATO_SOCKET`, and `LEGATO_ROLE_PROMPT` set

#### Scenario: Worker spawn env vars

- **WHEN** a worker is spawned for sub-task `st-3f9a` under parent `abc12345` with role `backend` and a per-worker brief
- **THEN** the tmux session SHALL have `LEGATO_TASK_ID=st-3f9a`, `LEGATO_AGENT_ROLE=backend`, `LEGATO_PARENT_TASK_ID=abc12345`, `LEGATO_SUBTASK_ID=st-3f9a`, `LEGATO_SOCKET`, `LEGATO_ROLE_PROMPT` (if non-empty), and `LEGATO_INITIAL_BRIEF` set

#### Scenario: Non-swarm spawn unchanged

- **WHEN** a single-task agent is spawned without swarm context
- **THEN** only the existing env vars (`LEGATO_TASK_ID`, `LEGATO_SOCKET`) SHALL be set
- **AND** swarm-specific variables SHALL be absent

## MODIFIED Requirements

### Requirement: Environment variable injection into tmux sessions
The system SHALL inject adapter-provided environment variables into tmux sessions when spawning agents. The variables MUST be available to all processes started within the session. For swarm participants, the system SHALL additionally inject swarm-context variables (`LEGATO_AGENT_ROLE`, `LEGATO_PARENT_TASK_ID`, `LEGATO_SUBTASK_ID`, `LEGATO_ROLE_PROMPT`, `LEGATO_INITIAL_BRIEF`) on top of the adapter's base set.

#### Scenario: Spawning a single-task agent with adapter configured
- **WHEN** `AgentService.SpawnAgent` is called for a task without swarm options
- **AND** an AI tool adapter is configured
- **THEN** the tmux session SHALL have `LEGATO_TASK_ID` set to the task ID
- **AND** the tmux session SHALL have `LEGATO_SOCKET` set to the IPC socket path
- **AND** swarm-context variables SHALL NOT be set

#### Scenario: Spawning a swarm agent with adapter configured
- **WHEN** `AgentService.SpawnAgent` is called with swarm options (role, parent, subtask, brief)
- **AND** an AI tool adapter is configured
- **THEN** the tmux session SHALL have the base env vars plus `LEGATO_AGENT_ROLE`, `LEGATO_PARENT_TASK_ID`, `LEGATO_SUBTASK_ID` (if swarm worker), `LEGATO_ROLE_PROMPT` (if non-empty), and `LEGATO_INITIAL_BRIEF` (if non-empty)

#### Scenario: Spawning an agent without adapter configured
- **WHEN** `AgentService.SpawnAgent` is called for a task
- **AND** no AI tool adapter is configured
- **THEN** the tmux session SHALL be spawned normally without additional environment variables
