## ADDED Requirements

### Requirement: Learning workflow task-transition trigger
Configured explicit local task transitions SHALL idempotently start the built-in learning workflow after the move is persisted. Trigger creation or execution failure SHALL NOT reverse or block the move, and learning workflow tasks SHALL be excluded from recursive triggering.

#### Scenario: Task enters configured Review column
- **WHEN** a user explicitly moves a task into a configured learning column with learning enabled
- **THEN** the move SHALL succeed and exactly one learning trigger/run SHALL be persisted for that transition

#### Scenario: Learning workflow task moves
- **WHEN** a system task owned by the learning workflow enters the same column
- **THEN** no nested learning workflow SHALL be started
