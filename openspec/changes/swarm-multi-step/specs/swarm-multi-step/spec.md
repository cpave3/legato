## ADDED Requirements

### Requirement: Plan declares ordered steps

A swarm plan SHALL declare one or more steps. Each step SHALL have a name and a list of sub-tasks. The plan SHALL be valid only if at least one step exists and each step contains at least one sub-task.

#### Scenario: Valid single-step plan
- **WHEN** a plan contains one step named "Setup" with one sub-task
- **THEN** `ValidatePlan` SHALL accept the plan

#### Scenario: Valid multi-step plan
- **WHEN** a plan contains three steps "Setup", "Build", "Review" each with at least one sub-task
- **THEN** `ValidatePlan` SHALL accept the plan

#### Scenario: Plan with no steps
- **WHEN** a plan contains zero steps
- **THEN** `ValidatePlan` SHALL reject the plan with error "plan must contain at least one step"

#### Scenario: Step with no sub-tasks
- **WHEN** a plan contains a step with zero sub-tasks
- **THEN** `ValidatePlan` SHALL reject the plan with error indicating the step index and "step must contain at least one sub-task"

### Requirement: Sub-tasks are tagged with their step index

When a plan is applied, each sub-task row SHALL be persisted with a `step_index` matching its position in the plan's `steps` array (0-based).

#### Scenario: ApplyApprovedPlan writes step indices
- **WHEN** `ApplyApprovedPlan` is called with a two-step plan
- **THEN** sub-tasks from the first step SHALL have `step_index = 0`
- **AND** sub-tasks from the second step SHALL have `step_index = 1`

#### Scenario: Existing rows default to step 0
- **WHEN** the database migration runs
- **THEN** all pre-existing sub-task rows SHALL have `step_index = 0`

### Requirement: Dispatch is gated by current active step

A sub-task SHALL only be dispatchable if all sub-tasks in every earlier step are in a terminal state (`done` or `cancelled`). If a sub-task belongs to a blocked step, dispatch SHALL be deferred.

#### Scenario: Dispatch step 0 sub-task when swarm is fresh
- **WHEN** `Dispatch` is called for a sub-task with `step_index = 0`
- **AND** no other sub-tasks exist with a lower step index
- **THEN** the sub-task SHALL be dispatched normally

#### Scenario: Dispatch step 1 sub-task before step 0 is done
- **WHEN** `Dispatch` is called for a sub-task with `step_index = 1`
- **AND** at least one sub-task with `step_index = 0` is non-terminal
- **THEN** `Dispatch` SHALL return error "step 1 is blocked: step 0 has non-terminal sub-tasks"
- **AND** a `cap_deferred`-style event SHALL be recorded for the conductor

#### Scenario: Dispatch step 1 sub-task after step 0 is terminal
- **WHEN** `Dispatch` is called for a sub-task with `step_index = 1`
- **AND** all sub-tasks with `step_index = 0` are `done` or `cancelled`
- **THEN** the sub-task SHALL be dispatched normally

#### Scenario: Dispatch within a step is still concurrency-capped
- **WHEN** `Dispatch` is called for a sub-task in the active step
- **AND** the active worker count is already at `MaxConcurrentAgents`
- **THEN** dispatch SHALL still be deferred per the existing concurrency cap behavior

### Requirement: Conductor is notified when a step completes

When every sub-task in the current active step becomes terminal and there are remaining steps, the conductor SHALL receive an `all_idle` event with a `step_completed` indicator and the completed step index.

#### Scenario: Step 0 completes with more steps pending
- **WHEN** the last non-terminal sub-task in step 0 transitions to `done`
- **AND** the plan has a step 1
- **THEN** the conductor SHALL receive an `all_idle` event whose payload includes "step 0 completed"
- **AND** a `next_step_ready` event SHALL be recorded in the inbox

#### Scenario: Final step completes
- **WHEN** the last non-terminal sub-task in the final step transitions to a terminal state
- **AND** there are no further steps
- **THEN** the conductor SHALL receive the existing `all_idle` event prompting `legato swarm finish`

### Requirement: Explicit step advancement

The system SHALL provide a `NextStep` operation that advances the swarm to the next step. It SHALL only succeed when the current step is entirely terminal. After advancement, sub-tasks in the next step become eligible for dispatch.

#### Scenario: NextStep succeeds when current step is terminal
- **WHEN** `NextStep(parentID)` is called
- **AND** all sub-tasks in the current active step are `done` or `cancelled`
- **AND** there is a next step
- **THEN** the swarm's current step SHALL advance to the next step index
- **AND** an `EventSwarmChanged` SHALL be published
- **AND** the method SHALL return nil

#### Scenario: NextStep fails when current step is not terminal
- **WHEN** `NextStep(parentID)` is called
- **AND** at least one sub-task in the current active step is non-terminal
- **THEN** the method SHALL return error "cannot advance: step N has non-terminal sub-tasks"

#### Scenario: NextStep fails when there are no more steps
- **WHEN** `NextStep(parentID)` is called
- **AND** the current active step is the final step
- **THEN** the method SHALL return error "no more steps"

### Requirement: CLI supports next-step verb

The CLI SHALL support `legato swarm next-step <parent-id>` to advance a swarm to the next step.

#### Scenario: Invoking next-step from CLI
- **WHEN** `legato swarm next-step task-123` is executed
- **AND** the swarm is ready to advance
- **THEN** the command SHALL call `NextStep` and print "Advanced to step N"
- **AND** the command SHALL exit with code 0

#### Scenario: Next-step when not ready
- **WHEN** `legato swarm next-step task-123` is executed
- **AND** the current step has non-terminal sub-tasks
- **THEN** the command SHALL print the error and exit with code 1

### Requirement: TUI shows step grouping

The TUI swarm coordination panel SHALL group sub-tasks by their step index and highlight the current active step.

#### Scenario: Multi-step swarm in TUI
- **WHEN** a parent task has a multi-step swarm
- **THEN** the coordination panel SHALL display each step name and its sub-tasks grouped together
- **AND** the current active step SHALL be visually distinguished
- **AND** completed steps SHALL be collapsed or marked as done

#### Scenario: User advances step from TUI
- **WHEN** the current step is terminal and the user presses the advance keybinding
- **THEN** the TUI SHALL call `NextStep` via the swarm service
- **AND** on success, the panel SHALL refresh to show the next step as active
