## 1. Server-side endpoints

- [ ] 1.1 Create `internal/server/swarm.go` with handlers for `POST /api/swarm/start`, `message`, `close`, `finish`, `dispatch` and `GET /api/swarm/status/<parent-id>`, `inbox/<parent-id>`, `pending-plan/<parent-id>`
- [ ] 1.2 Each handler delegates to the existing `service.SwarmService` method; map service errors to `4xx`/`5xx` JSON `{"error":"..."}` shapes
- [ ] 1.3 Register the new routes in `internal/server/server.go` behind the existing bearer-token auth middleware + CORS middleware
- [ ] 1.4 Add a passive in-memory `pendingPlans` map on the server (keyed by `parent_task_id`) populated from `EventPlanProposed` events; `pending-plan` endpoint reads from it; entries cleared on `EventSwarmChanged{NewStatus: "plan_applied" | "rejected"}` or after a TTL
- [ ] 1.5 Unit tests for each handler using `httptest.NewRecorder` + a mock `SwarmService` (mirror existing `agents_test.go` pattern)
- [ ] 1.6 Tests cover: happy path, parent-not-found, working-dir invalid, double-start refusal, unauth (401), missing CSRF/CORS preflight ok

## 2. WebSocket plan + swarm events

- [ ] 2.1 Add `MsgPlanProposed`, `MsgPlanVerdict`, `MsgSwarmChanged` to the `WSMessage` constant list and the type union in `internal/server/ws.go`
- [ ] 2.2 At server startup, subscribe to `events.EventPlanProposed` and `events.EventSwarmChanged`; broadcast matching messages to all connected clients via `hub.Broadcast`
- [ ] 2.3 Handle inbound `plan_verdict` messages on the WebSocket: forward to the conductor's reply socket via `ipc.Send`; log delivery failures (don't surface — multi-client races are expected)
- [ ] 2.4 Tests for the subscription/broadcast wiring (using the existing `mockTmux` + a real `events.Bus` so the broadcast loop actually fires)

## 3. Frontend plumbing

- [ ] 3.1 Extend `WSMessage` and `AgentInfo` types in `web/src/hooks/useWebSocket.ts` with the three new message types and any new fields
- [ ] 3.2 Add `web/src/lib/swarm.ts` with typed wrappers around the new HTTP endpoints (`startSwarm`, `messageWorker`, `closeWorker`, `finishSwarm`, `getSwarmStatus`, `drainInbox`, `peekInbox`, `getPendingPlan`); each handles auth headers + JSON error parsing
- [ ] 3.3 Add `web/src/hooks/useSwarmEvents.ts` that subscribes to `swarm_changed` WebSocket messages and provides a `useSwarmInbox(parentId)` hook returning the latest event list
- [ ] 3.4 Add a `useToast()` hook + provider for surfacing API success/error responses (or reuse if one exists; check `web/src/components/`)

## 4. Plan approval modal

- [ ] 4.1 Create `web/src/components/PlanApprovalModal.tsx` rendering plan summary, working dir, sub-task list with collapsible prompt previews
- [ ] 4.2 Modal subscribes to `MsgPlanProposed` via `useWebSocket` and opens automatically when one arrives for a known parent
- [ ] 4.3 Approve button → send WebSocket `plan_verdict` `{status: "approved", plan_path}`; close modal; toast "Plan approved"
- [ ] 4.4 Reject button → switch to notes-input view; on submit, send `plan_verdict` `{status: "rejected", notes}`; close modal; toast "Plan rejected with notes"
- [ ] 4.5 Esc / close button → close without verdict; conductor stays blocked
- [ ] 4.6 On reconnect, call `GET /api/swarm/pending-plan/<parent-id>` for any parents with active swarms and surface the modal if a pending plan is found
- [ ] 4.7 Verdict-already-received handling: when the server responds that the reply socket is closed, show "Plan already verdicted by another client" inline and close the modal

## 5. Start-swarm modal

- [ ] 5.1 Create `web/src/components/StartSwarmModal.tsx` with: parent task picker (when not pre-selected), working-directory text input, submit button
- [ ] 5.2 Pre-fill working directory from the selected card's workspace `path` if configured; otherwise leave empty
- [ ] 5.3 Submit calls `startSwarm(parentTaskID, workingDir)`; surface server-side errors inline (path doesn't exist, parent already has agent)
- [ ] 5.4 On success, close the modal and toast "Swarm started"
- [ ] 5.5 Wire a "Start swarm" button on the agents view to open the modal (with the focused parent pre-selected when applicable)

## 6. Per-worker action menu

- [ ] 6.1 Create `web/src/components/AgentActionMenu.tsx` — overflow menu (`⋯`) with role-appropriate items (worker: Send message, Close worker; conductor: Send message, Finish swarm)
- [ ] 6.2 Mount the menu on each `AgentSidebar` row that has a `parent_task_id` (i.e. swarm participants only)
- [ ] 6.3 "Send message" opens an inline input (or small modal) and on submit calls `messageWorker(subtaskID, text)` for workers, or `messageWorker(parentTaskID, text)` for the conductor (`legato swarm message` accepts either ID and the server routes appropriately)
- [ ] 6.4 "Close worker" shows a confirmation prompt then calls `closeWorker(subtaskID)`
- [ ] 6.5 "Finish swarm" prompts for a multi-line summary; on submit calls `finishSwarm(parentTaskID, summary)`
- [ ] 6.6 All actions show a toast on success and surface server errors inline

## 7. Swarm event log panel

- [ ] 7.1 Create `web/src/components/SwarmEventLog.tsx` rendering a list of events for the focused parent; each event is `#<id> [<kind>] <worker-title> — <preview>` with click-to-expand for the full payload
- [ ] 7.2 Add a Drain button that calls `drainInbox(parentID)` (acks server-side); update the local list to empty
- [ ] 7.3 Add a *peek* endpoint or query param to the inbox endpoint so live updates don't auto-ack — `GET /api/swarm/inbox/<parent-id>?peek=true` returns events without acking; default behavior (without `?peek=true`) acks
- [ ] 7.4 On `swarm_changed` for the focused parent, re-peek and re-render
- [ ] 7.5 Wire the panel into `web/src/pages/Agents.tsx` below the terminal panel; show only when the focused agent is a swarm participant

## 8. Wiring + smoke

- [ ] 8.1 Update `web/src/pages/Agents.tsx` to mount `StartSwarmModal`, `PlanApprovalModal`, the `AgentActionMenu` per row, and `SwarmEventLog` for swarm participants
- [ ] 8.2 Build the frontend (`cd web && pnpm build`) and verify no TS errors
- [ ] 8.3 Manual smoke test — round trip:
  1. Open the web PWA on a second device
  2. From the host CLI: start a swarm via the TUI (S keybinding) — verify the conductor appears in the web sidebar
  3. Have the conductor propose a plan — verify the web modal opens automatically
  4. Approve from the web — verify the conductor proceeds and dispatches workers
  5. Send a message to a worker via the web action menu — verify it lands in the worker pane
  6. Close a worker via the web — verify it transitions to `done` and drops out of the live workers
  7. Finish the swarm via the conductor's action menu — verify summary lands on the parent task and the conductor stays alive
- [ ] 8.4 Repeat smoke against an iOS PWA install (or whatever your primary mobile target is) to catch viewport / touch issues

## 9. Documentation

- [ ] 9.1 Add a "Swarm controls" section to `docs/claude/web-ui.md` covering the new modal flows and action menu
- [ ] 9.2 Update `docs/claude/swarm.md` with a paragraph noting that the web UI now has parity for the user-driven swarm verbs (link to `web-ui.md`)
- [ ] 9.3 Update `docs/claude/packages.md` to note the new `internal/server/swarm.go` file and the new WebSocket message types
- [ ] 9.4 Update `docs/swarm-integration-followup.md` to mark item #1 as done (or remove it from the list)

## 10. Final validation

- [ ] 10.1 `go build ./...` clean
- [ ] 10.2 `go vet ./...` clean
- [ ] 10.3 `go test ./...` clean
- [ ] 10.4 `cd web && pnpm build` clean
- [ ] 10.5 Smoke test from §8 passes end-to-end
