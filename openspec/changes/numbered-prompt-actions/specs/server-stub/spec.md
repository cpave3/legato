## MODIFIED Requirements

### Requirement: Prompt detection extracts numbered actions

The `prompt.Detect` function SHALL parse numbered option lines from terminal output to build action lists dynamically. When the tail of the output contains lines matching `^\s*\d+\.\s+.+$` in a contiguous block at the end, each line SHALL produce an `Action` with the line text as `Label` and the digit string as `Keys`.

When a tool approval or plan approval prompt is detected but no numbered option lines are found, the function SHALL fall back to hardcoded default actions (preserving current behavior).

The function SHALL continue to return the same `PromptState` struct with `Type`, `Context`, and `Actions` fields. The `Action` struct (`Label` + `Keys`) SHALL remain unchanged.

#### Scenario: Tool approval with numbered options

- **WHEN** terminal output contains a tool approval pattern AND the last lines include:
  ```
  1. Yes
  2. Yes, and don't ask again
  3. No
  ```
- **THEN** `Detect` SHALL return `ToolApproval` type with actions `[{Label: "Yes", Keys: "1"}, {Label: "Yes, and don't ask again", Keys: "2"}, {Label: "No", Keys: "3"}]`

#### Scenario: Tool approval with different numbered options

- **WHEN** terminal output contains a tool approval pattern AND the last lines include:
  ```
  1. Yes
  2. Always allow
  3. Don't allow
  4. Trust this tool
  ```
- **THEN** `Detect` SHALL return `ToolApproval` type with four actions, each using the digit as `Keys`

#### Scenario: Tool approval without numbered options (fallback)

- **WHEN** terminal output matches a tool approval pattern but contains no numbered option lines in the tail
- **THEN** `Detect` SHALL return `ToolApproval` type with the hardcoded default actions: `[{Label: "Yes", Keys: "Enter"}, {Label: "Always", Keys: "Down Enter"}, {Label: "No", Keys: "Down Down Enter"}]`

#### Scenario: Plan approval retains hardcoded actions

- **WHEN** terminal output matches a plan approval pattern and contains no numbered option lines
- **THEN** `Detect` SHALL return `PlanApproval` type with `[{Label: "Accept", Keys: "Enter"}, {Label: "Reject", Keys: "Escape"}]`

#### Scenario: Plan approval with numbered options

- **WHEN** terminal output matches a plan approval pattern AND contains numbered option lines
- **THEN** `Detect` SHALL return `PlanApproval` type with dynamically extracted actions using digit keys

#### Scenario: Numbered lines must be contiguous from bottom

- **WHEN** terminal output ends with:
  ```
  Some other text
  1. Yes
  2. No
  ```
- **THEN** only the contiguous numbered block (`1. Yes`, `2. No`) SHALL be extracted as actions

#### Scenario: Non-contiguous numbered lines ignored

- **WHEN** terminal output ends with:
  ```
  1. First item in a list
  Some intervening text
  1. Yes
  2. No
  ```
- **THEN** only the final contiguous block (`1. Yes`, `2. No`) SHALL be extracted

#### Scenario: Free text prompt unchanged

- **WHEN** terminal output ends with a cursor prompt (`❯` or `>`)
- **THEN** `Detect` SHALL return `FreeText` type with no actions, regardless of any numbered lines above
