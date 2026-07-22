## ADDED Requirements

### Requirement: Persistent shared attention queue
The system SHALL persist attention items independently of any running TUI, web client, or outbound notification provider. An attention item SHALL include a stable ID, source kind, source key, task ID when applicable, parent task ID when applicable, severity, title, summary, primary action kind, navigation target, creation and update timestamps, and an `open`, `dismissed`, or `resolved` status.

#### Scenario: Item survives presentation restart
- **WHEN** an actionable condition creates an attention item and all TUI and web clients stop
- **THEN** reopening either presentation SHALL show the same open item

#### Scenario: Both presentations read the same queue
- **WHEN** a TUI and web client are connected to the same Legato database
- **THEN** both SHALL list the same open attention items and counts

### Requirement: Actionable source coverage
The system SHALL create attention items for the initial actionable source set: an agent waiting on an approval or explicit question, a pending swarm plan approval, a swarm worker question, an unexpected worker or conductor failure, a review tour ready for human review, an agent answer requiring reviewer follow-up, a linked PR whose checks transition to failed, and a linked PR whose review decision transitions to changes requested. Generic progress, successful checks, ordinary idle state without a detected prompt, comment-count changes, and informational swarm events SHALL NOT create attention items.

#### Scenario: Detected agent approval becomes actionable
- **WHEN** a running agent enters waiting state and its detected prompt requires approval or an answer
- **THEN** the system SHALL create an open item whose primary action opens that agent and its existing prompt controls

#### Scenario: Ordinary idle agent is ignored
- **WHEN** an agent stops working without a detected approval or question prompt
- **THEN** the system SHALL NOT create an attention item solely for the idle transition

#### Scenario: Pending plan becomes actionable
- **WHEN** a conductor proposes a normal or extension plan and awaits a verdict
- **THEN** the system SHALL create an open plan-approval item for the parent task

#### Scenario: Worker question becomes actionable
- **WHEN** a swarm worker reports a question
- **THEN** the system SHALL create an open item on the parent swarm that identifies the worker and opens the existing worker-message flow

#### Scenario: Unexpected agent death becomes actionable
- **WHEN** a solo agent, worker, or conductor dies before the corresponding work is intentionally completed or canceled
- **THEN** the system SHALL create an error-severity item that opens the affected agent, swarm, or task context

#### Scenario: Review-ready work becomes actionable
- **WHEN** a review tour transitions to ready
- **THEN** the system SHALL create an open item whose primary action opens that tour

#### Scenario: Agent answer becomes actionable for reviewer
- **WHEN** an agent answer is appended to a review question
- **THEN** the system SHALL create an open item keyed by that answer message whose primary action opens the answered review step

#### Scenario: Later answer on the same step resurfaces
- **WHEN** an earlier answer item was dismissed and the agent appends another answer to the same review step
- **THEN** the later answer SHALL create a separate open item and SHALL NOT remain suppressed by the earlier dismissal

#### Scenario: PR regression becomes actionable
- **WHEN** a linked PR transitions from a non-failed check state to failed checks or from a non-changes-requested review state to changes requested
- **THEN** the system SHALL create or refresh an open PR attention item for that condition

#### Scenario: Informational source update is ignored
- **WHEN** a worker reports progress, CI transitions to success, or a PR comment count changes without a failed check or changes-requested transition
- **THEN** the system SHALL NOT create a new attention item

### Requirement: Source-key deduplication and refresh
The system SHALL allow at most one open attention item for a source key. Repeated events for the same still-active condition SHALL refresh that item rather than append duplicates, while distinct workers, review tours, prompts, or PR conditions SHALL remain independently actionable.

#### Scenario: Repeated worker question refreshes one item
- **WHEN** the same worker reports its unresolved question more than once
- **THEN** the queue SHALL contain one open item for that worker question with its latest summary and update timestamp

#### Scenario: Separate workers remain separate
- **WHEN** two workers in one swarm each ask a question
- **THEN** the queue SHALL contain independently actionable items for both workers under the same parent task

#### Scenario: Resolved condition can recur
- **WHEN** a source condition was resolved and the same condition later becomes active again
- **THEN** the system SHALL reopen the source-key item or create a successor that appears as one open item with a new activation timestamp

### Requirement: Automatic source reconciliation
The system SHALL resolve an open attention item when its underlying source condition clears, regardless of which presentation or external process caused the change. Reconciliation SHALL run on relevant source events and at application startup so dropped in-process events or restarts do not leave permanently stale items.

#### Scenario: Agent prompt clears
- **WHEN** the user responds to an agent and the agent leaves the actionable waiting condition
- **THEN** the corresponding prompt item SHALL resolve automatically

#### Scenario: Plan receives a verdict
- **WHEN** a pending plan is approved or rejected from either TUI or web
- **THEN** the corresponding plan-approval item SHALL resolve automatically in both presentations

#### Scenario: PR condition recovers
- **WHEN** failed checks later pass or a changes-requested review state no longer applies
- **THEN** the corresponding PR condition item SHALL resolve automatically

#### Scenario: Review is completed
- **WHEN** a ready review tour is marked reviewed
- **THEN** its review-ready and review-answer items SHALL resolve automatically

#### Scenario: Review transcript is replaced or removed
- **WHEN** a review tour is restarted or deleted
- **THEN** its review-ready and review-answer items SHALL resolve immediately

#### Scenario: Startup repairs stale item
- **WHEN** Legato starts with an open item whose persisted source condition is no longer active
- **THEN** startup reconciliation SHALL resolve that item before presenting the open queue

#### Scenario: Startup recovers missed actionable state
- **WHEN** Legato starts and persisted source state is actionable but no open attention item exists
- **THEN** startup reconciliation SHALL materialize the missing item

### Requirement: Shared manual dismissal
An authenticated user SHALL be able to dismiss an open attention item without mutating the underlying domain object. Dismissal SHALL persist with `dismissed` status visible to all clients, distinct from source-driven `resolved` status. If the same source condition clears and later reactivates, it SHALL be eligible to appear again.

#### Scenario: TUI dismissal updates web
- **WHEN** a user dismisses an item in the TUI
- **THEN** the item SHALL leave the open queue and open count in the web client

#### Scenario: Web dismissal updates TUI
- **WHEN** a user dismisses an item in the web UI
- **THEN** the item SHALL leave the open queue and open count in the TUI

#### Scenario: Dismissal does not approve source action
- **WHEN** a user dismisses a pending plan item
- **THEN** the plan SHALL remain pending and the conductor SHALL NOT receive a verdict

#### Scenario: Reactivated condition returns after dismissal
- **WHEN** a dismissed condition later clears and becomes active again
- **THEN** the system SHALL surface it as open attention again

### Requirement: Filters and ordering
The attention service SHALL support filtering open items by category, severity, current workspace, and task context. Categories SHALL be `agent`, `swarm`, `review`, and `pr`: agent prompts and solo-agent failures map to `agent`; plans, worker questions, and swarm-participant failures map to `swarm`; review-ready and review-answer items map to `review`; failed checks and changes-requested items map to `pr`. Workspace filtering SHALL use the task's current workspace association rather than the workspace at item activation. Open items SHALL be ordered by severity and most recent activation or update, with error items before action-required items and informational review-ready items after them.

#### Scenario: Default queue prioritizes failures
- **WHEN** the queue contains an agent failure, a plan approval, and a review-ready item
- **THEN** the failure SHALL appear before the approval and the approval before the review-ready item

#### Scenario: Category filter narrows results
- **WHEN** a user filters the queue to review items
- **THEN** only review-ready and review-answer attention items SHALL be returned

#### Scenario: PR review decision remains a PR item
- **WHEN** a user filters the queue to PR items
- **THEN** failed-check and changes-requested attention items SHALL be returned

#### Scenario: Workspace filter follows board scope
- **WHEN** a user selects a workspace filter in an attention presentation
- **THEN** only attention items associated with tasks currently in that workspace SHALL be shown while the global open count remains available

#### Scenario: Moving a task updates filtering immediately
- **WHEN** a task with an open attention item moves to another workspace
- **THEN** subsequent filtered queries SHALL return the item under the new workspace without requiring item reactivation

### Requirement: Presentation-neutral actions and navigation
Each attention item SHALL declare a primary action kind and a typed navigation target. TUI and web SHALL execute domain mutations through the existing service or endpoint used by the specialized source UI, and SHALL otherwise navigate to that existing UI rather than duplicate source-specific business logic inside the attention service.

#### Scenario: Plan action reuses verdict flow
- **WHEN** a user activates a plan-approval item
- **THEN** the presentation SHALL open the existing plan approval experience and its existing verdict path

#### Scenario: Worker question reuses messaging flow
- **WHEN** a user activates a worker-question item
- **THEN** the presentation SHALL open the existing worker messaging action for that worker

#### Scenario: Agent prompt exposes response controls
- **WHEN** a user activates an agent-prompt item
- **THEN** the presentation SHALL focus the agent session and expose controls appropriate to the detected approval or question, using existing prompt controls where that presentation already has them

#### Scenario: PR item opens linked pull request
- **WHEN** a user activates a failed-check or changes-requested PR item
- **THEN** the presentation SHALL open the linked PR URL or the existing PR-aware task detail action

#### Scenario: Review item opens exact tour context
- **WHEN** a user activates a review-ready or answered-review item
- **THEN** the presentation SHALL open the target review tour and, when available, the target step

### Requirement: TUI attention experience
The TUI SHALL provide a global keybinding that opens a keyboard-driven attention view. The view SHALL show the global open count, support list/detail navigation and filters, allow dismissal with confirmation where appropriate, and route primary actions into existing TUI views or overlays. The board and agent surfaces SHALL display a compact open-attention badge without reducing existing card or agent state indicators.

#### Scenario: Open inbox from board
- **WHEN** the user presses the attention keybinding from the board
- **THEN** the TUI SHALL open the attention view with the highest-priority open item selected

#### Scenario: Keyboard-only triage
- **WHEN** the attention view is focused
- **THEN** the user SHALL be able to navigate items, switch filters, inspect details, invoke the primary action, dismiss an item, and return using documented keyboard controls

#### Scenario: Empty queue
- **WHEN** no open attention items exist
- **THEN** the TUI SHALL show an empty state and a zero count without hiding access to filters or help

### Requirement: Web attention experience
The web application SHALL provide an authenticated, responsive attention route or panel with the same persistent queue, ordering, filters, details, dismissal behavior, and source actions as the TUI. The global web navigation SHALL display an open-attention badge, and the experience SHALL remain usable on installed mobile PWA layouts.

#### Scenario: Badge opens web inbox
- **WHEN** a user selects the attention badge in the web navigation
- **THEN** the application SHALL open the attention experience and display the shared open queue

#### Scenario: Mobile action remains usable
- **WHEN** the inbox is viewed at a mobile viewport width
- **THEN** item details and the primary action SHALL remain visible without requiring horizontal scrolling

#### Scenario: Auth protects attention APIs
- **WHEN** an unauthenticated client requests attention data or attempts dismissal
- **THEN** the server SHALL reject the request using the existing web authentication behavior

### Requirement: Real-time cross-surface invalidation
Creating, refreshing, resolving, or dismissing an attention item SHALL publish an attention-changed event on the existing event bus. The server SHALL broadcast a corresponding WebSocket invalidation, and running TUI/web clients SHALL refresh the affected item or queue without polling as the primary update mechanism.

#### Scenario: New item appears live
- **WHEN** an actionable condition creates an item while TUI and web are open
- **THEN** both presentations SHALL update their count and queue without restart or manual refresh

#### Scenario: Resolution disappears live
- **WHEN** an item resolves from a source action in one client
- **THEN** other connected clients SHALL remove it from their open queue after receiving the change event

#### Scenario: Reconnect restores authoritative state
- **WHEN** a web client misses attention events while disconnected and reconnects
- **THEN** it SHALL refetch the persistent queue and display current authoritative state

### Requirement: Outbound notification reuse
Existing OS and ntfy notifiers SHALL remain optional delivery channels. In v1, newly activated `agent_prompt`, `plan_approval`, `swarm_question`, `agent_failure`, `pr_checks_failed`, and `pr_changes_requested` items SHALL be eligible for outbound delivery; `review_ready` and `review_answer` items SHALL remain inbox-only. Eligible delivery SHALL honor existing provider configuration, per-task notification preference, and rate limiting. Outbound delivery success or failure SHALL NOT control item persistence, and refreshing an unchanged open item SHALL NOT repeatedly notify the user.

#### Scenario: Configured notifier delivers new item
- **WHEN** a new eligible item is activated for a task with notifications enabled
- **THEN** configured outbound providers SHALL receive a concise title and summary using the existing notifier implementations

#### Scenario: Review updates remain inbox-only
- **WHEN** a review-ready or review-answer item is activated
- **THEN** the item SHALL appear in TUI and web without invoking OS or ntfy delivery

#### Scenario: Inbox works without notifier
- **WHEN** no OS or ntfy provider is configured
- **THEN** attention items SHALL still be persisted and shown in TUI and web

#### Scenario: Delivery failure preserves item
- **WHEN** an outbound notification provider fails
- **THEN** the attention item SHALL remain open and available in both presentations

#### Scenario: Duplicate refresh is rate-limited
- **WHEN** repeated events refresh an unchanged open item
- **THEN** the system SHALL NOT send a new outbound notification for every refresh

### Requirement: Specialized source views remain authoritative
The unified inbox SHALL NOT delete or replace swarm events, pending plans, review transcripts, PR metadata, or agent session state. Source-specific history and controls SHALL remain available in their existing views, and acknowledging or draining a swarm event SHALL be independent from resolving an attention item.

#### Scenario: Draining swarm log does not dismiss question
- **WHEN** a user drains the swarm event log containing a worker question
- **THEN** the corresponding attention item SHALL remain open until dismissed, answered, or reconciled as cleared

#### Scenario: Dismissing attention preserves source history
- **WHEN** a user dismisses a review, PR, or swarm attention item
- **THEN** the underlying review transcript, PR metadata, or swarm event SHALL remain intact
