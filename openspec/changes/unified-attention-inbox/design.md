## Context

Legato currently has three related but separate mechanisms:

1. **Transient invalidation:** `internal/engine/events.Bus` fans domain changes to running subscribers. The TUI and web server already subscribe to agent, PR, swarm, plan, and review events, but the bus is bounded and intentionally drops events for slow subscribers.
2. **Persistent domain state:** agent sessions, pending plans, swarm events, PR metadata, and review tours/transcripts are persisted independently in SQLite. Each feature exposes its own view and action paths.
3. **Outbound delivery:** `service.Notifier` sends optional OS and ntfy notifications. Agent working-to-waiting/idle transitions currently invoke it directly, with per-task ntfy preferences and rate limiting.

No shared durable object represents “a human needs to do something.” A web modal can show a pending plan while the TUI shows an overlay; a worker question can remain in `swarm_events`; a review can become ready; and PR checks can fail, but none of these contributes to one shared queue or shared dismissal state.

This is cross-cutting work across SQLite, service orchestration, event/IPC propagation, server APIs, TUI, and React. The architecture must preserve the strict engine → service → presentation layering and reuse source-specific actions rather than moving plan, swarm, PR, or review business logic into a generic inbox.

## Goals / Non-Goals

**Goals:**

- Establish a durable, presentation-neutral model for human-attention items.
- Project actionable conditions from existing agent, plan, swarm, review, and PR state into one queue.
- Give TUI and web equivalent list, filter, count, dismiss, inspect, and primary-action behavior.
- Keep state synchronized through the existing event bus, IPC, and WebSocket invalidation paths.
- Reuse existing OS/ntfy delivery, per-task preference, and rate limiting without making delivery authoritative.
- Repair missing or stale projections after process restarts or dropped events.
- Avoid worker-progress noise by restricting the initial source set to conditions with a clear human action.

**Non-Goals:**

- Durable run history, full transcripts, session analytics, stale-work heuristics, reminders, snoozing, or escalation policies.
- Replacing source tables or specialized agent, plan, swarm, PR, and review views.
- Adding new plan verdict, worker messaging, PR mutation, or review business logic to the attention service.
- Treating ordinary idle agents, successful CI, comment-count changes, or generic progress as actionable.
- Multi-user assignment, personal read state, per-user inboxes, or role-based authorization. Legato retains its current single-trust-domain model.
- Changing the event bus into a durable broker.

## Decisions

### 1. Persist a projection, not a copy of source workflows

Add an `attention_items` table owned by the engine store. The table stores enough denormalized information to render and route an item without joining every source table:

| Field | Purpose |
|---|---|
| `id` | Stable opaque item ID |
| `source_kind` | `agent_prompt`, `plan_approval`, `swarm_question`, `agent_failure`, `review_ready`, `review_answer`, `pr_checks_failed`, or `pr_changes_requested` |
| `category` | Stable filter group: `agent`, `swarm`, `review`, or `pr` |
| `source_key` | Deterministic identity for one source condition |
| `task_id` | Task context, current-workspace join, and task-notification preference |
| `parent_task_id` | Parent grouping for swarm participants |
| `severity` | `error`, `action`, or `review` |
| `title`, `summary` | Renderable content |
| `action_kind` | Typed instruction for presentations |
| `target_json` | Versioned source/navigation target payload |
| `status` | `open`, `dismissed`, or `resolved` |
| `activated_at` | Most recent inactive → active transition |
| `created_at`, `updated_at`, `resolved_at` | Lifecycle timestamps |
| `notified_at` | Last outbound delivery for this activation |

A unique index on `source_key` gives one durable row per condition. Category is derived centrally: agent prompts and solo failures are `agent`; plans, worker questions, and swarm-participant failures are `swarm`; review-ready and review-answer items are `review`; PR checks/review regressions are `pr`. On reactivation, the service updates the row back to `open`, resets `activated_at`, `resolved_at`, and `notified_at`, and refreshes the display data. This retains a small amount of lifecycle history without introducing a separate activation table.

The source tables remain authoritative. The attention row is a query-efficient projection and shared dismissal record. Workspace filters join `task_id` (or the parent task for swarm participants) to the current task/workspace association at query time, so moving a task does not require rewriting open attention rows.

**Why:** A dynamically computed union of source queries could show current conditions but could not preserve shared dismissal, activation timestamps, notification state, or consistent IDs. Copying complete source payloads would drift and duplicate domain logic.

**Alternative considered:** Derive the queue entirely at read time from agent, swarm, PR, and review tables. Rejected because dismissal and deduplicated outbound delivery require persistence.

### 2. Put lifecycle policy in a service-layer `AttentionService`

Define a presentation-neutral interface along these lines:

```go
type AttentionService interface {
    List(ctx context.Context, filter AttentionFilter) ([]AttentionItem, error)
    CountOpen(ctx context.Context, filter AttentionFilter) (int, error)
    Get(ctx context.Context, id string) (*AttentionItem, error)
    Dismiss(ctx context.Context, id string) error
    Reconcile(ctx context.Context) error
}
```

Producer-facing methods remain narrow and typed rather than exposing arbitrary item insertion to presentations. They may be implemented as internal methods or a separate `AttentionSink` interface, for example:

```go
type AttentionSink interface {
    Activate(ctx context.Context, condition AttentionCondition) error
    ResolveSource(ctx context.Context, sourceKey string) error
}
```

The service owns:

- source-key construction,
- actionable-source policy,
- severity and action mapping,
- activation/refresh/resolution semantics,
- event publication,
- and outbound notification eligibility.

The engine store owns only CRUD/upsert/filter transactions. It does not import source services or decide what is actionable.

**Why:** The rules are business policy shared by TUI and web. Putting them in either presentation would diverge; putting them in engine would violate the project layering.

**Alternative considered:** Let every source service construct and persist full attention rows. Rejected because source-key, severity, lifecycle, and notifier rules would be duplicated across services.

### 3. Use typed source adapters at existing transition points

Integrate attention updates where Legato already knows that a source changed:

- **Agent prompt:** the agent activity transition captures the waiting pane and uses the existing engine prompt detector regardless of whether a web stream is connected. Activate only for blocking approval/question types; resolve when the prompt is submitted, dismissed as no longer applicable, the agent resumes work, or the session ends intentionally. Because detected prompt state is partly runtime-derived, startup reconciliation can only restore an agent-prompt item when current persisted waiting state plus a fresh pane detection confirms it.
- **Plan approval:** `EventPlanProposed` activates `plan:<parent>:<mode>`; plan verdict or pending-plan deletion resolves it. Extend `swarm_pending_plans` with a non-null `mode` (`normal` or `extension`) populated by every proposal path, so startup reconciliation and source routing can reconstruct the correct item after restart.
- **Swarm question:** extend `swarm_events` with `resolved_at`. `SwarmService.Question` records an unresolved question event and activates `swarm-question:<subtask>` using the latest unresolved payload. A successful reply through the existing worker-message path resolves all outstanding question events for that worker and the attention item; cancellation/close/finish does the same. Repeated questions from one worker refresh the same attention row while remaining durable source records.
- **Agent failure:** `EventAgentDied` activates a task- or subtask-scoped failure unless the death follows an intentional close, finish, cancel, or kill path. Source lifecycle completion resolves it only when the failed work is replaced, canceled, or otherwise made terminal by the user.
- **Review:** `EventReviewChanged{Kind:"ready"}` activates `review-ready:<tour>`. `Kind:"answer"` activates `review-answer:<message-id>` with tour/step routing in the target; this allows a later answer on the same step to resurface after an earlier answer was dismissed. `reviewed`, `deleted`, and `restarted` resolve both the tour's ready item and all answer items immediately. Opening an answer does not silently resolve it; explicit dismissal remains available.
- **PR:** after `PRTrackingService` compares old and new metadata, transitions into failed checks or changes requested activate separate keys; transitions out resolve them. Comment-count-only updates do nothing. Startup reconciliation scans current linked PR metadata.

Adapters call the attention service after the source transaction succeeds. An attention write failure is surfaced/logged according to the source boundary but does not roll back an already completed external poll or agent hook update; startup reconciliation repairs the projection.

**Why:** These points already possess old/new state or the semantic event needed to avoid expensive polling and false positives.

**Alternative considered:** Subscribe one generic projector to all bus events. Rejected as the sole mechanism because event payloads do not always carry enough old/new state, the bus may drop events, and some producer events occur in short-lived CLI processes.

### 4. Reconciliation is authoritative repair, not the primary update path

`AttentionService.Reconcile` scans persisted pending plans (including their stored mode), agent sessions with detectable blocking prompts, unresolved swarm question events and failure state, review tours/transcripts, and linked PR metadata. It computes active source keys, activates missing conditions, refreshes changed summaries, and resolves open items whose source is no longer active.

Run reconciliation:

- once during normal application/server startup after services are wired,
- after IPC reports a relevant change from a short-lived CLI process,
- and on explicit manual refresh.

Normal source transitions still update items immediately. Reconciliation is idempotent and repairs gaps caused by crashes or dropped event-bus messages.

**Why:** The existing event bus is deliberately non-durable. A durable projection cannot rely exclusively on it.

**Alternative considered:** Add a background polling loop. Rejected for v1 because existing producer hooks plus startup/manual reconciliation provide freshness without continuous cross-domain scans.

### 5. Dismissal and source resolution are distinct

`Dismiss(id)` sets `status=dismissed` and `resolved_at`, publishes invalidation, but never invokes a plan verdict, sends a worker message, changes a PR, or marks a review complete. Source reconciliation leaves a dismissed item hidden while the same activation remains active. It becomes eligible again only after the source is observed inactive and later activates anew, at which point `activated_at` changes and status returns to `open`.

A source action resolves the condition independently. For example, approving a plan deletes its pending state and resolves the item; merely opening the plan does not.

**Why:** Users need a way to remove noise without accidentally mutating domain workflows. Reappearing on every event refresh would make dismissal useless; suppressing forever would hide future regressions.

**Alternative considered:** “Mark read” per client. Rejected because the requested value is a shared work queue, and Legato has no user identity model on which to base personal read state.

### 6. Items carry typed targets; presentations own routing

`action_kind` is an enum such as:

- `open_agent_prompt`,
- `open_plan_approval`,
- `message_worker`,
- `open_swarm`,
- `open_review_tour`,
- `open_review_step`,
- `open_pull_request`,
- or `open_task`.

`target_json` contains a versioned target with stable IDs/URLs, not presentation routes. The service validates targets on creation. TUI and web map the same action kind to their existing navigation and action surfaces:

- the TUI changes root view, selection, or opens an existing overlay;
- the web uses router navigation, existing modals, agent selection, or external URL opening.

The attention HTTP API does not provide a generic “perform action” endpoint. Domain mutations continue through their existing endpoints/services. The inbox API is limited to list/get/count/dismiss.

**Why:** Navigation differs by presentation, while business mutations must retain existing validation and error behavior.

**Alternative considered:** A generic `POST /attention/{id}/execute`. Rejected because it would become a second dispatcher for all domain commands and obscure authorization/error contracts.

### 7. Add one TUI root view and one web route

**TUI:** Add `viewAttention` to the root app and a dedicated `internal/tui/attention` model. A global key (provisionally `!`, finalized against current bindings during implementation) opens it from non-input contexts. The status bar or global header shows `! <count>` when open items exist. The view supports:

- `j/k`, `g/G` navigation,
- category/severity/workspace filters,
- Enter for the primary action,
- a detail pane or expanded detail,
- `d` to dismiss with confirmation for action/error items,
- refresh,
- and Escape to return.

**Web:** Add `/attention` to the application router/navigation. Desktop uses a list/detail layout; mobile stacks details below the selected row or opens a detail sheet. The nav badge shows the global count. The page uses authenticated REST for authoritative reads and mutation, with WebSocket messages as invalidation.

Both presentations preserve existing source badges and specialized views.

**Why:** A root-level command center must be reachable regardless of which task or agent is selected. Embedding it only under Agents would omit PR/review work; embedding it only on the board would weaken mobile supervision.

**Alternative considered:** Overlay-only inboxes. Rejected because filters, details, and cross-domain navigation need more room and durable route/view state.

### 8. REST is authoritative; event bus, IPC, and WebSocket invalidate

Add authenticated endpoints:

- `GET /api/attention` with status/category/severity/workspace/task filters,
- `GET /api/attention/count`,
- `GET /api/attention/{id}`,
- `POST /api/attention/{id}/dismiss`.

Add `EventAttentionChanged` with payload `{ItemID, TaskID, SourceKind, Change}` where change is `activated`, `refreshed`, `resolved`, or `dismissed`. The server broadcasts `attention_changed`; web clients refetch count and affected queries. The TUI subscribes directly. Short-lived CLI processes broadcast an IPC `attention_changed` message after source mutations when necessary; the long-running process reconciles and publishes the canonical event.

The WebSocket event carries no full item body. SQLite/REST remains authoritative and reconnecting clients simply refetch.

**Why:** This follows existing board/review/swarm patterns and avoids stale replicated payloads.

**Alternative considered:** Push full attention items over WebSocket. Rejected because filters and dismissal races still require authoritative reads, and compact invalidation is easier to evolve.

### 9. Outbound notifications consume activations

Refactor notification orchestration so a transition from inactive to open calls an `AttentionDelivery` component that uses the existing `Notifier` instances. It applies:

- existing per-task ntfy preference,
- existing provider configuration,
- existing `CanNotify(taskID)` rate limiting,
- source eligibility: agent prompts, plan approvals, worker questions, failures, failed PR checks, and changes requested notify; review-ready and review-answer items remain inbox-only in v1,
- and the item’s `notified_at` activation state.

OS notifications remain configured globally. Ntfy remains per-task opt-in. Notification text is derived from the attention item. A failed delivery is best-effort, matching current notifier behavior, and never changes the item.

For compatibility, the existing direct `MaybeNotify` path is migrated rather than left in parallel. Ordinary idle transitions no longer produce “Agent ready” delivery unless prompt detection classifies them as actionable; this aligns outbound noise with the unified inbox.

**Why:** Creating the durable item before delivery ensures every push has an inspectable source and avoids two independent definitions of what deserves attention.

**Alternative considered:** Leave agent notifications direct and add attention delivery for new sources. Rejected because users would receive push notifications that have no corresponding inbox item and duplicate rate limiting would remain.

### 10. Implement as vertical TDD slices

Follow the repository’s TDD requirement using public behavior paths, not a horizontal batch of unit tests. The first tracer bullet is:

1. a service/API test activates one pending-plan condition,
2. `GET /api/attention` returns it,
3. dismissal removes it from the open query,
4. and source reactivation after a clear makes it visible again.

Subsequent vertical slices add one producer and one observable presentation behavior at a time: TUI plan item, web plan item, agent prompt, swarm question/failure, review, PR, then notifier delivery. Store tests support migration/query correctness but do not replace service/API/TUI/web behavior tests.

**Why:** The critical risk is disconnected wiring across source → persistence → presentation → action. Vertical tests prove the user-facing path exists.

## Risks / Trade-offs

- **[Risk] Source and projection writes are not one transaction across every service.** → Source state remains authoritative; failures are logged/surfaced where user-fixable, and idempotent startup/manual reconciliation repairs missing or stale items.
- **[Risk] Runtime prompt detection cannot always be reconstructed after restart.** → Re-detect against live waiting panes during reconciliation; do not synthesize an actionable prompt from waiting state alone.
- **[Risk] Worker questions have no explicit answer correlation today.** → Persist `resolved_at` on question source events and resolve all outstanding questions for that worker when the existing message-to-worker action succeeds. This gives reconciliation authoritative state; a future durable mailbox can introduce question IDs and one-to-one replies.
- **[Risk] Agent death may be intentional but indistinguishable after asynchronous reconciliation.** → Mark intentional close/kill/cancel paths before session termination and suppress failure activation for those paths; test each lifecycle entry point.
- **[Risk] PR polling can repeatedly observe the same failure.** → Deterministic source keys and `notified_at` ensure refreshes neither duplicate rows nor spam delivery.
- **[Risk] A global inbox can become another noisy feed.** → The initial allowlist excludes progress, ordinary idle, successful CI, and comment changes; additions require explicit source semantics and an action.
- **[Risk] Dismissal by one device affects every device.** → This is intentional shared-work-queue behavior. Personal read state remains out of scope until Legato has user identity.
- **[Risk] `target_json` becomes an untyped dumping ground.** → Use versioned Go target structs selected by `action_kind`, validate on activation, and expose typed TypeScript response unions.
- **[Trade-off] One row per source key retains limited history.** → Durable run/event history is a separate proposed capability; this change optimizes current triage and preserves activation timestamps only.
- **[Trade-off] Existing OS/ntfy “agent idle” behavior becomes narrower.** → This deliberately reduces noise by notifying only when a detected action is needed; document the behavior change.

## Migration Plan

1. Add the `attention_items` migration plus source-state migrations for pending-plan `mode` and swarm-question `resolved_at`, then add store CRUD/upsert/filter operations. Existing databases start with an empty attention projection; existing pending plans default to `normal`, and existing question events are backfilled as resolved to avoid surfacing historical ambiguity.
2. Implement `AttentionService`, deterministic source keys, lifecycle rules, event publication, and reconciliation.
3. Wire pending plans as the first end-to-end producer and add REST endpoints.
4. Add TUI and web inbox surfaces against the shared service/API, including count and dismissal.
5. Add source adapters incrementally for agent prompts, swarm questions/failures, reviews, and PR transitions.
6. Add event-bus, IPC, and WebSocket invalidation after authoritative reads work.
7. Migrate direct outbound notification triggering to attention activation and verify existing preferences/rate limits.
8. Run startup reconciliation to backfill currently actionable persisted plans, reviews, PRs, and live detectable prompts.
9. Update user and package/database/web documentation.

Rollback is safe at the application layer: older binaries ignore the additive table. If rollback occurs after deployment, source workflows remain unchanged because the inbox never owns their state. The migration table itself can remain without affecting older versions.

## Open Questions

- **TUI keybinding:** `!` communicates attention and appears free in current global help, but implementation must verify input/overlay conflicts before finalizing.
- **Review-answer resolution:** v1 leaves an answer item open until dismissal or review completion. If reviewers need a distinct “acknowledged answer” action, add it only after observing usage.
- **Agent failure clearing:** replacement-agent start, explicit task cancellation, and manual dismissal are clear outcomes; whether simply reopening the dead terminal context should resolve the item remains intentionally no.
- **Count semantics:** the main badge uses all open items, unaffected by the active view’s filter. A future preference could hide categories, but per-user preferences are out of scope.
