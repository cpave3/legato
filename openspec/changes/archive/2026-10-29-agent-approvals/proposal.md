## Why

A Legato that runs all the time has no person in front of each agent. Today an agent's permission request or question appears in two places only: the agent's terminal and a card in the web UI's chat for that agent. The person must find the right agent to answer it. A Claude Code request reaches Legato only because the web server's context watcher happens to subscribe to every native agent's event stream, and only while the web server runs.

Later phases of the agent approvals and credentials plan also need one place where an agent's request for access waits for a person's decision.

## What Changes

- **Durable approval requests.** A new `approval_requests` table and `service.ApprovalService` keep each request that needs the person: its source, task, execution, title, details, options, status and decision. A request has an ID such as `ap-3f9c21ab`. The first answer wins; the request is then withdrawn from every other place.
- **Agent request watcher.** A watcher in the service layer follows the event stream of every running native agent (Claude Code bridge, Codex bridge, Chimera). It turns `permission_request` and `question_request` into approval requests, withdraws them when the agent's terminal answers first, and expires them when the agent ends. It runs in the TUI and in `legato serve`, with or without a web server. Its subscription is what makes the Claude Code bridge raise requests in Legato.
- **One watcher for context fill.** The web server's context watcher stops holding its own event streams and reads `usage_updated` from the agent request watcher.
- **Decisions applied to the source.** An agent permission decision goes to `AgentService.ResolvePermission`; an answer goes to `AnswerQuestion`. A dismissal denies a permission and dismisses a question.
- **Attention.** A new `approvals` producer shows each open request under Needs intervention, with the request's options as actions.
- **Web.** A tray on every page while a request is open, with the options on each card. `?approval=<id>` on any page opens the tray on that request. The navigation count for Attention includes approvals.
- **Phone.** A new approval sends an ntfy notification whose click opens `legato://approval?url=<server>&id=<id>`. The Android app opens that server's page at the request.
- **TUI.** A banner names the open requests, and an overlay answers them with `y` and `n`.
- **API.** `GET /api/approvals`, `GET /api/approvals/{id}`, `POST /api/approvals/{id}/decision`, `POST /api/approvals/{id}/dismiss`, and the `approvals_changed` WebSocket message.

## Capabilities

### New Capabilities
- `approvals`: Durable approval requests, their lifecycle, the agent request watcher, the decision API, and where requests are shown.

### Modified Capabilities
- None. Attention gains a producer through its existing interfaces.

## Impact

- **Code:** `internal/engine/store` (migration 069, `approval.go`); `internal/service` (`approval.go`, `approval_watcher.go`, `attention_approval.go`, notifier click links); `internal/server` (`approvals.go`, `agent_context.go` reads from the watcher, navigation); `cmd/legato` (wiring for both runtimes, IPC); `internal/tui` (banner, overlay); `web/src` (tray, API client, query sync); `mobile/` (deep link).
- **APIs:** four endpoints, one WebSocket message type, one deep link form.
- **Config:** `notifications.approvals` (default `true`): send an ntfy notification for each new approval. `web.public_url` (optional): the address the phone uses for this server in approval links.
- **Dependencies:** none.
- **Unchanged:** the chat cards for permission and question requests keep working, and stay in step because the first answer wins.
