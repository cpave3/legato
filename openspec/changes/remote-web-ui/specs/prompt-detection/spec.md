## ADDED Requirements

### Requirement: Classify terminal output into prompt types
The prompt detector SHALL analyze the last N lines of terminal output and return a classified prompt type. Classification SHALL be based on regex pattern matching against known Claude Code prompt formats.

#### Scenario: Detect tool approval prompt
- **WHEN** the terminal output contains "Do you want to" or "Allow" followed by a tool name
- **THEN** the classifier returns `tool_approval` with the tool name as context

#### Scenario: Detect compact approval prompt
- **WHEN** the terminal output contains a `[Y/n]` or `Yes / Yes, and don't ask again / No` pattern
- **THEN** the classifier returns `tool_approval`

#### Scenario: Detect plan approval prompt
- **WHEN** the terminal output contains "Accept plan?" or "Do you want to proceed with this plan"
- **THEN** the classifier returns `plan_approval`

#### Scenario: Detect free text input
- **WHEN** the terminal output ends with an input prompt indicator (e.g., `>` or `❯`) and no pending approval question
- **THEN** the classifier returns `free_text`

#### Scenario: Detect agent working
- **WHEN** the terminal output does not match any prompt pattern and content is actively changing
- **THEN** the classifier returns `working`

#### Scenario: Unknown state defaults to free text
- **WHEN** the terminal output does not match any known pattern and content is stable
- **THEN** the classifier returns `free_text` as the safe default (allows the user to type)

### Requirement: Map prompt types to UI actions
Each prompt type SHALL map to a defined set of UI actions that the frontend can render.

#### Scenario: Tool approval actions
- **WHEN** the prompt type is `tool_approval`
- **THEN** the available actions are: `{label: "Yes", keys: "y\n"}`, `{label: "No", keys: "n\n"}`, `{label: "Always", keys: "a\n"}`

#### Scenario: Plan approval actions
- **WHEN** the prompt type is `plan_approval`
- **THEN** the available actions are: `{label: "Accept", keys: "y\n"}`, `{label: "Reject", keys: "n\n"}`

#### Scenario: Free text actions
- **WHEN** the prompt type is `free_text`
- **THEN** the available action is a text input field (no predefined buttons)

#### Scenario: Working state actions
- **WHEN** the prompt type is `working`
- **THEN** no input actions are available (read-only view)

### Requirement: Prompt detection is a pure function
The prompt detector SHALL be a pure function taking terminal output text and returning a prompt classification. It SHALL have no side effects, no external dependencies, and be safe to call from any goroutine.

#### Scenario: Concurrent classification
- **WHEN** multiple goroutines call the detector simultaneously with different inputs
- **THEN** each returns the correct classification without interference

### Requirement: Prompt detection handles ANSI escape codes
The detector SHALL strip ANSI escape codes before pattern matching to handle styled terminal output.

#### Scenario: Styled approval prompt
- **WHEN** the terminal output contains a tool approval prompt with ANSI color codes around keywords
- **THEN** the classifier still correctly identifies `tool_approval`
