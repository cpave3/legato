# Web Conductor Dashboard — Future Implementation Notes

## 1. Goal & Non-Goals

**Goal:** A dedicated web view for supervising a single swarm conductor. It replaces the "one terminal at a time" model with a dense, live dashboard: plan step navigator, multi-worker terminal grid, event feed, and inline plan editing.

**Non-goals:**
- Not a general cross-swarm monitoring page (that’s the global activity stream, Proposal 4).
- Not a log-analysis or metrics-retention tool.
- Not a replacement for the generic `/agents` page — solo agents stay there.

## 2. Layout Sketch

```
┌────────────────┬────────────────────────┬──────────────┐
│  Plan Steps    │   Terminal Multiplex   │  Event Feed  │
│  (collapsible) │   ┌─────┐ ┌─────┐      │  (live)      │
│  • Step 1 ✓    │   │ w-1 │ │ w-2 │      │              │
│  • Step 2 ▶    │   └─────┘ └─────┘      │              │
│    - task-a    │   ┌─────┐ ┌─────┐      │              │
│    - task-b    │   │ w-3 │ │ w-4 │      │              │
│  • Step 3 ○    │   └─────┘ └─────┘      │              │
└────────────────┴────────────────────────┴──────────────┘
```

**Desktop:** 3-column grid (plan | multiplex | events). The multiplex defaults to 2×2 panes (expandable 1×2 or 2×1). Each tile shows the worker’s name, role tag, and an xterm.js instance.

**Mobile:** Accordion stack — plan stepper first, then the focused terminal pane (swap via tap), then event feed collapsed.

**Plan editor overlay:** Modal/drawer that surfaces raw YAML, edits, and re-submits without leaving the page.

## 3. Backend Dependencies & Gaps

| Need | Current State | Gap |
|---|---|---|
| Plan YAML for editing | `/api/swarm/pending-plan/:id` returns parsed `SwarmPlan` objects | No endpoint returns **raw YAML text** for editing, and no PUT endpoint to persist edits back to disk |
| Multi-stream multiplex | WS supports `subscribe_agent` (one at a time) per client | Nothing prevents multiple subscriptions, but there is no batch `subscribe_agents` message type; the dashboard needs to interleave `agent_output` for 2–4 panes cleanly |
| Live event feed per parent | Events (`EventSwarmChanged`, `EventAgentDied`, progress) go to the `events.Bus` and the REST inbox (`/api/swarm/inbox/:id`) | The server does **not** forward swarm events over WebSocket in real time. Needs an event-bus → hub bridge emitting a new `swarm_event` WS type |
| Snapshot / subtask statuses | `/api/swarm/status/:id` exists and returns queued/dispatched/in-progress/done counts | ✅ Ready to use |
| Messaging / close / finish | `/api/swarm/message`, `/close`, `/finish` exist | ✅ Ready to use |

## 4. Frontend Dependencies

- **Multi-stream terminal hook:** A new hook/container that manages multiple `xterm.js` instances, each with its own FitAddon, resize heartbeat, and prompt-detection dismissal — driven by a shared `useWebSocket` subscription keyed by `agent_id`.
- **YAML editor component:** Lightweight Monaco or CodeMirror-lite with YAML syntax highlighting. The existing read-only plan modal (`PlanApprovalModal`) proves rendering; editing needs read/write state.
- **Plan-step stepper component:** Presentational stepper consuming `SwarmStatusData` from the existing `useSwarmEvents` hook.
- **Inline action menu:** The web's `AgentActionMenu` component and the TUI's `overlay.AgentActionOverlay` (send message, close worker, finish swarm) cover the same surface; the web side just needs to move from a modal into a per-tile dropdown.

## 5. Reusable Pieces from Tier 1+2

Once the current swarm enhancements land, the dashboard gets these for free:

- **AgentActionMenu** — per-worker send/close/finish actions.
- **Macro library** — saved send-keys snippets, useful for broadcasting corrections to all visible panes.
- **Sparkline telemetry** — replaces static durations in tile headers with 7–10 minute activity bars.
- **Adaptive status line** — worker tmux status bars already show swarm-local progress; dashboard tiles inherit the same context.
- **Prompt detection & `PromptBar`** — each pane can surface detected tool/plan approvals independently.
- **PlanApprovalModal** — cross-surface approve/reject/dismiss already wired; dashboard reuses the same verdict flow.

## 6. Open Design Questions

1. **Pane limit and layout:** Should it be fixed 2×2, user-pickable (1×2, 2×1, 2×2), or auto-expanded as workers start?
2. **Focused pane vs. global prompt bar:** If four panes show four different prompt detections, does the single bottom `PromptBar` target the last-clicked pane?
3. **Event feed fidelity:** Should it stream raw inbox entries (progress/built/died) or a higher-level deduplicated timeline? How aggressively should it cull old events?
4. **Plan editing semantics:** Does saving YAML write directly to disk and `SIGUSR1` the conductor, or create a new pending-plan proposal that requires an explicit approval step?
5. **Mobile scope:** Is the dashboard desktop-only, or does it degrade to a "one pane + peek" mobile layout?

## 7. Rough Sizing

| Piece | Size | Rationale |
|---|---|---|
| Layout shell & routing | **M** | New route (`/conductors/:parent_id`), responsive grid, persistent pane layout state |
| Multi-stream terminal multiplex | **L** | Multiple concurrent xterm.js lifecycles, per-pane resize, interleaved WS traffic, performance at 4 streams |
| Plan step navigator | **S** | Presentational; data already available from `/api/swarm/status/:id` |
| Live event feed | **M** | New server-side event-bus → WS bridge + scrollable/deduped list UI |
| Plan editor (YAML) | **M** | New raw-YAML endpoints + editor component + save/reload orchestration |
| Action menu integration | **S** | Existing component; minor UI relocation into tile headers |

**Total feel:** Medium–Large. The multiplex terminal is the riskiest piece; everything else is wiring existing surfaces into a new layout.
