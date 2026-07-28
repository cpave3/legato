# Attention

A unified, actionable inbox for all Legato work that requires a human. The inbox is a generic platform capability — feature-owned producers publish attention items, the core service coordinates and routes actions, and web/TUI/future clients consume one semantic model.

## Architecture

### Durable registry

- `internal/engine/store/migrations/042_attention_items.sql` — `attention_items` table with unique `(producer, source_key)`, status (`open`/`dismissed`/`resolved`), revision-aware dismissal, and indexed active/producer/task queries.
- `internal/engine/store/attention.go` — store methods: `UpsertAttentionItem` (idempotent on `(producer, source_key)`, resurfaces dismissed items only when source revision changes), `GetAttentionItem`, `GetAttentionItemBySource`, `ListAttentionItems` (ordered by urgency then recency), `DismissAttentionItem` (revision-conditional), `ResolveAttentionItem`, `ResolveAttentionItemsMissing` (reconciliation).

### Generic service

`internal/service/attention.go` — `AttentionService` with:
- `Upsert` — validates the generic contract and persists an item.
- `List` — filters active (open) items by producer/category/urgency/task/workspace.
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
1. Implement `Snapshot` to enumerate current actionable state as `[]AttentionInput`.
2. Implement `Execute` to handle declared actions by calling the feature's existing public service operations.
3. Register both in `cmd/legato/attention_runtime.go` (or the TUI composition path).
4. Use `RegisterProducerWithEvents` to map source events to the producer so the service re-reconciles when source state changes after startup (see Event subscriptions below).
5. Reconciliation runs at startup; mutations should also upsert/resolve directly for immediate feedback.

### Event subscriptions

The service subscribes to source events and re-reconciles the matching producer when they fire, so the inbox stays current without a restart:

| Producer | Events | Source |
|---|---|---|
| `plans` | `EventPlanChanged` | IPC `plan_changed` → bus, or direct `PlanService` mutation |
| `reviews` | `EventReviewChanged` | IPC `review_changed` → bus, or direct `ReviewService` mutation |
| `workflows` | `EventWorkflowChanged`, `EventAgentDied`, `EventAgentCompleted` | IPC `workflow_changed` → bus, or `Server.NotifyWorkflowChanged`, or agent lifecycle |

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

## MVP producers

| Producer | Source | Inclusion rule | Actions |
|---|---|---|---|
| `plans` | `PlanService.Queue` | `proposed` or `changes_requested` | Open plan (navigate), Approve (execute, requires confirm) |
| `reviews` | `ReviewService.Queue` | ready/reviewed/capturing with unreviewed work | Review changes (navigate), Mark reviewed (execute, requires confirm) |
| `workflows` | `WorkflowService.ListRuns` + `Show` | `needs_attention` on run | Open run (navigate), Trigger/Retry/Prompt (execute) |

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
- Reconciles plan and review producers against the durable store so agents can inspect the inbox without a running server.

## Deferred

Agents, parent-aggregated swarms, PR feedback/CI, live permission prompts, and the durable orchestration mailbox are deferred per the approved plan until their underlying sources provide stable structured signals.
