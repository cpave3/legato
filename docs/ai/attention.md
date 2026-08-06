# Attention

A high-signal interrupt queue for Legato work that is blocked on a human decision or requires failure remediation. Feature queues remain authoritative for routine backlog; the core service centrally controls which semantic signal kinds reach Attention, and web/TUI/CLI/Stream Deck consume that same policy.

## Architecture

### Durable registry

- `internal/engine/store/migrations/042_attention_items.sql` and `059_attention_signal_kind.sql` — `attention_items` table with unique `(producer, source_key)`, a persisted `signal_kind`, status (`open`/`dismissed`/`resolved`), revision-aware dismissal, and indexed active/producer/task/signal queries.
- `internal/engine/store/attention.go` — store methods: `UpsertAttentionItem` (idempotent on `(producer, source_key)`, resurfaces dismissed items only when source revision changes), `GetAttentionItem`, `GetAttentionItemBySource`, `ListAttentionItems` (ordered by urgency then recency), `DismissAttentionItem` (revision-conditional), `ResolveAttentionItem`, `ResolveAttentionItemsMissing` (reconciliation).

### Generic service

`internal/service/attention.go` — `AttentionService` with:
- `Upsert` — validates the generic contract and persists an item.
- `List` — returns active items whose semantic signal kind is in the central high-signal allowlist, then filters by producer/category/urgency/task/workspace.
- `Execute` — validates the item is open at the expected source revision, delegates to the registered producer's action handler, then re-reconciles that producer to remove/resolve the item.
- `Dismiss` — suppresses the current revision only; a new source revision can resurface the item.
- `Resolve` — marks an item resolved (used by reconciliation and direct producer calls).
- `Reconcile` / `ReconcileAll` — asks each producer for its current actionable snapshot, upserts current items, and resolves stale ones. Failed snapshots do not erase existing items.

### Producer contract

A producer implements `AttentionProducer` (snapshot) and `AttentionActionHandler` (action execution):

```go
type AttentionProducer interface {
    Producer() string
    Snapshot(context.Context) ([]AttentionInput, error)
}

type AttentionActionHandler interface {
    Execute(context.Context, AttentionActionRequest) error
}
```

The service validates identity and revision, invokes the handler for the item's producer, then re-reconciles. The core never switches on feature types.

To register a new producer:
1. Implement `Snapshot` to enumerate current interrupt-worthy state as `[]AttentionInput` and assign a stable, centrally registered `SignalKind`. Unknown kinds fail closed.
2. Implement `Execute` to handle declared actions by calling the feature's existing public service operations.
3. Register both in `cmd/legato/attention_runtime.go` (or the TUI composition path).
4. Use `RegisterProducerWithEvents` to map source events to the producer so the service re-reconciles when source state changes after startup (see Event subscriptions below).
5. Reconciliation runs at startup; mutations should also upsert/resolve directly for immediate feedback.

### Event subscriptions

The service subscribes to source events and re-reconciles the matching producer when they fire, so the inbox stays current without a restart:

| Producer | Events | Source |
|---|---|---|
| `plans` | `EventPlanChanged` | IPC `plan_changed` → bus, or direct `PlanService` mutation |
| `workflows` | `EventWorkflowChanged`, `EventAgentDied`, `EventAgentCompleted` | IPC `workflow_changed` → bus, or `Server.NotifyWorkflowChanged`, or agent lifecycle |
| `workflow_triggers` | `EventWorkflowTriggerChanged` | Trigger request state changes |

`RegisterProducerWithEvents(producer, eventTypes...)` maps events to producers. `StartEventReconciliation(ctx)` subscribes and launches goroutines. Call it once after all producers are registered.

Workflows need `EventAgentDied` and `EventAgentCompleted` because the workflow producer checks agent session state (waiting/idle/ended) to determine if a step needs attention.

### Action and navigation descriptors

Actions carry a stable `ID`, `Label`, `Kind` (`navigate` or `execute`), `Intent`, and flags for `RequiresText` / `RequiresConfirm`. Navigation carries a `Kind` (e.g., `plan`, `review`, `workflow_run`, `task`) and `Target`. The core treats these as opaque data; clients interpret them at the boundary.

### Events

`events.EventAttentionChanged` with `AttentionChangedPayload` is published after every upsert, resolve, dismiss, and action. The web server forwards it as `attention_changed` over WebSocket. Clients refetch the queue on receipt.

## HTTP API

- `GET /api/attention` — active items with optional `?producer=&category=&urgency=&task_id=` filters. Returns `{ items: [...], total: N }`.
- `POST /api/attention/{id}/actions/{action_id}` — execute a declared action. Body: `{ expected_revision, confirmed?, text? }`. Returns 204 or 409 (conflict) on stale revision.
- `POST /api/attention/{id}/dismiss` — dismiss at current revision. Body: `{ expected_revision }`. Returns 204 or 409.

All endpoints require the configured auth token.

## Default signal policy

| Producer | Included in Attention | Kept in feature surface only |
|---|---|---|
| `plans` | Proposed revisions awaiting a current user decision | `changes_requested` revisions being reworked |
| `workflows` | Manual gates, agents genuinely waiting for input, failed steps | Successful/completed runs and workflow history |
| `workflow_triggers` | Missing input, confirmation, and failed automatic triggers | Completed/skipped trigger history |
| `reviews` | Nothing | All ready review tours remain in Reviews |
| `learnings` | Nothing | Candidates, proposals, activation, verification, and stale records remain in Learning |

Failures use high urgency; blocking decisions use normal urgency. The unfiltered API and `AttentionService.Count` apply the same allowlist, so the navigation badge, web, TUI, CLI, and Stream Deck agree. Producer/category filters refine this default set; they cannot opt into excluded backlog.

## Web

- `/attention` route with `AttentionPage` — urgency-sorted list, execute/dismiss/open per item, live refresh via `attention_changed` WebSocket message.
- `Layout.tsx` — unified attention badge in sidebar and mobile nav showing total count.
- `useAttention` hook — fetches and subscribes.

## TUI

- `I` key from the board opens the attention queue (`internal/tui/attention/model.go`).
- `j`/`k` navigation, `Enter` executes the primary action, `D` dismisses, `q` returns to board.
- Refreshes on `EventAttentionChanged`.

## CLI

- `legato attention list` — reconcile and list actionable items. Flags: `--producer`, `--category`, `--urgency`, `--json`.
- Reconciles plan decisions against the durable store; workflow and trigger projections are maintained by the normal runtime and remain queryable from the durable registry.

## Deferred

Agents, parent-aggregated swarms, PR feedback/CI, live permission prompts, and the durable orchestration mailbox are deferred per the approved plan until their underlying sources provide stable structured signals.

## Dedicated feature queues

Review and Learning records are deliberately not projected into Attention. Their own queue/ledger remains the place to discover and act on that work. This preserves source state and prevents Attention from becoming a second backlog.

Workflow definitions no longer support `notify_on_complete`; successful runs remain visible in workflow history but do not create dismiss-only Attention items.
