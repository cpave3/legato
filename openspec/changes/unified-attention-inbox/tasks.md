## 1. Persistent attention model

- [ ] 1.1 Add the next SQLite migration for `attention_items`, including the source-key unique index, category/status/severity checks, task/workspace lookup indexes, lifecycle timestamps, target JSON, and notifier activation state; also persist pending-plan `mode` and swarm-question `resolved_at`, defaulting existing plans to `normal` and existing ambiguous historical questions to resolved. Verify a migrated existing database and a fresh database both open successfully.
- [ ] 1.2 Add engine store types plus transactional activate/refresh, resolve, dismiss, get, count, and filtered-list operations; verify through store tests that one source key yields one open row, reactivation resets activation state, category mappings/filtering are stable, and priority order matches the specification.
- [ ] 1.3 Add typed, versioned attention target structs and JSON encoding/decoding at the service boundary; verify malformed or mismatched targets return an error rather than creating unroutable items.

## 2. Service tracer bullet: pending plan to API

- [ ] 2.1 RED: add an integration-style service/server test proving that a proposed pending plan appears through `GET /api/attention`, dismissal removes it from the open query without verdicting the plan, and a clear-then-reactivate cycle returns it; confirm the test fails before implementation.
- [ ] 2.2 GREEN: implement the minimal `AttentionService`, source-key lifecycle, pending-plan adapter, authenticated list endpoint, and dismiss endpoint required to pass the tracer-bullet test.
- [ ] 2.3 Refactor the tracer-bullet implementation into presentation-neutral attention and producer interfaces while keeping the test green and preserving engine → service → presentation imports.

## 3. Query API and change propagation

- [ ] 3.1 Add `Get`, `CountOpen`, and filters for status, category, severity, workspace, task, and parent task to `AttentionService`; verify public service tests cover default priority ordering, stable category mappings, and query-time current-workspace filtering, including a task moved after item activation.
- [ ] 3.2 Add the authenticated `GET /api/attention` happy path from one failing `httptest`, then add invalid filters and unauthenticated/method rejection one failing behavior at a time.
- [ ] 3.3 Add `GET /api/attention/count`, `GET /api/attention/{id}`, and `POST /api/attention/{id}/dismiss` as separate RED/GREEN endpoint slices, including unknown-ID and unauthenticated behavior for each before moving on.
- [ ] 3.4 Add `EventAttentionChanged` and its typed payload to the existing event bus, publishing only after successful activation/refresh/resolution/dismissal; verify subscribers receive item ID, task/source context, and change kind.
- [ ] 3.5 Extend IPC and server WebSocket wiring with compact `attention_changed` invalidation messages; verify a real event bus reaches a connected web client and no full/stale item payload is broadcast.
- [ ] 3.6 Wire relevant short-lived CLI IPC mutations to trigger canonical reconciliation in long-running TUI/server processes; verify a source mutation performed through the CLI becomes visible without restarting the presentation.

## 4. Reconciliation and source lifecycle

- [ ] 4.1 RED: add a startup-reconciliation test proving a persisted normal pending plan with no item produces the correct normal-plan attention target; confirm it fails before implementation.
- [ ] 4.2 GREEN: implement the minimum pending-plan reconciliation to pass the normal-plan test, then add an extension-mode example and stale-item example one at a time, minimally extending the implementation for each.
- [ ] 4.3 Implement idempotent cross-source reconciliation orchestration in `AttentionService`, returning user-fixable errors instead of swallowing them; verify running it twice produces no duplicates or extra activation notifications.
- [ ] 4.4 Invoke reconciliation after service wiring at TUI/server startup and on manual refresh; verify open counts are correct before the first inbox render.
- [ ] 4.5 Add source-clear tracking so a dismissed condition remains hidden during same-condition refresh but becomes open after an inactive-to-active transition; verify the full dismiss, refresh, clear, and reactivate lifecycle through the public service API.

## 5. TUI plan-attention vertical slice

- [ ] 5.1 RED: add a root TUI behavior test that opens the attention view from the board, shows a pending-plan item/count, dismisses it, and returns to the board; confirm the test uses the root `App.Update`/`View` path and fails before implementation.
- [ ] 5.2 GREEN: add `viewAttention`, an `internal/tui/attention` model, attention service wiring, a conflict-free global keybinding, count badge, list/detail rendering, empty state, and dismissal sufficient to pass the root test.
- [ ] 5.3 Add TUI category, severity, and workspace filters plus `j/k`, `g/G`, refresh, detail, dismissal confirmation, help, and Escape behavior one behavior at a time with a failing root `App.Update`/`View` test before each minimum implementation; also verify narrow-terminal rendering through the root view.
- [ ] 5.4 Route a plan item's primary action into the existing plan-approval overlay and verdict flow; verify approving from the attention view resolves the item and allows the conductor to proceed.
- [ ] 5.5 Subscribe the root TUI to `EventAttentionChanged` and refresh count/selection without losing a valid cursor; verify activation and resolution from another client update the open TUI live.

## 6. Web plan-attention vertical slice

- [ ] 6.1 RED: add a web integration/component test that loads `/attention`, renders the pending-plan item and global badge from the authenticated API, opens the existing plan modal, and observes dismissal from shared state; confirm it fails before implementation.
- [ ] 6.2 GREEN: add typed attention API wrappers/hooks, `/attention` routing, global navigation badge, responsive list/detail UI, empty/loading/error states, and dismissal sufficient to pass the plan-attention test.
- [ ] 6.3 Route the plan primary action into the existing `PlanApprovalModal` for normal and extension plans; verify approve/reject resolves the item while closing or manually dismissing does not send a verdict.
- [ ] 6.4 Subscribe web attention hooks to `attention_changed`, refetch authoritative count/query data, and refetch after WebSocket reconnect; verify cross-client activation and resolution update without manual refresh.
- [ ] 6.5 Verify the web inbox at mobile PWA width has no horizontal scrolling and keeps summary, detail, primary action, filters, and dismissal usable.

## 7. Actionable agent prompts

- [ ] 7.1 RED: add a test through the real agent activity entry point proving one detected tool-approval wait activates one `agent_prompt` item; confirm it fails before implementation.
- [ ] 7.2 GREEN: capture/detect the waiting pane outside the web-only stream path and activate the minimum tool-approval item; then add plan-approval, explicit-question, and ordinary-idle examples one at a time, minimally extending classification for each.
- [ ] 7.3 RED→GREEN: add source-action tests one outcome at a time for successful prompt submission, resumed work, intentional session end, and failed submission; implement each resolution rule minimally and preserve user-correctable submission errors.
- [ ] 7.4 Add live-pane prompt re-detection to reconciliation for running waiting agents, first testing restoration of one detectable prompt and then absence for ordinary idle; never infer actionability from waiting state alone.
- [ ] 7.5 Add TUI prompt state and approval/question controls, since these currently exist only in web, and route the item through the root `App.Update`/`View` path to focus the terminal, respond, and observe shared resolution.
- [ ] 7.6 Route the web action to its existing detected-prompt controls and verify through the `/attention` route that approval/question responses resolve the shared item.

## 8. Swarm questions and failures

- [ ] 8.1 RED: add a `SwarmService.Question` test proving one persisted unresolved question activates one parent-grouped item; confirm it fails before implementation.
- [ ] 8.2 GREEN: implement the minimum question activation, then add repeated-question and two-worker examples one at a time to establish refresh versus separate-item behavior.
- [ ] 8.3 RED→GREEN: test and implement successful message-to-worker resolution, including durable `resolved_at`, before separately adding worker/swarm close, cancel, and finish clearing rules; add a drain/ack example proving source-log acknowledgment alone leaves attention open.
- [ ] 8.4 Route worker-question actions through root TUI `App.Update`/`View` and the web `/attention` route into the existing worker message flow with the originating worker selected; verify a successful reply resolves the shared item.
- [ ] 8.5 RED: add one public agent-service test proving an unexpected solo-agent death creates error attention; implement the minimum classification, then add worker and conductor death examples individually.
- [ ] 8.6 Add intentional kill, close, finish, and cancel examples one at a time, minimally extending suppression/clearing for each lifecycle entry point.
- [ ] 8.7 Route failure items through root TUI and web attention paths to the affected agent, swarm, or parent task, then test replacement/cancel/terminal clearing individually; merely opening the item SHALL remain non-resolving.

## 9. Review attention

- [ ] 9.1 RED: add a test through the real review-ready command/service path proving one ready tour activates one `review_ready` item; implement the minimum adapter.
- [ ] 9.2 Add reviewed, deleted, restarted, and startup-reconciliation examples one at a time, minimally implementing each lifecycle rule; reviewed/deleted/restarted SHALL immediately resolve both review-ready and all answer items for the tour.
- [ ] 9.3 RED: add a test through the real review-answer path proving one appended agent answer activates `review_answer:<message-id>` with tour/step routing while an unanswered reviewer question does not; implement the minimum adapter, then prove a later answer on the same step creates a separately dismissible item.
- [ ] 9.4 Route review-ready and review-answer items to the exact tour/step through root TUI and web attention paths; test dismissal transcript preservation and review-completion resolution separately.

## 10. Pull-request attention

- [ ] 10.1 RED→GREEN: in the real PR polling/apply-status path, activate separate items only on transition into failed checks and changes requested; verify success, pending, review-required, and comment-count-only changes create no item.
- [ ] 10.2 Resolve each PR condition independently when checks recover or review state changes, and reconcile current linked PR metadata on startup; verify repeated polls refresh without duplicate rows.
- [ ] 10.3 Route PR attention from TUI and web to the linked pull request or existing PR-aware task detail fallback; verify missing/stale URLs surface an actionable error instead of silently dismissing the item.

## 11. Outbound notification integration

- [ ] 11.1 RED: add public service-path tests one source at a time proving agent prompts, plan approvals, worker questions, failures, failed PR checks, and changes requested are delivery-eligible while review-ready/review-answer remain inbox-only; then test per-task ntfy preference, `CanNotify`, and delivery failure as separate behaviors.
- [ ] 11.2 GREEN: add attention activation delivery using the existing `Notifier` implementations and persist `notified_at` per activation so refreshes do not resend.
- [ ] 11.3 Migrate the existing direct `MaybeNotify` agent-ready path so actionable attention activation is the single notification trigger; verify ordinary idle no longer pushes and blocking prompt behavior still does.
- [ ] 11.4 Verify installations with no notifier configuration retain full TUI/web inbox behavior and that configured notification text identifies the source task and primary reason for attention.

## 12. End-to-end parity and quality

- [ ] 12.1 Add an end-to-end TUI test covering mixed priority ordering, filtering, source navigation, shared dismissal, and live resolution across at least plan, failure, review, and PR items.
- [ ] 12.2 Add an end-to-end web test covering the same mixed queue, authenticated API, responsive rendering, source navigation, shared dismissal, WebSocket invalidation, and reconnect recovery.
- [ ] 12.3 Run a manual two-client smoke test: keep TUI and web open, trigger every initial source category, act/dismiss from alternating clients, and verify counts/source histories remain consistent.
- [ ] 12.4 Run migration/store tests, targeted race tests, the full Go quality gate with `task check`, `task web:test`, `task web:build`, and `cd web && pnpm run lint`; resolve all failures without weakening user-visible errors.

## 13. Documentation

- [ ] 13.1 Update `README.md` and TUI help/keyboard documentation with the attention view, badge, filters, shared dismissal semantics, and finalized keybinding.
- [ ] 13.2 Update `docs/claude/packages.md` and `docs/claude/database.md` with the attention service/store ownership, table schema, source projection rules, and reconciliation behavior.
- [ ] 13.3 Update `docs/claude/web-ui.md` with the attention route, authenticated endpoints, WebSocket invalidation, mobile behavior, and source-action routing.
- [ ] 13.4 Update `docs/claude/swarm.md`, `docs/claude/pr-tracking.md`, and review documentation to describe which source transitions create/resolve attention and clarify that source logs/transcripts remain authoritative.
- [ ] 13.5 Update notification configuration documentation to explain that OS/ntfy are optional delivery channels for newly activated attention items and that ordinary idle transitions no longer generate pushes.
