# Attention

Attention is Legato's single operational dashboard for work that needs human intervention, review, or merge remediation. Feature surfaces remain authoritative for their full backlog and history; the core service centrally controls which reliable semantic signals reach Attention, and web/TUI/CLI/Stream Deck consume that same policy.

## Architecture

### Durable registry

- `internal/engine/store/migrations/042_attention_items.sql` and `059_attention_signal_kind.sql` — `attention_items` table with unique `(producer, source_key)`, a persisted `signal_kind`, status (`open`/`dismissed`/`resolved`), revision-aware dismissal, and indexed active/producer/task/signal queries.
- `internal/engine/store/attention.go` — idempotent upsert, revision-conditional dismissal, resolution, filtering, and snapshot reconciliation.

### Generic service

`internal/service/attention.go` owns validation, the central default signal allowlist, producer/handler registration, list/count consistency, revision-safe action execution, and reconciliation. A producer snapshot failure preserves its last-known items. One source event can reconcile multiple producers; this matters for shared agent lifecycle events consumed by both workflow and standalone-agent projections.

A producer implements:

```go
type AttentionProducer interface {
    Producer() string
    Snapshot(context.Context) ([]AttentionInput, error)
}
```

Executable source actions additionally implement `AttentionActionHandler`. Navigation-only producers, such as reviews, PR blockers, and standalone agents, leave remediation in their authoritative feature surface.

### Producers and event subscriptions

| Producer | Included signals | Reconciliation events |
|---|---|---|
| `plans` | Proposed revisions awaiting a current user decision | `EventPlanChanged` |
| `reviews` | Explicitly ready tours with visible unreviewed work | `EventReviewChanged` |
| `pull_requests` | Open non-draft tracked PRs with a required review, changes requested, failed CI, or a combination; one item per PR, so each PR of a stack (`task_branches`) has its own (source key `<task>:<number>`). Failed CI is left out while an open Argus case targets the PR | `EventPRStatusUpdated`, and after each Argus push |
| `agents` | Running standalone interactive agents explicitly reporting waiting or error | `EventAgentActivityChanged`, start/death/completion lifecycle |
| `workflows` | Manual gates, workflow agents waiting for input, failed steps | Workflow and agent lifecycle events |
| `workflow_triggers` | Missing input, confirmation, and failed automatic triggers | `EventWorkflowTriggerChanged` |
| `approvals` | Pending approval requests (agents' permissions and questions), with their options as actions; see [Approvals](approvals.md) | `EventApprovalChanged` |
| `devices` | The last `legato device reset`, until dismissed; see [Approvals](approvals.md) § Signed approvals | `EventApprovalChanged` |
| `argus` | Argus's open CI cases on the person's PRs (`argus_case`, `argus_needs_you`), pushed by Argus and put on the task that owns the PR; navigation `external`; see [Integrations](integrations.md) § Cases in Attention | After each push, `EventPRStatusUpdated`, `EventCardsRefreshed` |
| `settings` | Settings changed since this process started that apply only at a restart (`settings_restart`): "Restart to apply N changes", the keys in the detail, with **Restart Legato** (`execute`, `restartAll`) and **Open settings**; revision is the waiting keys. See [Settings](settings.md) | `EventSettingsChanged` |
| `settings-move` | The one offer to move the settings `config.yaml` pins into the database (`settings_move`), while any can move; navigation `settings#move`; revision `offer-1`, so a dismissal is kept. See [Settings](settings.md) § Writing config.yaml, and the move | `EventSettingsChanged` |
| `restart` | A restart of Legato that failed and was rolled back in the last week (`restart_rolled_back`, high urgency): the reason and who asked, and the failure (the configuration did not load, or Legato was not healthy within 30 s); navigation `settings#updates`. See [Releases and updates](updates.md) § Rollback | none (read at start, after the rollback's own restart) |
| `engram` | A running agent asked to start as an engram that did not boot, so it runs as a normal session (`engram_boot_failed`, normal urgency): "<engram> did not boot", engram's message in the detail; on the task, navigation `task`. Kept while that execution runs (`store.ListEngramBootFailures`: `executions.engram_boot_failed` and `engram_boot_error`, migration 105). See [Engram sessions](engram.md) | `EventAgentStarted`, `EventAgentDied`, `EventAgentCompleted` (a finished workflow agent) |
| `updates` | A newer release of Legato waiting for the person (`update_available`): in ask mode, with **Update and restart**, or after an install failed, with **Try again**; navigation `settings#updates`; see [Releases and updates](updates.md) | `EventUpdateChanged` |
| `integrations` | Delegations that did not reach an agent (`delegation_failed`), agents' replies Argus never took (`case_reply_failed`), a connection Argus refused (`integration_disconnected`); the last 7 days; see [Integrations](integrations.md) | `EventDelegationChanged` |
| `handoff` | On a main: away tasks done on their worker (`handoff_done`), failed there (`handoff_failed`), lost (`handoff_lost`) or whose review is stuck (`handoff_review_stuck`); workers holding away tasks that refuse this main (`worker_not_accepting`) or have been unreachable 15 minutes (`worker_unreachable`); see [§ Handoff](#handoff) | `EventCardUpdated` (rows), `EventCardsRefreshed` (pulls, reach, return, recall, take back), and every 30 seconds |

Startup reconciliation remains the safety net. Source events reconcile the relevant projections without requiring an Attention request to query GitHub or mutate source state.

`agent_state` IPC still publishes `EventCardUpdated` and web agent invalidation, and now also publishes `EventAgentActivityChanged`. `EventPRStatusUpdated` retains its existing `cards_changed` behavior while independently reconciling PR attention.

### Revision and dismissal semantics

Each producer builds a revision from meaningful source state. Dismiss suppresses only that exact revision. A changed PR blocker, agent activity, review handoff, plan, or workflow state can resurface; an unchanged snapshot cannot. When a successful snapshot omits a previous source key, reconciliation resolves the stale item.

## Default signal policy

Attention deliberately includes only states the local durable data can support reliably:

- **Needs intervention:** plan decisions, workflow gates/failures/input, standalone agent waiting/error, pending approvals, and a device reset.
- **Needs intervention** also: a restart of Legato rolled back (`restart_rolled_back`).
- **Needs intervention** also: an agent whose engram did not boot (`engram_boot_failed`), while it runs.
- **Needs intervention** also: an away task failed or lost on its worker, and a worker that no longer accepts this main or has been unreachable for 15 minutes.
- **Ready for review:** tours explicitly marked ready through `legato review ready` and still containing visible unreviewed work. Also an away task done on its worker (`handoff_done`), waiting to be returned.
- **Merge blockers:** open, non-draft tracked PRs whose persisted review decision is `REVIEW_REQUIRED` or `CHANGES_REQUESTED`, whose persisted check status is `fail`, or a combination.

The following do not raise Attention:

- an empty review decision or pending CI, because these do not establish an actionable merge blocker;
- total PR comment count, because it does not identify unresolved actionable feedback;
- draft, closed, or merged PRs;
- inferred GitHub mergeability, branch protection, conflicts, or merge-queue state, which are not persisted;
- idle activity or process termination as an agent completion claim;
- an away task whose agent needs the person (the worker's Attention and notification cover it; the Away lane shows a pill);
- workflow/swarm-owned sessions in the standalone agent producer, preventing duplicate ownership;
- successful workflow history and Learning records.

Failures use high urgency; blocking decisions and waiting states use normal urgency. The unfiltered API and `AttentionService.Count` apply the same allowlist, so navigation badges and generic clients agree.

## HTTP API and events

- `GET /api/attention` — active default-policy items, optionally filtered by producer/category/urgency/task/workspace.
- `POST /api/attention/{id}/actions/{action_id}` — execute a declared source action at an expected revision.
- `POST /api/attention/{id}/dismiss` — dismiss the expected revision.
- `EventAttentionChanged` is forwarded as `attention_changed`; web clients refetch the authoritative snapshot.

All endpoints use the configured authentication boundary.

## Web dashboard

`/attention` renders three fixed sections with counts and section-level all-clear states:

1. **Needs intervention**
2. **Ready for review**
3. **Merge blockers**

`argus_case` joins Merge blockers; `argus_needs_you` falls to Needs intervention. `handoff_done` joins Ready for review; the other handoff signals fall to Needs intervention. Unknown future allowed signals fall back to Needs intervention rather than disappearing. A detail is drawn as Markdown without raw HTML or images, folded to a few lines with More when long. An item with `external` navigation opens that address in a new tab ("Open in <producer>") and, when it has a task, also offers Open task (`/board?highlight_task=`). Cards retain source, urgency, reason/detail, relative time, executable source actions, revision-scoped Dismiss, and direct navigation: a `task` navigation opens `/board?highlight_task=<id>`, which selects the task's card, or its row in the Away lane when the task is away on a worker. Dismiss is visually secondary to the source-owned action or Open. An action may carry `confirmation`, text the page asks with `window.confirm` before running it (Take back), and a navigate action may carry `href`, its own address instead of the item's navigation: an http(s) address opens in a new tab (Open on worker), a path is a link in the web UI. A larger global all-clear appears only when all sections are empty. Controls wrap below content on narrow screens.

## Other clients

- **TUI:** `I` opens the shared generic list; `j`/`k`, Enter, `D`, and `q` retain their existing behavior. Enter on an item with `external` navigation and no executable action opens its address in the browser. Enter on a `handoff` item runs nothing and says "Return, recall and take back from the web UI", as the board refuses away tasks; `D` dismisses it.
- **CLI:** `legato attention list` reconciles CLI-available sources (plans, and `handoff` from the database, without notifying) and lists the durable default-policy set with producer/category/urgency/JSON options.
- **Stream Deck and navigation badges:** consume the same expanded count/list policy.

These clients intentionally remain generic lists in this delivery; only the web surface adopts grouped dashboard presentation.

## Handoff

`internal/service/handoff_attention.go`: `HandoffAttentionAdapter` (producer `handoff`, `HandoffAttentionProducer`) shows what a main's away tasks and workers need from this main only: Return, Recall, Take back, pairing again. The decisions are in Burnlite LEG-11; the handoff itself is in [Integrations](integrations.md).

| State | Signal, source key, revision | Section, urgency | Actions | Phone |
|---|---|---|---|---|
| The worker's last state `done` | `handoff_done`, the handoff ID, `done` | Ready for review, normal | `return` (execute), `open_worker`, `open` | no |
| `failed` | `handoff_failed`, the handoff ID, `failed:<reason>` | Needs intervention, high | `recall` (execute), `open_worker`, `open` | yes |
| The review is stuck (`AwayTask.Review.Stuck()`, not done) | `handoff_review_stuck`, the handoff ID, `review:<pass>:<reason>` | Needs intervention, high | `return` ("Return as is"), `another_pass`, `message` (`requires_text`), `recall` (`requires_confirmation`), `open_worker`, `open` | yes |
| Lost on the worker (`lost_at`) | `handoff_lost`, the handoff ID, `lost:<lost_at>` | Needs intervention, high | `take_back` (execute, `requires_confirmation` with a `confirmation` text), `open` | yes |
| A worker's `refused_at` (401) | `worker_not_accepting`, `worker:<id>:refused`, `refused_at` | Needs intervention, high | `pair` (navigate) | yes |
| A worker's `unreachable_since` at least 15 minutes ago (`WorkerUnreachableAttentionAfter`) | `worker_unreachable`, `worker:<id>:unreachable`, `unreachable_since` | Needs intervention, high | `open` (navigate) | yes, once per outage |

- Task items are built from the away tasks (`store.ListAwayTasks`) with the worker's **last known state**, not the main's view of the worker, so a short outage neither hides a done or failed task nor raises it again. Lost wins over the state. Needs you, working, idle and the rest raise nothing. A task item navigates to the task (`/board?highlight_task=`, which selects its row in the Away lane); `open_worker` has `href`, the away task's `WorkerURL`. The title is the main's task title: "<task> is done on <worker>", "Handing <task> to <worker> failed", "<task> is lost on <worker>".
- Worker items are raised only for a listed worker (`ListWorkers`, so not one removed and kept only for Leave) that holds away tasks, and navigate to `settings#workers`. The unreachable title is "<worker> unreachable since <time>, N away tasks", the time of day in `HandoffAttentionOptions.Location` (the date too when not today).
- Items resolve when a successful snapshot no longer has them: a row or pull shows another state, the worker answers again, or the away task closes (Return, Recall, Take back, which publish `EventCardsRefreshed`). Dismiss is revision-scoped as everywhere; a done item dismissed comes back when the task is done again after leaving Done.
- `Execute` looks the away task up by the handoff ID (the source key) and calls `TaskHandoffService.Return`, `Recall`, `TakeBack`, `AnotherPass` or `MessageAuthor` (with the action's text). A stuck review's item gives the reason, the pass and the open blocking findings, comments and checks; it is the only item review passes raise: a running review, or one waiting for the author, raises nothing. Without `Handoffs` the adapter has no handler.
- **Phone.** `HandoffAttentionOptions.Notifier` (ntfy) and `ServerURL` (as the approval notifications work it out). Each snapshot claims every notifying state with `store.ClaimHandoffNotice("<source key>@<revision>")` (`handoff_notices`, migration 088) and sends only the ones it claimed, in the background, with a link to `<server>/board?highlight_task=<task>` or `<server>/settings#workers`. The claim makes the TUI and `legato serve` on one database (each with its own worker connections and producer) notify once, and a restart notify nothing again; a new outage has a new `unreachable_since`, so it notifies again. The link is a web address: the Android app has no deep link for it. Off with `notifications.handoff: false`.
- **Wiring.** `mainHandoffs.registerAttention` (`cmd/legato/worker_connections_runtime.go`) makes the adapter once per process, with the first Attention service (`wireAttention` for `legato serve` and the TUI's web server, and the TUI's own), registers it on each (`HandoffAttentionAdapter.Register`: the producer on `EventCardUpdated` and `EventCardsRefreshed`, and its handler), and starts `Watch`, a reconcile every 30 seconds for the 15-minute threshold.
- Tests: `internal/handofftest/attention_test.go`, through the two-instance harness with the main's clock.

