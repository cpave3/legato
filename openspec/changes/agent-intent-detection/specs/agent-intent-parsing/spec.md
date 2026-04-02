## ADDED Requirements

### Requirement: Intent extraction from pane output
The system SHALL provide an `IntentService` in the service layer that captures the last N lines (default 50) of a tmux pane, strips ANSI escape codes, and sends the cleaned text to the configured LLM provider with a system prompt instructing it to extract a structured intent.

#### Scenario: Successful intent extraction
- **WHEN** `ParseIntent(ctx, taskID)` is called for a task with an active agent session
- **THEN** the service captures pane output, calls the LLM, and returns an `AgentIntent` struct with `Action`, `Description`, `RiskLevel`, `ApproveText`, and `DenyText` fields

#### Scenario: LLM returns valid JSON
- **WHEN** the LLM response contains a JSON object (optionally wrapped in markdown code fences)
- **THEN** the service parses the JSON into an `AgentIntent` struct

#### Scenario: LLM returns unparseable response
- **WHEN** the LLM response cannot be parsed as JSON
- **THEN** the service returns a fallback intent with Action="unknown", Description="Agent is waiting for input", RiskLevel="medium", and empty approve/deny text

#### Scenario: No active agent for task
- **WHEN** `ParseIntent` is called for a task with no active agent session
- **THEN** the service returns an error

#### Scenario: LLM not configured
- **WHEN** the `IntentService` was created without an LLM provider (nil)
- **THEN** `ParseIntent` returns an error indicating intent parsing is unavailable

### Requirement: ANSI escape code stripping
The intent service SHALL strip all ANSI escape sequences (CSI, OSC, SGR) from captured pane output before sending to the LLM.

#### Scenario: Pane output with color codes
- **WHEN** the captured pane output contains ANSI SGR color codes (e.g. `\x1b[32m`)
- **THEN** the stripped output contains only the visible text content

### Requirement: Intent triggered on waiting state
The system SHALL trigger intent parsing when an agent transitions to `waiting` state. The parsed intent SHALL be delivered to the TUI via a message/event.

#### Scenario: Agent enters waiting state
- **WHEN** an IPC `agent_state` message sets activity to `waiting` for a task
- **THEN** the system initiates intent parsing for that task (if LLM is configured)

#### Scenario: Agent leaves waiting state before parsing completes
- **WHEN** intent parsing is in progress but the agent transitions to `working` or exits
- **THEN** the pending intent result is discarded when it arrives

### Requirement: Intent caching
The system SHALL cache the most recent parsed intent per task ID. The cache entry SHALL be invalidated when the agent's state changes away from `waiting`.

#### Scenario: Navigate away and back to waiting agent
- **WHEN** the user navigates away from a waiting agent and returns
- **THEN** the previously parsed intent is displayed without re-parsing

#### Scenario: Agent state changes
- **WHEN** a cached intent exists but the agent transitions to `working`
- **THEN** the cached intent for that task is cleared
