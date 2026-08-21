# Web Conductor Dashboard — Current Scope and Deferred Ideas

## Implemented command center

The web Agents page now promotes a selected swarm participant into a swarm-level command center:

- Desktop keeps the conductor in the primary pane.
- Every durable subtask is visible in a scrollable worker area, including queued and terminal workers without sessions.
- One live worker can expand into an interactive pane beside the conductor.
- Compact tmux cards use bounded read-only captures. They do not subscribe or send resize messages.
- Native Chimera cards show lifecycle metadata and use `NativeAgentPanel` when expanded.
- Mobile uses one participant picker and one interactive pane.
- The existing swarm event log refreshes on lifecycle and event-only `swarm_changed` notifications without automatically acknowledging inbox entries.
- Board task detail can start a swarm with the task fixed and the normal task working-directory precedence.

This remains part of `/agents`; solo agents keep the existing single-agent layout.

## Current backend capabilities

| Need | Current state |
|---|---|
| Swarm status | `GET /api/swarm/status/:id` returns parent state, active step, and queued through terminal subtasks |
| Interactive streams | One WebSocket client can subscribe to several agents; output is keyed by `agent_id` |
| Compact previews | `GET /api/agents/{task_id}/preview` returns a bounded plain-text tmux capture without creating a stream or size owner |
| Event refresh | The event bus forwards `swarm_changed`; question and debounced progress inserts also publish an event-only marker |
| Messaging and lifecycle | Existing message, close, finish, cancel, and plan-verdict APIs remain in use |
| Raw plan editing | Pending plan APIs expose parsed plans, but there is no raw YAML read/write contract |

A batch subscription message is not required. The command center subscribes to the conductor and at most one expanded worker. Compact cards poll snapshots only while eligible and visible.

## Deferred ideas

The implemented command center intentionally excludes the broader original dashboard design:

- a separate `/conductors/:parent_id` route;
- raw YAML editing and plan mutation endpoints;
- a dedicated plan-step sidebar;
- a retained historical event timeline beyond the unacknowledged inbox;
- continuously live terminals for all compact workers;
- cross-swarm monitoring;
- TUI multi-pane parity.

Raw plan editing needs an explicit revision and approval model before adding write endpoints. A historical timeline needs retention and acknowledgement semantics distinct from the conductor inbox. TUI parity needs separate capture buffers and polling orchestration.

## Safety constraints

- Only interactive panels can report dimensions to tmux.
- Automatic event refresh always peeks; only an explicit Drain action acknowledges entries.
- Worker lists come from durable swarm status first and are enriched with live sessions, so missing or dead sessions do not remove planned work from view.
- Large swarms use a scrollable list and bounded preview polling rather than assuming the default concurrency of four is a total limit.
