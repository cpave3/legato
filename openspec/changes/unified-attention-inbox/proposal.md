## Why

Legato already detects many moments that require human action, but exposes them separately through agent state badges, plan modals, swarm event logs, PR indicators, review queues, OS notifications, and ntfy. Users must watch several surfaces to discover blocked or review-ready work, and dismissing a signal in one surface does not create shared state in the others.

A persistent, unified attention inbox will give TUI and web users one authoritative queue of actionable work while reusing the event bus, swarm inbox, review state, PR polling, and existing outbound notification providers.

## What Changes

- Add a persistent attention-item model and service that records actionable signals with source identity, task/swarm context, severity, summary, navigation target, action kind, timestamps, and shared open/resolved state.
- Materialize attention items from existing agent, plan, swarm, PR, and review flows rather than replacing their domain state or creating separate producer logic.
- Define the initial actionable set: agent approval/question waits, pending swarm plan approval, worker questions, unexpected solo-agent/worker/conductor failure, review tours ready for human review, agent answers awaiting reviewer follow-up, and failed CI or requested PR changes.
- Deduplicate repeated producer events into one open item per source condition and automatically resolve items when the underlying condition clears.
- Add presentation-neutral service operations to list, filter, count, inspect, and dismiss items, plus typed action targets that each presentation routes into existing source services or views.
- Add a keyboard-driven TUI attention view with an open count, filters, detail, dismissal, and routing into the existing agent, plan, task, swarm, PR, and review experiences.
- Add a responsive web inbox with the same shared items and state, direct actions, navigation, live WebSocket updates, and an app-wide attention badge.
- Route OS and ntfy delivery from newly activated approval/question, failure, and PR-regression items while retaining per-task notification preferences and rate limiting; review-ready and review-answer items remain inbox-only in v1, and delivery never defines inbox state.
- Keep existing specialized views and source records. The unified inbox links into them instead of duplicating plan verdict, worker messaging, PR, or review business logic.

## Capabilities

### New Capabilities
- `unified-attention-inbox`: Persistent attention aggregation, lifecycle and deduplication rules, shared TUI/web behavior, source-specific actions and navigation, real-time updates, and integration with optional OS/ntfy delivery.

### Modified Capabilities

None.

## Impact

- **Storage:** a new SQLite migration and store queries for persistent attention items and source-key uniqueness.
- **Service layer:** a new presentation-neutral attention service plus adapters from agent activity, swarm events/plans, PR status changes, and review events.
- **Events:** a new attention-changed event payload on the existing in-process event bus and corresponding IPC/WebSocket propagation.
- **TUI:** a new root attention view, badge/count in global chrome, keyboard actions, and navigation into existing views/overlays.
- **Web:** new authenticated attention REST endpoints, WebSocket invalidation, navigation entry and badge, and a responsive inbox page/panel.
- **Notifications:** existing `Notifier`, task notification preference, ntfy, and OS implementations become optional delivery channels for attention items rather than independent sources of truth.
- **Tests/docs:** service-level lifecycle tests, endpoint and presentation tests, TUI/web end-to-end paths, migrations, and updates to package/database/web/TUI documentation.
- No breaking configuration or external API changes are required; the change is additive.
