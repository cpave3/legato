## ADDED Requirements

### Requirement: Send keys to tmux session
The `TmuxManager` interface SHALL include a `SendKeys(session string, keys string, literal bool) error` method that injects input into a tmux session pane via `tmux send-keys`.

#### Scenario: Send literal text
- **WHEN** `SendKeys("legato-abc123", "y", true)` is called
- **THEN** tmux executes `send-keys -t legato-abc123 -l "y"` injecting the literal character

#### Scenario: Send key sequence
- **WHEN** `SendKeys("legato-abc123", "Enter", false)` is called
- **THEN** tmux executes `send-keys -t legato-abc123 Enter` sending the Enter key

#### Scenario: Session does not exist
- **WHEN** `SendKeys` is called for a non-existent session
- **THEN** the method returns an error

### Requirement: Agent service response injection
The `AgentService` SHALL expose a `RespondToAgent(ctx, taskID, response string) error` method that looks up the tmux session for the given task and injects the response text followed by Enter.

#### Scenario: Approve agent action
- **WHEN** `RespondToAgent(ctx, "abc123", "y")` is called
- **THEN** the service sends "y" as literal text followed by an Enter key to the agent's tmux session

#### Scenario: No active session for task
- **WHEN** `RespondToAgent` is called for a task with no active agent
- **THEN** the method returns an error
