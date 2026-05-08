## Why

The swarm-conductor change shipped working internals — conductor lifecycle, plan approval, send-keys IPC, agent grouping — and the web UI received the visual half (sidebar grouping, conductor/worker badges, accent borders). What it didn't get was the *acting* half: there's no way to start a swarm, approve a plan, message a worker, close a sub-task, or finish a swarm from the web UI. Today a phone or tablet user can watch a swarm but has to drop into a tmux pane on the host to do anything.

For users who run legato as a remote PWA — the use case the web UI was built for — this means swarm features only work when you're sitting at the host. The plan-approval gate is the most painful: a conductor that submits a plan blocks until verdict, and if you're not at the TUI when the IPC fires, you have no way to respond from the device you're actually using.

This change closes the gap by mirroring the swarm CLI surface as web-side HTTP/WebSocket endpoints and surfacing the matching UI in the PWA.

## What Changes

- **New web endpoints** for the swarm CLI verbs the user needs to act:
  - `POST /api/swarm/start` (parent_id, working_dir) → spawns the conductor, mirrors `S` keybinding flow.
  - `POST /api/swarm/message` (subtask_id, text) → wraps `legato swarm message`.
  - `POST /api/swarm/close` (subtask_id) → wraps `legato swarm close`.
  - `POST /api/swarm/finish` (parent_id, summary) → wraps `legato swarm finish`.
  - `POST /api/swarm/dispatch` (subtask_id) → manual dispatch (mostly for debugging; the conductor usually drives this).
  - `GET /api/swarm/status/<parent-id>` → JSON snapshot, mirrors `legato swarm status`.
  - `GET /api/swarm/inbox/<parent-id>` → drain unacked events, mirrors `legato swarm inbox`.
- **New WebSocket message types** for live plan/event delivery:
  - `MsgPlanProposed` — emitted when any IPC `plan_proposed` event arrives, carries `parent_task_id`, `plan_path`, `reply_socket`.
  - `MsgPlanVerdict` — sent *from* the web client when the user verdicts; server forwards to the conductor's reply socket.
  - `MsgSwarmChanged` — emitted on `EventSwarmChanged`, lets the web invalidate cached state and re-fetch agents/inbox.
- **New web UI components**:
  - **Start-swarm modal** triggered from a button on the agents view (or a card-level action when the board UI lands in web). Collects working directory; submits to `POST /api/swarm/start`.
  - **Plan-approval modal** that opens automatically when `MsgPlanProposed` arrives. Renders the plan summary + sub-task list; offers approve / reject-with-notes / dismiss. Edit-in-`$EDITOR` is **not supported** in v1 (no editor surface in web — see design.md for the alternative).
  - **Per-worker action menu** in `AgentSidebar` items: "Send message", "Close worker", and on the conductor entry "Finish swarm".
  - **Swarm event log panel** in the agent terminal view (or as a collapsible panel) showing recent unacked events, with a "drain inbox" action that calls `legato swarm inbox` server-side.
- **Auth + CORS** apply to all new endpoints unchanged (existing bearer-token middleware + CORS allow-origin).
- **No new third-party dependencies.**

## Capabilities

### New Capabilities

- `web-swarm-controls`: server-side HTTP + WebSocket endpoints for swarm CLI verb equivalents, plus the auth/error semantics that match the existing web API.
- `web-plan-approval`: the web-side plan-approval modal — listens for `MsgPlanProposed`, renders the plan, sends `MsgPlanVerdict` back. Includes the substitute for in-overlay editing (download plan / re-upload edited plan flow).
- `web-swarm-actions`: per-worker and per-conductor action UI in the agents sidebar — message input, close button, finish button, and the swarm-start entry point.

### Modified Capabilities

- `multi-server`: the new swarm endpoints inherit the existing multi-instance auth (Bearer token middleware), CORS rules, and TLS config; no changes to those, but we note them under modified to record the surface expansion.

## Impact

- **Removed**: nothing.
- **New code**:
  - `internal/server/swarm.go` — HTTP handlers for the new endpoints.
  - `internal/server/ws.go` — WebSocket message handlers for `MsgPlanProposed`, `MsgPlanVerdict`, `MsgSwarmChanged`. Subscribe to `EventPlanProposed` and `EventSwarmChanged` and broadcast to clients.
  - `web/src/components/StartSwarmModal.tsx` — working-dir input modal.
  - `web/src/components/PlanApprovalModal.tsx` — plan review + verdict UI.
  - `web/src/components/SwarmEventLog.tsx` — pending events panel.
  - `web/src/components/AgentActionMenu.tsx` — per-worker overflow menu.
- **Modified**:
  - `internal/server/server.go` — register the new routes.
  - `internal/server/agents.go` — extend `AgentResponse` if needed for swarm context (already has role / parent_task_id / subtask_id from the previous change).
  - `web/src/pages/Agents.tsx` — wire the new modals and action menu.
  - `web/src/hooks/useWebSocket.ts` — add the new message types to `WSMessage` and the agent-info dispatcher.
  - `web/src/lib/auth.ts` — no changes; `authHeaders()` covers the new endpoints.
- **Server changes are additive**: existing endpoints unchanged.
- **Frontend bundle** grows by 3 new components (~5–8 KB minified). No new third-party dependencies.
- **Compatibility**: Web clients running an older build won't receive the new WebSocket messages but won't break — they'll just continue to operate without swarm controls. Clients on the new build connecting to an older legato server will see the action buttons but the API calls will 404; UI should surface those failures gracefully.
- **No DB or migration impact**.
- **No CLI surface changes** — server endpoints wrap the existing `SwarmService` methods directly.
