## ADDED Requirements

### Requirement: Intent summary panel in agent view
When the selected agent is in `waiting` state and a parsed intent is available, the agent view SHALL render a compact intent summary panel in the terminal output area. The panel SHALL display the action type, a human-readable description, and a risk level badge.

#### Scenario: Intent available for selected waiting agent
- **WHEN** the user is viewing the agent split-view and the selected agent is waiting with a parsed intent
- **THEN** a summary panel appears showing action, description, risk level, and `y` to approve / `n` to deny hints

#### Scenario: No intent available
- **WHEN** the selected agent is waiting but no intent has been parsed (LLM not configured or parsing failed with empty approve/deny)
- **THEN** the terminal output is shown as normal with no intent panel

#### Scenario: Intent loading
- **WHEN** intent parsing is in progress for the selected waiting agent
- **THEN** a small "Analyzing..." indicator is shown in the terminal area

### Requirement: Approve and deny keybindings
In the agent view, when an intent panel is visible, pressing `y` SHALL approve the action and pressing `n` SHALL deny it.

#### Scenario: User approves
- **WHEN** the intent panel is visible and the user presses `y`
- **THEN** the system injects the intent's `ApproveText` + Enter into the agent's tmux session and dismisses the intent panel

#### Scenario: User denies
- **WHEN** the intent panel is visible and the user presses `n`
- **THEN** the system injects the intent's `DenyText` + Enter into the agent's tmux session and dismisses the intent panel

#### Scenario: No approve/deny text available
- **WHEN** the intent has empty `ApproveText` and `DenyText` (fallback intent from unparseable LLM response)
- **THEN** the `y` and `n` keybindings are not active and the panel shows "Attach to terminal to respond" instead

### Requirement: Risk level visual styling
The intent panel SHALL visually distinguish risk levels using color-coded badges: green for low, yellow for medium, red for high.

#### Scenario: High risk action
- **WHEN** an intent with `RiskLevel: "high"` is displayed
- **THEN** the risk badge is rendered in red

#### Scenario: Low risk action
- **WHEN** an intent with `RiskLevel: "low"` is displayed
- **THEN** the risk badge is rendered in green
