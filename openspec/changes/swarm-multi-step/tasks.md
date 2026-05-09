## 1. Database Layer

- [ ] 1.1 Add migration `016_swarm_step_index.sql` — `ALTER TABLE swarm_subtasks ADD COLUMN step_index INTEGER NOT NULL DEFAULT 0`
- [ ] 1.2 Add `StepIndex` field to `store.Subtask` struct in `internal/engine/store/types.go`
- [ ] 1.3 Update `CreateSubtask` query in `internal/engine/store/swarm.go` to persist `step_index`
- [ ] 1.4 Add `ListSubtasksByParentAndStep` query in `internal/engine/store/swarm.go` for step-aware lookups
- [ ] 1.5 Add `GetMaxStepIndex` query in `internal/engine/store/swarm.go` to find the highest step for a parent
- [ ] 1.6 Add `SetParentActiveStep` query in `internal/engine/store/swarm.go` to persist `active_step` on parent task
- [ ] 1.7 Add migration `017_swarm_active_step.sql` — `ALTER TABLE tasks ADD COLUMN swarm_active_step INTEGER NOT NULL DEFAULT 0`
- [ ] 1.8 Add `SwarmActiveStep` field to `store.Task` struct

## 2. Engine — Plan Structure & Validation

- [ ] 2.1 Introduce `PlanStep` struct with `Name string` and `Subtasks []PlanSubtask` in `internal/engine/swarm/plan.go`
- [ ] 2.2 Change `Plan.Subtasks` to `Plan.Steps []PlanStep` and update YAML tags
- [ ] 2.3 Update `ValidatePlan` to validate `steps:` array, per-step subtask limits, and overall subtask cap
- [ ] 2.4 Update `plan_test.go` with test cases for single-step plan, multi-step plan, empty steps, and empty step subtasks
- [ ] 2.5 Update `WriteTo` to serialize the new `steps:` shape

## 3. Service Layer — Step Gating & Advancement

- [ ] 3.1 Add `NextStep(ctx context.Context, parentID string) error` to `SwarmService` interface
- [ ] 3.2 Implement `NextStep` in `swarmService` — validates current step is terminal, advances `swarm_active_step`, publishes `EventSwarmChanged`
- [ ] 3.3 Update `ApplyApprovedPlan` to iterate over `plan.Steps`, writing `step_index` per subtask
- [ ] 3.4 Update `Dispatch` to check active step gating before spawning — block dispatch if subtask step > parent's active step
- [ ] 3.5 Update `Dispatch` to record a deferred event when step is blocked (similar to concurrency cap)
- [ ] 3.6 Update `maybeNotifyAllIdle` to detect per-step completion and emit `all_idle` with `step_completed` indicator when a step finishes and more steps remain
- [ ] 3.7 Update `Snapshot` to include `active_step` in the parent payload
- [ ] 3.8 Update `ListSubtaskInfos` to include `StepIndex` in `SwarmSubtaskInfo`

## 4. HTTP API Layer

- [ ] 4.1 Add `SwarmService.NextStep` method call to server interface (already covered by 3.1)
- [ ] 4.2 Add `swarmNextStepHandler()` in `internal/server/swarm.go` — POST `/api/swarm/next-step`
- [ ] 4.3 Register route in `internal/server/server.go`
- [ ] 4.4 Add server tests for next-step handler (success, blocked, no more steps)

## 5. CLI Layer

- [ ] 5.1 Add `SwarmNextStep` function in `internal/cli/swarm.go` — calls `NextStep` via IPC
- [ ] 5.2 Wire `next-step` subcommand in `cmd/legato/main.go` or existing CLI router
- [ ] 5.3 Add CLI tests for `swarm next-step` verb

## 6. TUI Layer

- [ ] 6.1 Update `SwarmSubtaskInfo` DTO in `internal/service/swarm.go` to expose `StepIndex`
- [ ] 6.2 Update swarm coordination panel in `internal/tui/app.go` (or dedicated panel file) to group sub-tasks by step index and highlight active step
- [ ] 6.3 Add keybinding/action to advance to next step from TUI when current step is terminal
- [ ] 6.4 Update swarm status rendering to show step names and completion state

## 7. Config & Integration

- [ ] 7.1 Add `SwarmConfig.MaxStepsPerPlan` with default (e.g., 10) in `config/config.go`
- [ ] 7.2 Wire `MaxStepsPerPlan` into `ValidatePlan` call sites
- [ ] 7.3 Update `legato swarm progress` / `legato swarm status` JSON output to include `active_step`
- [ ] 7.4 Smoke test: run a single-step plan end-to-end to confirm backward compatibility
- [ ] 7.5 Smoke test: run a two-step plan end-to-end to verify gating, notification, and manual advancement
