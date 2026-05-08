## ADDED Requirements

### Requirement: Plan format

A swarm plan SHALL be a YAML document with a top-level `swarm` map (parent_task_id, working_dir, summary) and a `subtasks` list. Each sub-task SHALL specify a title and prompt; scope, role, and agent are optional.

#### Scenario: Valid plan structure

- **WHEN** the conductor writes a plan file with `swarm.parent_task_id`, `swarm.working_dir`, `swarm.summary`, and at least one `subtasks` entry containing `title` and `prompt`
- **THEN** validation SHALL accept the plan

#### Scenario: Missing required field

- **WHEN** a plan omits `swarm.parent_task_id` or `swarm.working_dir` or has no `subtasks`
- **THEN** the system SHALL reject the plan with a specific validation error naming the missing field

#### Scenario: Sub-task prompt fallback

- **WHEN** a sub-task entry omits `prompt`
- **THEN** the validator SHALL accept the entry
- **AND** the system SHALL render a default brief at dispatch time including the parent description, sub-task title, scope (if any), and a completion-instruction footer

#### Scenario: Scope glob validation

- **WHEN** a sub-task entry includes `scope` patterns
- **THEN** the validator SHALL reject malformed glob patterns at validation time

#### Scenario: Plan size cap

- **WHEN** a plan exceeds `cfg.Swarm.MaxSubtasksPerPlan` (default 10) sub-tasks
- **THEN** the system SHALL reject the plan with a message indicating the cap and the count

### Requirement: Plan submission CLI

The system SHALL provide `legato swarm propose-plan <plan-file>`. The call SHALL block until the user renders a verdict (approve, edit-and-approve, or reject) via the TUI overlay, then return a structured result.

#### Scenario: Approve returns success

- **WHEN** the user approves the plan via the overlay
- **THEN** the CLI SHALL exit zero with stdout `{"status":"approved","plan_path":"<path-to-final-plan>"}`
- **AND** the final plan path SHALL match the input path (no edits)

#### Scenario: Edit-and-approve returns success with edited path

- **WHEN** the user opens the plan in `$EDITOR` from the overlay, modifies, and approves
- **THEN** the CLI SHALL exit zero with stdout `{"status":"approved","plan_path":"<path>"}`
- **AND** the input file SHALL contain the user's edits

#### Scenario: Reject returns rejection notes

- **WHEN** the user rejects the plan with notes
- **THEN** the CLI SHALL exit zero with stdout `{"status":"rejected","notes":"<text>"}`
- **AND** the system SHALL deliver the notes via `send-keys` to the conductor's pane formatted as `[swarm event] plan rejected: > "<notes>"`

#### Scenario: Headless auto-approve

- **WHEN** the call is invoked with `--auto-approve` and no TUI is running
- **THEN** the system SHALL accept the plan without surfacing the overlay
- **AND** the CLI SHALL exit zero with stdout `{"status":"approved","plan_path":"<path>"}`

#### Scenario: No TUI present without auto-approve

- **WHEN** the call is invoked without `--auto-approve` and no TUI instance is running
- **THEN** the call SHALL block indefinitely (or until the user starts a TUI that picks up the proposal)

### Requirement: Plan approval overlay

The TUI SHALL surface a plan-approval overlay when an IPC `plan_proposed` message arrives. The overlay SHALL render the plan summary as markdown and show each sub-task with its title, role, agent, scope, and prompt.

#### Scenario: Overlay opens on IPC

- **WHEN** any running TUI instance receives an IPC message `{type: "plan_proposed", task_id: <parent-id>, content: <plan-path>}`
- **THEN** the overlay SHALL open immediately on top of the current view

#### Scenario: Approve key

- **WHEN** the overlay is open and the user presses `y`
- **THEN** the overlay SHALL close
- **AND** the system SHALL send an IPC reply `{type: "plan_verdict", task_id: <parent-id>, status: "approved"}` that unblocks the conductor's `propose-plan` call

#### Scenario: Edit key

- **WHEN** the user presses `e`
- **THEN** the overlay SHALL invoke `$EDITOR` on the plan YAML file
- **AND** on editor exit the overlay SHALL re-validate the plan and re-render
- **AND** subsequent `y` SHALL approve the edited plan

#### Scenario: Reject key with notes

- **WHEN** the user presses `n`
- **THEN** the overlay SHALL prompt for rejection notes via a single-line text input
- **AND** on submit the overlay SHALL close and send an IPC reply with `status: "rejected"` and the notes

#### Scenario: Cancel without verdict

- **WHEN** the user presses `esc`
- **THEN** the overlay SHALL close without sending a verdict
- **AND** the conductor's `propose-plan` call SHALL remain blocked
- **AND** the user can re-open the overlay later via a board action (so the gate is not permanently lost)

### Requirement: Plan persistence

The plan file produced by the conductor SHALL be persisted alongside the parent task's working directory under `<working-dir>/.legato/plans/<parent-task-id>-<timestamp>.yaml` and SHALL remain on disk after approval as a record of what was dispatched.

#### Scenario: Plan written to canonical location

- **WHEN** the conductor calls `propose-plan` with a plan path
- **THEN** the system SHALL copy the plan to `<working-dir>/.legato/plans/<parent-task-id>-<timestamp>.yaml`
- **AND** the canonical path SHALL be the one returned in the CLI output

#### Scenario: Multiple plans for one swarm

- **WHEN** the conductor proposes a second plan after the first was approved (re-planning)
- **THEN** both plans SHALL be retained on disk distinguishable by timestamp
