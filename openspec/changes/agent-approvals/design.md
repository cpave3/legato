## Context

All three native agent tools speak one protocol to Legato (`/v1/sessions/{id}/...`), and `internal/engine/chimera.Client` reads it for all of them. A pending permission arrives as a `permission_request` event (`requestId`, `tool`, `command`, `reason`, optional `plan` and `options`); a question as a `question_request` (`requestId`, `questions[]`). `permission_resolved`, `permission_timeout`, `question_resolved`, `question_timeout` and `run_finished` close them. A snapshot lists `pendingPermissions` and `pendingQuestions`.

`AgentService.ResolvePermission(ctx, taskID, requestID, PermissionAction)` and `AnswerQuestion(ctx, taskID, requestID, QuestionAnswer)` answer a request over the agent's endpoint.

The Claude Code bridge raises a request in Legato only while at least one client holds its event stream (`handleModPermission` checks `len(b.subscribers)`). When the last one leaves, it expires its requests. The Codex bridge raises every request whatever its viewers. The web server's `contextWatcher` holds one stream per running native agent to read context fill, so today Claude Code requests reach Legato as a side effect.

Attention has producers that return a snapshot of items, actions that a producer's handler executes at an expected revision, and Dismiss. Both runtimes wire Attention through `wireAttention`; the TUI also builds its own instance.

## Goals / Non-Goals

**Goals:**
- Every permission and question request of every native agent waits in one durable list, shown in Attention, a web tray, the TUI and as a phone notification.
- The first answer, from any place, wins, and the others see the request leave.
- A request ends when its agent ends, and when the person dismisses it.
- The approval service is general: phase 2 (credential broker) submits requests through the same API.

**Non-Goals:**
- Credential requests and `[legato approvals]` messages to agents. They come with the broker (phase 2); the service supports them from the start.
- Signed decisions and a native approval sheet in the Android app (phase 5).
- Requests from terminal-only agents (menus read from a pane). Later.
- Plans, workflow gates and swarm plans as approval sources. They keep their own surfaces for now.

## Decisions

### D1. One table, one service, sources by name

`approval_requests` holds: `id` (`ap-` and 8 hex characters), `source` (`agent_permission`, `agent_question`, later `credential`), `source_key` (unique with `source`), `task_id`, `execution_id`, `title`, `summary`, `details` (JSON), `options` (JSON), `blocking`, `status`, `decision` (JSON), `decided_by`, `revision`, timestamps. Statuses: `pending`, `approved`, `denied`, `answered`, `dismissed`, `withdrawn`, `expired`, `failed`.

`ApprovalService.Submit` is idempotent on `(source, source_key)`: a second submit of the same request returns the existing row and reports that nothing was created. Only the process that created a request sends its notification, so two Legato processes on one database do not notify twice.

A source registers an `ApprovalResolver` by name. `Decide` changes a pending row to its decided status in one conditional update, then calls the resolver. If the update finds no pending row, the decision lost and returns `ErrApprovalDecided`. If the resolver fails, the row becomes `failed` with the error.

*Alternative:* store approvals as Attention items. Rejected: Attention items are projections that a producer can resolve on any snapshot, and they have no decision record.

### D2. The agent request watcher lives in the service layer

`ApprovalWatcher` keeps one event stream per running agent with a native endpoint. It finds the agents from the agent service at start, on `EventAgentStarted`, `EventAgentChimeraDiscovered` and `EventAgentDied`, and every 15 seconds. When a stream opens, the watcher reads the session snapshot and submits each pending permission and question, so a request raised while Legato was down is found.

The source key is `<execution_id>/<endpoint session id>/<requestId>`. A bridge can reuse a request ID after it restarts; the execution ID tells the two apart.

The watcher passes every event to listeners. The web server's context watcher is one: it keeps its first reading from `GET /context` but no longer opens its own stream.

*Alternative:* keep the server's context watcher and add the approval logic there. Rejected: a TUI without a web server, and `legato serve`, must both see requests.

### D3. Holding the stream is the approver signal

The Claude Code bridge raises requests while a client holds its event stream. The watcher always holds one, so requests always reach Legato while Legato runs. No new bridge endpoint is needed. When Legato stops, the bridge expires the requests it raised, and the terminal dialog remains, as today.

### D4. How requests close

| Event | Effect on a pending approval |
| --- | --- |
| `permission_resolved`, `question_resolved`, `*_timeout` for its request | `withdrawn` (answered elsewhere) |
| `run_finished` | `withdrawn` for that execution's pending requests |
| snapshot after reconnect without the request | `withdrawn` |
| `EventAgentDied` for its task | `expired` |
| start of a Legato process: execution terminal | `expired` |
| person dismisses | permission: denied with the note "Dismissed in Legato"; question: dismissed |

A decision made in Legato also produces a `permission_resolved`; the watcher ignores it because the row is no longer pending.

### D5. Options

A permission's options come from the request's `options` (`[{id, label}]`), else one `allow`. Every permission also has `deny`. A question's options are its own; the web tray and the API take answers per question. Attention gets one execute action per permission option and a deny action. For a question with one single-choice question, Attention gets one action per option; otherwise only an action that opens the tray.

### D6. Where requests are shown

- **Attention:** producer `approvals`, signal kind `approval`, category `approval`, urgency `normal`. Source revision is the approval's revision. Reconciled on `EventApprovalChanged`.
- **Web:** `ApprovalTray` in `Layout`, collapsed to a bar with the count when the person closes it. `?approval=<id>` opens it on that request. The tray reads `GET /api/approvals?status=pending`, refreshed by `approvals_changed`.
- **TUI:** a line above the key bar on every view while requests are open: `◆ 2 approvals need you · ! answer`. `!` opens an overlay with one request at a time: `y` takes the first option, `n` denies, `j`/`k` move, `esc` closes.
- **Phone:** ntfy with `Click: legato://approval?url=<server>&id=<id>`. The server address is `web.public_url`, else the address the pairing flow announces, else none (then no click link). The Android app opens the saved server whose address matches (or the only saved server) at `/?approval=<id>`.

### D7. API

- `GET /api/approvals?status=pending|all&task=<id>` → `{approvals: [...]}`.
- `GET /api/approvals/{id}` → the approval.
- `POST /api/approvals/{id}/decision` with `{decision: "allow"|"deny"|"answer", option?, feedback?, answers?}` → the decided approval; `409` when already decided, `400` for an unknown option or bad answers.
- `POST /api/approvals/{id}/dismiss` with `{note?}`.
- `approvals_changed` on the WebSocket with `item_id` and `kind`.

`EventApprovalChanged` reaches other processes as IPC message `approval_changed`.

## Risks / Trade-offs

- [The watcher holds a stream for every native agent] → One SSE connection per agent on loopback; the context watcher's stream is removed, so the count does not grow.
- [Two processes on one database both watch] → Submit is idempotent and only the creator notifies; decisions are conditional updates.
- [A Claude Code request is now always raised in Legato] → This is the behaviour today whenever the web server runs; the terminal dialog stays answerable.
- [A dismissal denies a permission the agent needed] → The note says it was dismissed in Legato, and the agent can ask again.

## Migration Plan

Migration 069 adds `approval_requests`. No data moves. A Legato older than this change cannot open the database afterwards, as for every migration.

## Open Questions

- Should plans, workflow gates and swarm plans move into the approval service later, or stay with their own pages?
