## ADDED Requirements

### Requirement: Borrowed agent sessions
The agent-session service SHALL support borrowing an existing task session for a workflow interaction without transferring lifecycle ownership to the workflow attempt.

#### Scenario: Borrowed workflow attempt ends
- **WHEN** an interview attempt using a borrowed session completes, fails, retries, or is cancelled
- **THEN** the task session SHALL remain tracked and running and no workflow cleanup SHALL kill, finish, rename, or delete it
