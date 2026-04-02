## MODIFIED Requirements

### Requirement: Agent split-view terminal panel
The agent split-view terminal output panel SHALL support rendering an intent summary overlay when the selected agent has a parsed intent available. The intent panel SHALL be rendered at the bottom of the terminal area, with the terminal output occupying the remaining space above it. When no intent is available, the full terminal area is used for output (existing behavior).

#### Scenario: Waiting agent with intent
- **WHEN** the selected agent is in `waiting` state and a parsed intent exists
- **THEN** the terminal area splits: captured output above, intent summary panel below (approximately 6 lines)

#### Scenario: Waiting agent without intent
- **WHEN** the selected agent is in `waiting` state but no intent exists
- **THEN** the full terminal area shows captured output (existing behavior)

#### Scenario: Non-waiting agent selected
- **WHEN** the selected agent is in `working` or dead state
- **THEN** the full terminal area shows captured output (existing behavior)

#### Scenario: Agent transitions while viewing intent
- **WHEN** the intent panel is visible and the agent transitions to `working` state
- **THEN** the intent panel is dismissed and the full terminal area resumes showing captured output
