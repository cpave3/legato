# Approvals

An **approval** is a request that waits for the person: an agent's permission request ("run this command"), an agent's question, and later a credential request (see [Credentials](credentials.md)). Legato keeps each one durably, shows it everywhere the person might be (the web tray, Attention, the TUI and the phone), and applies the first decision to the request's source. The design is in `openspec/changes/agent-approvals`.

## What an approval is

`approval_requests` (migration 069), kept by `internal/engine/store/approval.go`:

| Field | Meaning |
| --- | --- |
| `id` | `ap-` and 8 hex characters |
| `source`, `source_key` | Who raised it, and the request's own key there. Unique together: the same request submitted twice is stored once |
| `task_id`, `execution_id` | The agent it belongs to |
| `title`, `summary` | One line, and what it would do (a command) |
| `details` | JSON, as the source wrote it: the tool, command, reason, plan, questions, the task's title and the agent tool |
| `options` | `[{id, label, kind}]`, `kind` `allow` or `deny` |
| `blocking` | The requester waits for the decision |
| `status` | `pending`, then one of `approved`, `denied`, `answered`, `dismissed`, `withdrawn`, `expired`, `failed` |
| `decision`, `decided_by`, `decided_at` | What was decided, where (`web`, `tui`, `attention`), when |
| `error` | Why it failed, or why it was withdrawn or expired |

`service.ApprovalService` (`internal/service/approval.go`):

- `Submit` stores a request, or returns the one already stored for its source and source key. Only the call that stored it publishes `created` and runs the `Created` hook (the phone notification), so two Legato processes on one database notify once.
- `Decide` checks the decision against the approval (an allow must name one of its allow options, or takes the first), lets the source's `ApprovalValidator` refuse it, closes the row in one conditional update, then calls the source's `ApprovalResolver`. Only the first decision counts; the next gets `ErrApprovalDecided`. A resolver error leaves the approval `failed` with the error.
- `Dismiss` is a decision of `dismiss`; the source says what that means for it.
- `Withdraw`, `WithdrawExecution` and `ExpireTask` close pending approvals without asking the source.
- `Wait` reads an approval until it is decided or a timeout passes (for requesters that wait, from phase 2).
- Every change publishes `EventApprovalChanged` and, through the `Broadcast` hook, the IPC message `approval_changed`, so other Legato processes see it.

## Agent permissions and questions

`service.ApprovalWatcher` (`approval_watcher.go`) follows the event stream of every running agent with a native endpoint (Claude Code and Codex through their bridges, Chimera), in the TUI and in `legato serve`, whether or not a web server runs. It finds the agents at start, on `EventAgentStarted`, `EventAgentChimeraDiscovered` and `EventAgentDied`, and every 15 seconds.

- When a stream opens, the watcher reads the session's snapshot and submits each pending permission and question, so a request raised while Legato was down is found. A request no longer in the snapshot is withdrawn.
- `permission_request` and `question_request` become approvals with source `agent_permission` or `agent_question` and source key `<execution>/<session>/<requestId>` (a bridge can reuse request IDs after it restarts).
- A permission's options are the ones the agent offered (`once`, `always`, ...), else one `allow`, and always `deny`. A question has no options: it is answered.
- `permission_resolved`, `permission_timeout`, `question_resolved` and `question_timeout` withdraw the request (it was settled at the agent: in its terminal, or in the chat card). `run_finished` withdraws the execution's requests. `EventAgentDied` expires them, and so does a reconcile that finds the execution no longer running.
- The resolver applies decisions through `AgentService.ResolvePermission` and `AnswerQuestion`: allow with the option, deny with the person's note, dismiss as a denial with the feedback "Dismissed in Legato" (a question is dismissed).
- Holding the stream is what makes a Claude Code bridge raise its requests in Legato: it raises them only while a client holds its events. The watcher always holds one, so no other "approver attached" signal is needed.
- The watcher passes every event to listeners. The web server's context watcher is one (`Server.UseAgentEvents`): it takes its first reading from `GET /context` and no longer opens a stream of its own.

## Where the person sees them

- **Web tray** (`ApprovalTray`, mounted by `Layout` while the navigation summary counts any): a column of cards at the bottom right, above the tab bar on a phone, with who asks, what it would run, the plan when there is one, and its options. Deny takes a note for the agent. Questions use `NativeQuestionCard` (with `touch`). Below the `md` breakpoint (a phone, the Legato app) the tray is a sheet up to 85% of the screen with larger text, and every choice is a full-width button 48 px tall, stacked; from `md` up it is the compact desktop tray. The tray folds to a count and opens again when a new approval arrives. `?approval=<id>` on any page opens it on that card and is then dropped from the address.
- **Attention**: producer `approvals`, signal kind `approval`, under Needs intervention. Each allow option is an action `allow:<id>`, deny is `deny`; a question with one single-choice question gets `answer:<index>` for each option and `dismiss`. Navigation kind `approval` opens `/attention?approval=<id>`.
- **TUI**: the key bar says `◆ 2 approvals need you · ! answer` on every view. `!` opens an overlay of the pending approvals: `y` allows with the first option, `1`–`9` with that option (a credential's `1 for 30 minutes · 2 once`), `n` denies (dismisses a question), `j`/`k` choose, `esc` closes. A question is answered in the web UI. `internal/tui/app_approvals.go`, `overlay/approval.go`.
- **Phone**: each new approval sends one ntfy notification (`ApprovalNotifications` in `approval_notify.go`). Its title is the approval's, its message names the task and the agent tool and never the command, and its click opens `legato://approval?url=<server>&id=<id>`. The Legato app opens the saved server at that address (or the only one saved) at `/?approval=<id>`. Off with `notifications.approvals: false`. The server's address is `web.public_url`, else `https://<web.tls.hostname>:<port>` when TLS has a host name, else the first local network address; without any the notification has no link. A CLI or `legato mcp serve` process that creates an approval works the address out the same way from the configuration.

## Signed approvals

Some decisions can be made only by a person at an enrolled phone, with a key the phone keeps in its secure hardware and unlocks with their fingerprint or PIN for each signature. An agent with the web token can call every API, but cannot touch the phone. The design is in `openspec/changes/signed-approvals`.

- **Which approvals.** An approval is `signed` when its source says so. A credential rule decides with its `signed`: `true` or `false` when written, and otherwise `true` for a rule with `approve: request` once any device is enrolled (`needsSignature`). Adding and removing devices is always signed. An allow without a valid signature is refused with `ErrApprovalNeedsSignature` (`403`); a deny never needs one. The TUI and Attention cannot allow a signed approval, and say to use the phone.
- **Devices** (`internal/service/approval_devices.go`, migration 072: `approval_devices`, `approval_challenges`, `instance_values`). A phone asks `POST /api/devices/challenge`, makes a key with that challenge and sends `POST /api/devices {name, kind: "android", chain, challenge}`. `internal/engine/attest` checks the chain: Google's roots, the revocation list (kept a day; a chain is refused while none was ever read), the challenge, a hardware security level, EC P-256 signing, authentication for each use, and the app (`dev.legato.mobile`, signed with one of `approval_devices.android_app_digests`; the default is the public debug key's, so set the digest of a private release key, see `mobile/README.md` § Building). The first device is enrolled at once. A later one is `pending` and raises an approval of source `devices`, which an enrolled device signs. `DELETE /api/devices/{id}` raises one too. `GET /api/devices` lists them; Settings › Devices in the web UI shows them.
- **Signing.** `POST /api/approvals/{id}/challenge {device}` returns the request in RFC 8785 form (`canonical`, base64: what Legato knows of it, with this server's ID) and a 32-byte challenge for that approval and device, good for 5 minutes and once. The device signs `{"challenge","device","option","request","v":1}` (ECDSA P-256, SHA-256, DER) and sends it in the decision as `signature: {device, challenge, value}`. `VerifyDecision` spends the challenge whatever the outcome, checks the request is unchanged and verifies the signature. The approval records `decided_device`, and the credential audit names the device.
- **Lost phone.** `legato device list` lists the devices; `legato device reset` at the host removes them all, writes the audit log, sends an ntfy notice and records the reset (`instance_values` key `devices_reset`, with a sequence number). Attention shows the last reset (producer `devices`, signal `devices_reset`, high urgency, opening `/settings#devices`) until it is dismissed; enrolling a phone afterwards does not hide it, since that phone may be the one the reset was for, and the next reset is a new revision. The command sends `approval_changed` over IPC so a running Legato shows it at once. Passkeys are not built yet.
- **The phone's side** is in `mobile/README.md` § Signed approvals.

## API

Behind the web token:

- `GET /api/approvals?status=pending|all&task=<id>` → `{approvals: [...]}`, oldest first (`pending` by default).
- `GET /api/approvals/{id}`.
- `POST /api/approvals/{id}/decision` with `{decision: "allow"|"deny"|"answer", option?, feedback?, answers?, by?}`: `200` with the approval, `409` when it is no longer pending, `400` for an option it does not have or answers to questions it does not ask, `502` when the agent refused it.
- `POST /api/approvals/{id}/dismiss` with `{note?, by?}`.
- `approvals_changed` on the WebSocket (`item_id`, `agent_id`, `kind`), and `approvals` in `/api/navigation-summary`.

## Wiring

`startApprovals` in `cmd/legato/approval_runtime.go` builds the service and the watcher for both runtimes; `attach` gives a web server the API, the WebSocket forwarder and the watcher's events; `registerApprovalAttention` adds the Attention producer (used by `wireAttention` and the TUI's own Attention instance).
