## Context

Today the web UI is a read-mostly observer of legato state. The agent split-view shows running agents (with the conductor/worker grouping landed in `swarm-conductor`); the terminal panel streams an agent's output via `pipe-pane`; CRUD-y actions are limited to spawning ephemeral agents and killing them.

Swarms add a richer control surface that the web doesn't yet cover. The conductor's *blocking* `propose-plan` flow is the most painful gap: a conductor that submits a plan halts on `ipc.BroadcastRequest` waiting for a `plan_verdict`. If the user is on their phone running the PWA, they see a `[legato] new swarm event` notification land in the conductor's pane (via the streaming terminal panel) but have no way to verdict the plan from the web. They have to walk to the host, attach to the TUI, and use the overlay there.

The CLI surface for swarms is well-defined and stable (`legato swarm propose-plan|dispatch|message|broadcast|close|finish|status|inbox`). All those verbs route through `service.SwarmService`. The server already imports `SwarmService` (it's wired in `cmd/legato/main.go::runTUI` and passed into the web server's NewApp). We just don't expose it via HTTP/WebSocket yet.

The web frontend uses React 19 + Vite + TailwindCSS + TanStack-style state in custom hooks. WebSocket message routing lives in `web/src/hooks/useWebSocket.ts` with a `WSMessage` discriminated union; new types extend that.

Auth is bearer-token (`Authorization: Bearer <token>`) on REST and `?token=` on WebSocket; existing middleware covers any new endpoints we register on the same mux.

## Goals / Non-Goals

**Goals:**

- A user on the web PWA can start a swarm on a parent card, supply a working directory, and have the conductor spawn — without touching the TUI.
- When the conductor submits a plan, the web user receives a modal with the plan summary and sub-task list, and can approve, reject-with-notes, or dismiss.
- The web user can message a specific worker, close a worker, and finish the swarm from the agents view.
- Recent swarm events (the inbox) are visible from the web without dropping into a terminal pane.
- All web actions go through the same `SwarmService` methods the CLI uses — no parallel logic.

**Non-Goals:**

- **Edit-in-`$EDITOR` from the web.** The TUI plan-approval overlay supports `e` to edit the plan YAML in `$EDITOR` before approving. The web has no editor surface; supporting it would require either an in-browser YAML editor (heavy) or download/edit/upload flow (clunky). v1 ships without web edit; users who need to edit must reject with notes and let the conductor revise, or fall back to the TUI for that one flow.
- **Decomposition overlay equivalent.** The TUI doesn't have one anymore (the conductor drives decomposition); web doesn't need one either.
- **Swarm timeline / replay.** Showing the historical event sequence as a timeline is interesting but out of scope here; we can render the unacked inbox plus a flat list of recent events per parent. Anything more sophisticated belongs to the reporting screen integration (`docs/swarm-integration-followup.md` item #2).
- **Multi-conductor swarms / per-worker scope edits / mid-flight role swaps.** Not features in legato; not adding them.
- **Mobile-only optimizations.** The PWA already responds to small viewports for the existing screens; we follow the same patterns. We don't redesign for mobile-first specifically.

## Decisions

### 1. New verbs go through HTTP, not WebSocket

Each swarm verb gets a dedicated HTTP endpoint (`POST /api/swarm/<verb>` or `GET` for read-only). WebSocket carries *events* (plan_proposed, swarm_changed) — a one-way broadcast channel from server to web — and the *plan_verdict* reply (the only client→server WebSocket message in this change).

**Rationale**: HTTP semantics fit verb-with-result (`POST /api/swarm/dispatch` returning success/error). WebSocket would need ad-hoc request/reply correlation. We already use HTTP for `POST /api/agents/spawn` and `POST /api/agents/kill`, so the pattern is established. Plan verdict goes via WebSocket because it's a *reply to a server-initiated event* — same socket that delivered the proposal carries the answer back.

**Alternative considered**: all-WebSocket. Rejected because the request/reply correlation cost outweighs the benefit.

### 2. Plan verdict travels back over the WebSocket that delivered the proposal

When `MsgPlanProposed` arrives at a web client carrying the conductor's reply socket path, the web client replies with `MsgPlanVerdict {parent_task_id, status, notes, plan_path}`. The server's WebSocket handler receives it and sends it via `ipc.Send` to the conductor's reply socket — same path the TUI overlay uses.

**Rationale**: The conductor's `propose-plan` CLI is blocked on its temporary listener socket. Whoever responds first wins. By keeping the verdict path on the WebSocket the proposal arrived on, we naturally serialize per-client. Multi-client races (two phones approving the same plan) are resolved at the conductor: the second `ipc.Send` arrives at a closed listener and is silently dropped.

**Alternative considered**: dedicated `POST /api/swarm/plan-verdict`. Rejected because the proposal arrives over WebSocket (server-initiated push); having the reply go via HTTP requires the client to remember the reply socket path across the round-trip. Easier to keep both halves on the same connection.

### 3. Plan editing uses a "reject with notes" pattern, not in-browser editing

When the user wants to change the plan, web rejects it with notes (free-form text). The conductor receives the rejection, revises, re-submits. No in-browser YAML editor.

**Rationale**: in-browser YAML editing is a real feature with real costs (an editor component, schema validation, conflict resolution if the plan changes server-side). The conductor *can* revise based on natural-language notes; that's the path the rejection flow already supports. Users who want to surgically edit a plan can:

1. Reject with notes describing the edit.
2. Or fall back to the TUI overlay (`y/e/n`) for that one swarm.
3. Future change can add a download-edit-upload flow if it's needed.

**Alternative considered**: integrate a CodeMirror or Monaco editor for YAML. Rejected for v1 — too heavy a dep for a feature that'll be used rarely.

### 4. Action menu placement: per-row overflow in the existing AgentSidebar

Each agent entry gets an overflow `⋯` button. Clicking it opens a small menu with the actions appropriate for that role:

- **Worker** entry: Send message · Close worker
- **Conductor** entry: Send message · Finish swarm

This avoids cluttering the row with always-visible buttons and keeps the sidebar dense.

**Rationale**: matches the existing PWA pattern (the `PromptBar` already has an overflow menu). Keeps the sidebar at its current width.

**Alternative considered**: dedicated action bar above the terminal panel. Rejected — context-switches the user away from the agent they're viewing; per-row actions stay tied to the row.

### 5. Swarm event log: collapsible panel, manual drain

A new "Swarm events" section appears in the agent split-view below the terminal panel when the focused agent is part of a swarm. It shows the parent's *unacked* events (the inbox) with a "Drain" button that calls `GET /api/swarm/inbox/<parent-id>` (which marks them acked server-side). Events are rendered as one-line entries: `#42 [progress] worker "API" — text…` with full payload on click.

**Rationale**: gives the web user the same audit trail the conductor would get from `legato swarm inbox`. The drain action is explicit (not auto on view) so the user can decide when to mark events read — useful when multiple users are on the same instance and only one is the "active" reader.

**Alternative considered**: auto-drain on view. Rejected because it's destructive (acked events are no longer surfaced to the conductor as new); user should opt in.

### 6. WebSocket subscription is per-client; server fans out events

Both `EventPlanProposed` and `EventSwarmChanged` are published on the existing in-process event bus. The WebSocket hub already broadcasts `MsgAgentsChanged` to all connected clients on agent state changes. We extend that: server subscribes to the new events at startup and pushes them to all WebSocket clients via the existing `hub.Broadcast`.

**Rationale**: the TUI uses the same event bus and gets the same notifications. The web is a peer subscriber, not a special case. Plan proposals fan out to every connected client; whichever responds first wins (see Decision 2).

**Risk**: spurious re-renders. Each `MsgSwarmChanged` triggers a refetch of the affected swarm's status. We can debounce client-side if it becomes a problem.

### 7. Authorization parity with existing endpoints

All new HTTP endpoints sit behind the existing bearer-token middleware. WebSocket messages (`MsgPlanVerdict`) are accepted only on already-authenticated connections (the connection's auth check is at handshake time).

No per-endpoint role/permission checks — legato has no multi-user model, so any authenticated client can do any swarm action. This matches the existing posture for `agents/spawn` and `agents/kill`.

### 8. Error surfacing: structured JSON, status-bar-friendly

Each endpoint returns:

- `2xx` with a JSON body on success (typically `{"status": "ok"}` or the swarm snapshot).
- `4xx` with `{"error": "<message>"}` for user errors (e.g. parent already has a conductor).
- `5xx` with `{"error": "<message>"}` for unexpected failures.

The web UI shows errors via a transient toast/notification component. For the plan-approval flow specifically, errors during verdict submission (e.g. reply socket already closed because another client verdicted first) surface as "Plan was already verdicted by another client" rather than a generic failure.

## Risks / Trade-offs

- **Risk**: two web clients verdict the same plan simultaneously. → **Mitigation**: the conductor's reply socket only accepts one message before the CLI's `BroadcastRequest` returns. Subsequent verdicts are silently dropped at the conductor; the second web client gets a "verdict already received" response. UI handles this gracefully (toast + close modal).
- **Risk**: a web client connects mid-swarm and missed a `plan_proposed` event. → **Mitigation**: we add a passive `GET /api/swarm/pending-plan/<parent-id>` that returns the most recently proposed plan that hasn't been verdicted, so newly-connected clients can surface it. *Alternatively*, we can rely on the conductor's `propose-plan` call still being blocked — the user can re-trigger it from the conductor pane. Pick passive endpoint as primary, fallback as backup.
- **Risk**: long plans don't fit in a modal. → **Mitigation**: modal is scrollable; the per-sub-task prompt is collapsed by default with a "show prompt" expand. Wide screens get a side-by-side layout.
- **Risk**: unauthorized cross-instance plan verdicts. The conductor's reply socket lives in `SocketDir()` (e.g. `$XDG_RUNTIME_DIR/legato/`). Any process that can see that path can spoof a verdict. → **Mitigation**: same posture as the existing IPC — assumed local trust. The web auth boundary is at the WebSocket handshake; once authenticated, the server is the one calling `ipc.Send`, so spoofing requires breaching the legato process. Acceptable.
- **Trade-off**: no in-browser YAML editing means workflows that need surgical plan edits force a TUI fallback. For v1 we accept this; iteration can revisit if it becomes a real friction point.
- **Trade-off**: the swarm event log is per-parent, viewed in the context of the focused agent. Users who want a global "all swarms across the instance" view don't get one in this change. Reporting integration can surface that later.

## Migration Plan

Additive only — no DB or schema changes, no removed surface. Steps:

1. Add the new HTTP handlers in `internal/server/swarm.go` and register routes in `internal/server/server.go`.
2. Extend `WSMessage` in `internal/server/ws.go` and the corresponding TS type in `web/src/hooks/useWebSocket.ts`.
3. Subscribe the WebSocket hub to `EventPlanProposed` and `EventSwarmChanged` in the server bootstrap.
4. Build the React components in this order so each is testable in isolation:
   1. `SwarmEventLog` (pure render of inbox data).
   2. `AgentActionMenu` (overflow menu wrapping the per-row actions).
   3. `StartSwarmModal` (working-dir input).
   4. `PlanApprovalModal` (subscribe to plan_proposed, render, verdict).
5. Wire them in `Agents.tsx`.
6. Manual smoke test the round trip: start swarm via web → conductor proposes plan → web modal → approve → workers spawn → message a worker via web → close it → finish.

Rollback is trivial: revert the change. No data persists that's incompatible with older legato versions; the swarm tables and CLI verbs were introduced in `swarm-conductor` and are unaffected here.

## Open Questions

- **Should the start-swarm flow auto-pick the working directory** based on the selected card's workspace path (when configured)? Right now the TUI overlay uses `os.Getwd()` regardless. The web has no equivalent of cwd; it'd need to *always* prompt the user for a path. Pre-fill from workspace path is cleanest. Probably yes; revisit during implementation.
- **Should the plan-approval modal show the parent task description**, not just the plan summary? The plan summary is the conductor's words; the parent description is the user's original spec. Showing both helps the user judge whether the plan addresses the original ask. Probably yes; add to design at implementation time.
- **Per-worker terminal-attach from the action menu?** Web already has terminal streaming for the focused agent; clicking a worker in the sidebar already focuses it. Adding a "view terminal" action to the menu would be redundant. Skip.
- **Should the swarm event log entries be clickable to drill into the source worker?** E.g. clicking a `progress` event from `worker "API"` focuses that worker in the sidebar. Useful for navigation; adds modest complexity. Probably yes for v1, behind a one-line click handler.
- **Notification noise on multi-client setups.** If three users have the PWA open and a plan is proposed, three modals open. First verdict wins; the other two get a "plan already verdicted" message. Acceptable but worth confirming with the user before shipping.
