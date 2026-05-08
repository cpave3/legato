## ADDED Requirements

### Requirement: Optional role system prompt

`AIToolAdapter` implementations MAY implement an optional `RoleSystemPrompt(role string) string` method. When implemented, the adapter SHALL return a system prompt appropriate for the given swarm role.

#### Scenario: Adapter implements role prompts

- **WHEN** `RoleSystemPrompt("builder")` is called on the Claude Code adapter
- **THEN** it SHALL return a non-empty system prompt instructing the agent to operate as a builder (focused on its scope, no commits without review, etc.)

#### Scenario: Adapter does not implement role prompts

- **WHEN** an adapter does not implement `RoleSystemPrompt`
- **THEN** the agent service SHALL detect this (interface assertion) and skip prompt injection while still setting role env vars

#### Scenario: Unknown role

- **WHEN** `RoleSystemPrompt("unknown-role")` is called
- **THEN** the adapter SHALL return an empty string and the system SHALL skip prompt injection

### Requirement: Swarm environment variables

Adapter `EnvVars(taskID, socketPath)` SHALL be supplemented at spawn time with swarm-specific variables when the agent is part of a swarm.

#### Scenario: Swarm env vars on spawn

- **WHEN** an agent is spawned with role `builder` for parent task `abc12345`
- **THEN** the tmux session SHALL have `LEGATO_TASK_ID`, `LEGATO_SOCKET`, `LEGATO_AGENT_ROLE=builder`, and `LEGATO_PARENT_TASK_ID=abc12345` set

#### Scenario: Non-swarm spawn unchanged

- **WHEN** an agent is spawned without swarm context
- **THEN** only the existing env vars SHALL be set (`LEGATO_AGENT_ROLE` and `LEGATO_PARENT_TASK_ID` SHALL be absent)
