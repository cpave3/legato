## Workflows: internals

The user guide, with the YAML schema, executors, templates, triggers and CLI, is [Workflows](../../site/src/content/docs/using/workflows.md) (on the site at `/legato/using/workflows/`). This file keeps what an agent working on Legato's code needs beyond it. Parsing and discovery are in `internal/engine/workflow/`; runs, executors and triggers in `WorkflowService` (`internal/service/`); see [Packages](packages.md). The web pages are in [Web UI](web-ui.md) § Workflows; the CLI in [CLI](cli.md).

### Runs and tasks

- A run creates one local execution task in the reserved **Workflows** workspace (`ViewAll` excludes it; `ViewWorkspace(id)` opts in). A succeeded run moves it to **Done** and archives it at once.
- A `task` input's selected task is stored on the run as `related_task_id`. Agent sessions use the execution task, so an existing task's active agent session is not replaced. `GET /api/tasks/{id}/activity` lists related runs and their managed artifacts.
- Deterministic steps execute synchronously until an agent step starts; the run then advances when that agent completes or fails its step through the CLI callback.
- A step whose executor returns `WorkflowStepWaiting` (`"waiting"`, `workflow_waiting.go`) waits on that executor's own service. The step and its attempt are `waiting` (the run stays `running`; a run that is `waiting` is one at a manual gate) and carry no agent session, so `endGoneSession` does not fail them and a restart leaves them as they are. The service finishes the step with `WorkflowService.CompleteWaiting` (its outputs as a deterministic executor returns them, then the steps after it run) or `FailWaiting`, and finds its steps again after a restart with `WaitingAttempts(executorType)`. `Cancel` calls the executor's `Cancel` for a waiting attempt, as for a running one. Such a step's service may act on an outside service: the one exception to schema v1's rule against outside publishers is `github.pr.review_passes` (LEG-253), whose loop posts to GitHub; see [Integrations](integrations.md) § Review loops on any pull request.
- Embedded examples (`internal/engine/workflow/examples/*.workflow.yaml`, `workflow.Examples`, `ExampleNamed`) are definitions Legato ships for people to add; `TestEveryEmbeddedExampleValidates` parses each. `review-pr-passes` is the first. An optional input a step references needs a `default`, or resolving it fails as unavailable.
- `WorkflowService.Show` fills `WorkflowRunView.Details` (by step ID) from each step's latest attempt whose executor is a `WorkflowStepDetailer`; the review step's is `ReviewRunDetails` (phase, pass, a summary line, blocking open, reason, every review posted). A run's loop publishes `EventWorkflowChanged` with kind `review_loop` (`service.ReviewLoopChangedKind`), which `Server.StartReviewRunEvents` forwards as `workflow_changed`, so an open run page follows the loop.
- `task.note` takes `task` as a task input or a task ID as text; `optional: true` (a boolean, checked at validation) makes an empty task a skip (`note_id` empty, `skipped: true`) instead of a failure.
- `task.move` calls the sync service's `PushMove` in a configured provider runtime, which may queue the mapped remote transition even for a same-column no-op, so a previously failed push can recover. Success confirms local persistence and queueing, not remote completion.
- `agent.run` takes an optional `with.engram` (text, templated like the step's other fields; anything else fails validation, and a template that resolves to anything but text fails the step): the agent starts as that engram (`AgentSpawnOptions.Engram` with `EngramRequired`). An engram `engram identity list` does not list fails the step before anything starts; any other boot failure starts the agent without it, as an interactive spawn does (the warning goes to the log and Attention). A workflow never reads or writes engram bindings: its spawn does not touch `last_used_at`. Finishing a workflow agent (`FinishWorkflowAgent`) ends its engram session (`EndEngramSession`). See [Engram sessions](engram.md).
- `WorkflowService.SetAgentKinds` gives the service the configured adapters: an `agent.run` step whose `agent` is known from the inputs alone and names no configured adapter is refused when the run starts, and no run is created.

### Agent completion

Completion is persisted and downstream steps are advanced before Legato removes the completed agent session, because the callback (`legato workflow step complete`) may be running inside that session. When the step is recorded but the next step cannot start, `Complete` returns a `*StepRecordedError` (matched by `ErrWorkflowStepRecorded`) that says the step was completed and why the next one failed, so a caller does not retry a completion that already took; the run is failed at the next step as before. Learn's borrowed-agent interview completes through `legato learn reflection submit`, which persists the typed reflection before completing its exact waiting attempt ([Legato Learn](learning.md)).

Managed artifact collection is replay-safe: rerunning an interrupted completion callback reuses an existing `(attempt, output)` artifact only when its source path and SHA-256 digest still match; changed content is rejected. Accepted files are copied atomically into managed storage so worktree cleanup does not destroy them.

### HTTP API

Start a run (the `legato-job` example from the user guide):

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"inputs":{"prompt":"Find why the sync test is flaky","repo":"cpave3/legato"}}' \
  https://server:3080/api/workflows/legato-job/runs
```

`GET /api/workflow-runs` returns each run with `related_task_title` (also on `GET /api/workflow-runs/{id}`) and `steps`, a summary of its steps in order (`step_id`, `title`, `status`, `trigger`), which the web UI's runs list draws its step bar and sentence from (the handler sets `WorkflowRunFilter.WithSteps`; the service's own scans of runs, such as the attention count, leave it off). It takes optional `status`, `workflow_name`, `limit` (1–100) and `input.<name>=<value>` parameters. Multiple input filters are ANDed and compare normalized scalar JSON values, so numeric and boolean inputs can be given as query text. Results are newest first. Input names that cannot be represented safely as a direct JSON object key, repeated singleton or input values, empty input values and out-of-range limits return `400` rather than silently broadening the query.

### Live updates and stalls

Run views update through targeted `workflow_changed` events. When an agent activity transition belongs to an active `agent.run` attempt, the server resolves its task to the persisted run and step and emits that targeted event; unrelated `agents_changed` events do not reload workflow runs or definitions. An unfinished `agent.run` step needs attention while its agent is waiting or idle. A detected exit (including a pane back at a shell) fails every waiting attempt tied to that exact session (owned `agent.run` and borrowed `agent.interview`) with `agent exited before step completion`, rather than leaving it at `waiting_agent`; Retry step can start it again. The Workflows navigation item takes its red count from the navigation summary (`/api/navigation-summary`); run rows explain the stall, and the run page offers **Prompt to continue** for a live stalled agent. Legato does not prompt automatically, to avoid duplicate or unsafe agent work. The Runs tab loads only the runs; the Workflows tab loads the definitions and the runs (for each workflow's last run). On a phone, run rows stack, steps stay one to a line, wide values scroll inside their box, and actions are touch-sized.

The card context menu's Workflows flyout on the web board scrolls independently and flips around viewport edges; cards also expose an action button for keyboard, touch and assistive-technology access.

### Extension model

Workflow persistence and UI are executor-neutral. A future deterministic executor implements the service executor contract and renders type-specific details without replacing runs, steps, attempts, outputs or artifacts. DAGs, parallel execution, schedules, shell plugins, approval gates and native external publishers (but for `github.pr.review_passes`) are not part of schema version 1.

## Triggers on a worker's handed-off cards

A card a main handed off to this Legato is the main's (LEG-243). `WorkflowTriggerCoordinator.process` checks `store.OpenHandoffForTask` before anything else and marks the trigger request `skipped` with the reason ("the card was handed off from <main>, whose workflows run when it returns; set handed_off: true on the trigger to run on such cards") unless the trigger sets `handed_off: true` (`TaskEnteredColumnTrigger.HandedOff`, schema v1). The built-in learning triggers are skipped the same way, so a worker's curator does not curate a main's task into the worker's ledger. The main's own triggers run when the task returns into another column (a `handoff` transition; see [Integrations](integrations.md) § Handing off a task). Review passes are not workflows and are unaffected. The Workflows page says "cards handed off from a main included" on a trigger that opts in; trigger requests themselves, skipped ones included, are not listed by any route yet.

## Steps on a task that is away

A task away on a worker is read-only on the main (`RefuseAway`). `task.move` was already refused through `moveCard`; `task.metadata.set` now asks `RefuseAway` too and fails the step with its words (LEG-239). `task.note` still adds the note on the main, where it is when the task returns, and adds `away_on` and `warning` to its outputs: the note is not forwarded, so the worker's agent works without it. Forwarding it to the worker as a main note was the alternative; it would need a handoff route of its own and an agent told mid-turn, and the operator can message the agent instead.
