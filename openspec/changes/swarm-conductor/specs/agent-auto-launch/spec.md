## ADDED Requirements

### Requirement: Adapter launch command

`AIToolAdapter` implementations MAY implement an optional `LaunchCommand(env map[string]string, prompt string) string` method. The method SHALL return a shell command line that, when run inside the agent's tmux session, launches the AI tool with the appropriate flags, system prompt, and initial brief.

#### Scenario: Claude Code launch command

- **WHEN** `LaunchCommand` is called on `ClaudeCodeAdapter` with role-prompt and worker-brief env values
- **THEN** the returned command SHALL invoke `claude` with `--append-system-prompt` set to the role prompt
- **AND** the initial brief SHALL be delivered as the first user message via the appropriate flag (`--print`, `-p`, or stdin redirect — adapter-specific)

#### Scenario: Adapter without launch command

- **WHEN** an adapter does not implement `LaunchCommand` (interface assertion fails) or returns an empty string
- **THEN** the system SHALL skip auto-launch and leave the tmux session at a shell prompt with env vars set
- **AND** this fallback SHALL preserve existing single-task spawn behavior

### Requirement: Post-spawn auto-launch via send-keys

After a tmux session is created for an agent, the system SHALL invoke the adapter's `LaunchCommand` and run the result inside the session via `tmux send-keys`. The agent's launch is asynchronous to the spawn call.

#### Scenario: Launch executed after session creation

- **WHEN** `agentService.SpawnAgent` successfully creates a tmux session
- **AND** the adapter implements `LaunchCommand` returning a non-empty string
- **THEN** the system SHALL call `tmux send-keys -t <session> "<launch-command>" Enter`
- **AND** the session SHALL be left in the running AI-tool process

#### Scenario: Failed launch does not roll back the session

- **WHEN** the `send-keys` call fails (tmux error, session vanished mid-launch)
- **THEN** the system SHALL log the failure
- **AND** the spawn call SHALL still return success (the session exists and the user can recover by attaching)

### Requirement: Launch context env vars

The system SHALL set the following env vars on the spawned tmux session before invoking the launch command:

- `LEGATO_TASK_ID` (always)
- `LEGATO_AGENT_ROLE` (when set; conductor or worker role label)
- `LEGATO_PARENT_TASK_ID` (when set; for swarm workers)
- `LEGATO_SUBTASK_ID` (when set; for swarm workers)
- `LEGATO_ROLE_PROMPT` (when adapter implements `RoleSystemPrompt` and the role yields a non-empty prompt)
- `LEGATO_INITIAL_BRIEF` (when set; the per-worker brief from the conductor's plan)
- `LEGATO_SOCKET` (always; for hook IPC)

#### Scenario: Worker spawn env vars

- **WHEN** a worker is spawned for a sub-task with role `backend`, parent `abc12345`, sub-task `st-3f9a`, and a per-worker prompt from the plan
- **THEN** the tmux session SHALL have all of `LEGATO_TASK_ID`, `LEGATO_AGENT_ROLE=backend`, `LEGATO_PARENT_TASK_ID=abc12345`, `LEGATO_SUBTASK_ID=st-3f9a`, `LEGATO_ROLE_PROMPT`, `LEGATO_INITIAL_BRIEF`, and `LEGATO_SOCKET` set

#### Scenario: Single-task spawn env vars

- **WHEN** a non-swarm single-task agent is spawned
- **THEN** the tmux session SHALL have `LEGATO_TASK_ID` and `LEGATO_SOCKET` set, and `LEGATO_AGENT_ROLE`, `LEGATO_PARENT_TASK_ID`, `LEGATO_SUBTASK_ID`, `LEGATO_ROLE_PROMPT`, `LEGATO_INITIAL_BRIEF` SHALL all be unset
