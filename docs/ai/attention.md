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
| `pull_requests` | Open non-draft tracked PRs with changes requested, failed CI, or both | `EventPRStatusUpdated` |
| `agents` | Running standalone interactive agents explicitly reporting waiting or error | `EventAgentActivityChanged`, start/death/completion lifecycle |
| `workflows` | Manual gates, workflow agents waiting for input, failed steps | Workflow and agent lifecycle events |
| `workflow_triggers` | Missing input, confirmation, and failed automatic triggers | `EventWorkflowTriggerChanged` |

Startup reconciliation remains the safety net. Source events reconcile the relevant projections without requiring an Attention request to query GitHub or mutate source state.

`agent_state` IPC still publishes `EventCardUpdated` and web agent invalidation, and now also publishes `EventAgentActivityChanged`. `EventPRStatusUpdated` retains its existing `cards_changed` behavior while independently reconciling PR attention.

### Revision and dismissal semantics

Each producer builds a revision from meaningful source state. Dismiss suppresses only that exact revision. A changed PR blocker, agent activity, review handoff, plan, or workflow state can resurface; an unchanged snapshot cannot. When a successful snapshot omits a previous source key, reconciliation resolves the stale item.

## Default signal policy

Attention deliberately includes only states the local durable data can support reliably:

- **Needs intervention:** plan decisions, workflow gates/failures/input, and standalone agent waiting/error.
- **Ready for review:** tours explicitly marked ready through `legato review ready` and still containing visible unreviewed work.
- **Merge blockers:** open, non-draft tracked PRs whose persisted review decision is `CHANGES_REQUESTED`, whose persisted check status is `fail`, or both.

The following do not raise Attention:

- pending review or pending CI, because these may be passive waiting rather than user action;
- total PR comment count, because it does not identify unresolved actionable feedback;
- draft, closed, or merged PRs;
- inferred GitHub mergeability, branch protection, conflicts, or merge-queue state, which are not persisted;
- idle activity or process termination as an agent completion claim;
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

Unknown future allowed signals fall back to Needs intervention rather than disappearing. Cards retain source, urgency, reason/detail, relative time, executable source actions, revision-scoped Dismiss, and direct navigation. Dismiss is visually secondary to the source-owned action or Open. A larger global all-clear appears only when all sections are empty. Controls wrap below content on narrow screens.

## Other clients

- **TUI:** `I` opens the shared generic list; `j`/`k`, Enter, `D`, and `q` retain their existing behavior.
- **CLI:** `legato attention list` reconciles CLI-available sources and lists the durable default-policy set with producer/category/urgency/JSON options.
- **Stream Deck and navigation badges:** consume the same expanded count/list policy.

These clients intentionally remain generic lists in this delivery; only the web surface adopts grouped dashboard presentation.
