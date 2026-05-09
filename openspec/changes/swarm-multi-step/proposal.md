## Why

Current swarm execution dispatches every sub-task in a single plan concurrently (up to the concurrency cap). For complex work—e.g., scaffolding a new application and then building features on top of that scaffolding—this flat model forces users to either (a) over-specify upfront or (b) manually orchestrate multiple swarms. A tiered, multi-step plan lets the conductor validate each phase before the next one begins, reducing waste and improving reliability.

## What Changes

- **Plan structure** — `Plan.Subtasks []PlanSubtask` becomes a slice of `Step`, where each `Step` has a `Name` and its own `[]PlanSubtask`. A plan can still be a single-step plan (backwards-compatible at the YAML level via a one-item `steps` list).
- **Validation** — `ValidatePlan` validates per-step sub-task limits in addition to overall limits.
- **Dispatch gating** — The `ApplyApprovedPlan` and `Dispatch` methods are step-aware. Sub-tasks in *step N* are not dispatched until *all* sub-tasks in steps `< N` are terminal (`done`, `cancelled`, or `reporting` with explicit conductor close).
- **Conductor notification** — When a step completes, the conductor receives an `all_idle` event with a `step_completed` flag, prompting it to call `legato swarm next-step <parent-id>` (or the equivalent UI action) to unlock the next step.
- **TUI update** — The swarm coordination panel shows the current active step, its sub-tasks, and queued future steps.
- **CLI addition** — New `legato swarm next-step <parent-id>` verb to advance to the next step.
- **Database migration** — Add `step_index` (integer, 0-based) to `swarm_subtasks` table.
- **BREAKING**: The plan YAML format changes from top-level `subtasks:` to `steps:` containing `name:` + `subtasks:`. Existing single-step plans must be restructured.

## Capabilities

### New Capabilities
- `swarm-multi-step`: Multi-step swarm execution with step-by-step gating, step-aware dispatch, and conductor-driven advancement between steps.

### Modified Capabilities
- (none — no existing spec-level capability is having its requirements changed; this introduces a new orthogonal capability)

## Impact

- `internal/engine/swarm/plan.go` — plan structure, parse, validation
- `internal/engine/swarm/plan_test.go` — updated tests
- `internal/engine/store/migrations/` — new migration `016_swarm_step_index.sql`
- `internal/engine/store/swarm.go` — step-aware queries
- `internal/engine/store/types.go` — `Subtask.StepIndex` field
- `internal/service/swarm.go` — step-gating in `ApplyApprovedPlan`, `Dispatch`, `maybeNotifyAllIdle`, new `NextStep` method
- `internal/server/swarm.go` — new `nextStep` HTTP handler
- `internal/server/server.go` — route registration
- `internal/cli/swarm.go` — new `legato swarm next-step` CLI verb
- `internal/tui/app.go` — swarm panel displays step grouping and "advance" action
- `config/config.go` — `MaxStepsPerPlan` default
