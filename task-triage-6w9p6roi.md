# Triage: Introduce an agent Error working state

**Task ID:** `6w9p6roi`  
**Disposition:** Ready for implementation  
**Estimated complexity:** Medium — the hook is small, but activity is rendered and summarized in several clients.

## User outcome

When Chimera fires its future `OnError` hook for a Legato-managed agent, Legato records that agent's activity as `error`, immediately shows **ERROR** in red anywhere live agent state is displayed, and sends an error notification. A subsequent normal Chimera event can move the same running session back to working, waiting, or idle.

## Current behavior

- Live activity is stored as an unconstrained string in `agent_sessions.activity`; the established values are `working`, `waiting`, and `""` for idle (`internal/engine/store/agent_session.go`, `internal/service/agent.go`).
- Execution activity is constrained in both Go and SQLite to `working` or `waiting` (`internal/engine/store/execution.go`, `internal/engine/store/migrations/055_executions.sql`). Chimera hooks include an execution ID, so merely writing `error` to the live session would make the same hook fail while recording its execution interval.
- Chimera hook installation is table-driven. It currently installs `SessionCreated`, `UserPromptSubmit`, `PostToolUse`, `PermissionRequest`, `Stop`, `SessionEnd`, `Interrupt`, and `Timeout` scripts under `~/.chimera/hooks/<EventName>/` (`internal/engine/hooks/chimera.go`). There is no `OnError` entry yet.
- Parent-session hook reports update live activity, execution activity, broadcast an `agent_state` IPC event, and notify only for `working -> waiting/idle` (`internal/cli/cli.go`). Child Chimera sessions are resolved by session ID and can notify on `SessionEnd` without changing the parent.
- `MaybeNotifyAgent` only formats readiness notifications: title `Agent ready`, with waiting/idle body text (`internal/service/agent.go`). Push notifications honor the task's notification preference and rate limit; configured OS notifications fire regardless of that per-task preference.
- Unknown non-empty states currently degrade incorrectly in several places: TUI agent views and Stream Deck treat them as idle; `agent summary` groups them into the idle count; workflow attention reports them as “agent idle before step completion.” The web board omits an indicator for an unknown non-empty state.

## Implementation scope

### 1. Define and persist `error` activity

- Add `store.ActivityError = "error"` beside `ActivityWorking` and `ActivityWaiting` in `internal/engine/store/types.go`.
- Accept `ActivityError` in `Store.RecordExecutionActivityAt` in `internal/engine/store/execution.go`.
- Add migration `056_...sql` that rebuilds `execution_activity` with its state check expanded to `('working', 'waiting', 'error')`, preserving rows and recreating:
  - `idx_execution_activity_execution_time`
  - `idx_execution_activity_range`
  - `idx_execution_activity_one_open`
- Do not add error duration to productivity report totals. Existing working/waiting totals remain semantically unchanged. The generic duration map may contain `error`, but `service.DurationData` and report DTOs continue to expose only working and waiting.
- Include error intervals in `GetStateTimeline`: track error overlap per bucket and return `error` when it is the largest overlap. Use deterministic precedence for ties, with `error` taking precedence over waiting and working so a visible failure is not hidden by equal-duration activity.

No migration is required for `agent_sessions.activity`, which has no value check. The legacy `state_intervals` table is superseded by execution activity and should not be revived.

### 2. Install the Chimera `OnError` hook

- Add an entry to `chimeraScripts` in `internal/engine/hooks/chimera.go`:
  - event directory: `OnError`
  - script: `legato-on-error.sh`
  - activity: `error`
- Reuse `chimeraAgentStateScript`; it does not need to parse or depend on the future hook payload. The installed command should be equivalent to:

  ```sh
  legato agent state "$LEGATO_TASK_ID" \
    --execution-id "$LEGATO_EXECUTION_ID" \
    --activity "error" \
    --working-dir "$PWD" \
    --session-id "$CHIMERA_SESSION_ID" \
    --event "OnError"
  ```

- Installation remains idempotent because the managed script is overwritten, and uninstall must remove the new script along with existing managed hooks.
- Update CLI usage/help text that currently documents only `working|waiting|""` (`cmd/legato/main.go` and relevant command metadata) to include `error`.

This assumes Chimera will scan `~/.chimera/hooks/OnError/` and provide the same environment variables as `Stop`, per the task brief. Legato should not block on Chimera implementing the event.

### 3. Send a distinct error notification

- Extend the shared notification formatter in `internal/service/agent.go` so `activity == "error"` produces:
  - title: `Agent error`
  - parent body: `Agent <task title or task ID> had an error`
  - child body: `Agent <child purpose> for <task title or task ID> had an error`
- Preserve notification channel policy: push/ntfy remains gated by the task's notify preference and rate limiter; configured OS notifications remain unconditional, matching idle/waiting behavior.
- In both activity entry points, notify when entering `error` from any different state, not only from `working`:
  - `cli.AgentState` for installed hooks
  - `agentService.SetAgentActivity` for server/prompt-driven callers
- Do not send duplicate notifications for repeated `error -> error` reports.
- For a child Chimera session, `OnError` should notify with child context and must not mutate the parent session, matching the existing child `SessionEnd` ownership behavior.
- Continue to broadcast the resulting parent activity through the existing IPC/WebSocket refresh path.

### 4. Render Error in red everywhere live activity is represented

Update explicit state switches so `error` is not treated as idle:

- **TUI agent page:** all sidebar cards, selected-agent status dot, and detail/header labels in `internal/tui/agents/model.go` should show the warning icon plus `ERROR` using `theme.SyncError`. Selected rows need a selected-background-safe dark red, analogous to the existing selected dead state.
- **TUI board:** both selected and unselected card paths in `internal/tui/board/card.go` should show warning icon plus `ERROR` in red.
- **Web board:** `web/src/components/board/BoardCard.tsx` should render a red `ERROR` badge/icon when `agent_state === "error"`.
- **Web agent navigation:** `AgentSidebar.tsx` and `AgentSelect.tsx` should render a red dot and `Error` label rather than falling through to idle.
- **Web state timeline:** error buckets should render red in `AgentSidebar.tsx`.
- **Stream Deck:** `streamdeck/internal/render/render.go` should use its existing `Red` constant for the top state bar when activity is `error`; update activity comments/types in `streamdeck/internal/legato/client.go`.
- Update stale comments on activity fields in service, board, web/Stream Deck contracts to list `error`.

The API already transports activity as a string (`AgentResponse.Activity`, board `AgentState`, WebSocket `AgentInfo.activity`), so no response-shape change is needed.

### 5. Keep summary and workflow semantics accurate

- Extend `Store.GetAgentActivityCounts`, the service interface, and `cli.AgentSummary` to carry an explicit error count instead of allowing `error` to fall into idle. Render a non-zero red tmux segment such as `1 error`; continue to always render the idle segment as today.
- Update interface implementations and fakes affected by the added return value.
- In workflow attention enrichment (`internal/service/workflow.go`), map `session.Activity == "error"` to `agent error before step completion` (or equivalent explicit error wording), while retaining existing waiting and idle reasons.

## Acceptance criteria

1. Installing Chimera hooks creates executable `~/.chimera/hooks/OnError/legato-on-error.sh`; uninstalling removes it without disturbing unrelated hooks.
2. With valid Legato task, execution, and Chimera session environment, the generated `OnError` script reports `--activity "error"` and `--event "OnError"`.
3. A parent `OnError` report changes the running agent's live activity to `error`, opens/persists an error execution interval, broadcasts the state change, and succeeds rather than failing execution-state validation.
4. A later working, waiting, or idle report closes the error interval and changes the live state normally.
5. Entering error sends exactly one `Agent error` notification per configured channel under existing channel policy; repeating error while already in error sends none.
6. A child-session `OnError` sends a child-context error notification and leaves the parent activity unchanged.
7. TUI board, TUI agent view, web board, web agent selectors/sidebar/timeline, and Stream Deck show error in red and do not label it idle.
8. `legato agent summary` reports error separately in red and does not include error sessions in idle.
9. A waiting workflow attempt whose agent activity is error is marked as needing attention with an error-specific reason.
10. Existing working, waiting, idle, dead-session, notification preference, and reporting-total behavior remains unchanged.

## Test plan

Follow the repository's TDD convention: add failing behavior tests first, then implement.

- `internal/engine/hooks/chimera_test.go`
  - Add `OnError` to the externally defined expected event table.
  - Assert install content/activity/event, executable mode, and uninstall removal.
- `internal/engine/store/execution_test.go` and migration tests
  - Record `working -> error -> working/idle`; assert one open interval at most and retained error duration.
  - Assert arbitrary states remain rejected.
  - Upgrade a pre-056 database and verify existing activity rows and indexes survive while `error` inserts succeed.
- `internal/engine/store/state_interval_test.go`
  - Assert an error-majority timeline bucket is `error`, including the documented tie precedence.
- `internal/cli/cli_test.go`
  - Exercise the public `cli.AgentState` path for parent and child `OnError` reports.
  - Assert persisted live state, execution activity, exact notification title/body, no duplicate on repeated error, and unchanged parent for child reports.
  - Assert summary has a red error count and the idle count excludes it.
- `internal/service/agent_test.go`
  - Exercise `SetAgentActivity` entering error from working/idle, notification preference behavior, OS notifier behavior, and duplicate suppression.
- `internal/service/workflow_attention_test.go`
  - Assert error activity yields an error-specific attention reason.
- `internal/tui/board/card_test.go` and `internal/tui/agents/model_test.go`
  - Assert `ERROR` appears for error activity in selected and unselected render paths and does not render `IDLE` for that agent.
- Web component tests
  - Add error cases to `BoardCard.test.tsx`, `AgentSidebar.test.tsx`, and an `AgentSelect` test (create one if absent), asserting visible Error labels and red classes/accessible titles.
- `streamdeck/internal/render/render_test.go`
  - Assert error activity uses the red state bar.
- Run `go test ./...`, the web test suite, and the Stream Deck module tests. Run `go test -race ./...` because activity changes are broadcast through event-driven paths.

## Risks and boundaries

- **Schema migration is required:** without expanding the `execution_activity` check, the installed hook updates the live row and then returns an error while recording execution state, leaving a partially applied report.
- **Error activity is not execution terminality:** `error` describes the current working state of a still-running session. It must not automatically set execution status to `failed`, kill the agent, or complete/cancel a workflow step.
- **Recovery must remain possible:** normal later hooks overwrite `error`; no manual reset or sticky-error behavior is introduced.
- **No Chimera payload dependency:** the initial integration records only the occurrence. Persisting error text/stack data is out of scope until Chimera defines and exposes that payload.
- **No new error-duration report metric:** the error interval is retained for timeline/history correctness, but aggregate productivity reports remain working/waiting-only unless separately designed.
