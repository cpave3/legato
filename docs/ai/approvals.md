# Approvals

An **approval** is a request that waits for the person: an agent's permission request ("run this command"), an agent's question, and later a credential request (see [Credentials](credentials.md)). Legato keeps each one durably, shows it everywhere the person might be (the web tray, Attention, the TUI and the phone), and applies the first decision to the request's source. The design is in `openspec/changes/archive/2026-10-29-agent-approvals`.

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

- `Submit` stores a request, or returns the one already stored for its source and source key. Only the call that stored it publishes `created` and runs the `Created` hook (the phone notification), so two Legato processes on one database notify once. A `Quiet` request (a Reeve change set at the `auto` level, decided as it is made) never runs the hook.
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
- Chimera's HTTP client supplies positional question IDs (`q1`, `q2`, …) before the watcher or web panel sees its id-less questions. Its interaction adapter converts the panel's keyed answers to Chimera's question-text answer array, including custom text; bridge agents keep their own answer format. See [Chimera native integration](chimera-native.md) § Questions.
- `permission_resolved`, `permission_timeout`, `question_resolved` and `question_timeout` withdraw the request (it was settled at the agent: in its terminal, or in the chat card). `run_finished` withdraws the execution's requests. `EventAgentDied` expires them, and so does a reconcile that finds the execution no longer running.
- The resolver applies decisions through `AgentService.ResolvePermission` and `AnswerQuestion`: allow with the option, deny with the person's note, dismiss as a denial with the feedback "Dismissed in Legato" (a question is dismissed).
- Holding the stream is what makes a Claude Code bridge raise its requests in Legato: it raises them only while a client holds its events. The watcher always holds one, so no other "approver attached" signal is needed.
- The watcher passes every event to listeners. The web server's context watcher is one (`Server.UseAgentEvents`): it takes its first reading from `GET /context` and no longer opens a stream of its own.

## Where the person sees them

- **Web tray** (`ApprovalTray`, mounted by `Layout` while the navigation summary counts any, or focus mode is on): a column of cards at the bottom right, above the tab bar on a phone, with who asks, what it would run, the plan when there is one, and its options. Deny takes a note for the agent. Questions use `NativeQuestionCard` (with `touch`). Below the `md` breakpoint (a phone, the Legato app) the tray is a sheet up to 85% of the screen with larger text, and every choice is a full-width button 48 px tall, stacked; from `md` up it is the compact desktop tray. The tray folds to a count and opens again when a new approval arrives. `?approval=<id>` on any page opens it on that card and is then dropped from the address.
- **Paired instances' change sets** (`PeerApprovalCard` in the tray, `usePeerApprovals`): each paired instance's pending Reeve change sets, below this instance's, marked with the instance and the Reeve's violet edge. They are decided there, with a device enrolled there: **◈ Approve on <instance>** (no ◈ when it needs no signature) opens that instance's page at `/?approval=<id>`, and **Open <instance> ↗** its page. The server asks its peers every 30 seconds (`Server.StartPeerApprovals`, `ReevePeerService.PeerApprovals`, each peer's `GET /api/integrations/reeve/approvals` with scope `reeve`, `OwnApprovals`), keeps what they said, sends `approvals_changed` when the set changes, and counts them in the navigation summary as `peer_approvals`, so `Layout` mounts the tray for them too. `GET /api/reeve/peers/approvals` → `{approvals: [{id, title, summary?, signed?, verified?, created_at, instance_id, instance, url, instance_url}], unreachable: [names]}`, as last heard. See [The Reeve](reeve.md) § Reeve to Reeve.
- **Focus mode** (`QuestionFocus.tsx`, opened by the header's button while an approval asks questions): the question approvals one at a time over the whole screen, each approval's questions reviewed and sent together as one `answer` decision (an approval with one question sends from it); keys on the dialog (1–9, ↑↓, Enter, ←, `/`, ⌘/Ctrl+Enter, Esc); on a phone it fills the screen, `index.css`'s cap on dialogs overridden, with Back and Next in a bar at the foot. On or off is `lib/focusMode.ts`, outside the tray, since `Layout` drops the tray while nothing is pending and keeps it mounted while focus mode is on: with nothing to answer it fades out (`useFade`) and rests (a note at the top right, under the page header, so it never covers a message box; or "Focus is on" in the tray's header when other approvals wait), and the next question opens it again. `lib/questionDrafts.ts` is the drafting both it and `NativeQuestionCard` use, and `recommended()` shows an agent's "(Recommended)" as a mark without changing the label sent.
- **Browser notifications** (`ApprovalNotifier`, mounted by `Layout` where the browser can show notifications: a secure page, and not the Legato app, which has ntfy): each approval that arrives while the Legato tab is not in front is a notification saying what is asked, by which task's agent and what it would do. A click brings the tab forward with the tray open on it (`?approval=<id>`); the notification goes once the approval is decided. Approvals already waiting when the page loads do not notify. Settings › Security › **Notify me of approvals** turns it off for this browser (`legato:approval-notifications`, on by default) and asks the browser for permission. `web/src/lib/approvalNotifications.ts`.
- **Attention**: producer `approvals`, signal kind `approval`, under Needs intervention. Each allow option is an action `allow:<id>`, deny is `deny`; a question with one single-choice question gets `answer:<index>` for each option and `dismiss`. Navigation kind `approval` opens `/attention?approval=<id>`.
- **TUI**: the key bar says `◆ 2 approvals need you · ! answer` on every view. `!` opens an overlay of the pending approvals: `y` allows with the first option, `1`–`9` with that option (a credential's `1 for 30 minutes · 2 once`), `n` denies (dismisses a question), `j`/`k` choose, `esc` closes. A question is answered in the web UI. `internal/tui/app_approvals.go`, `overlay/approval.go`.
- **Desktop app**: the feed of its Legato widget lists every paired server's pending approvals, allowed, denied or signed with the desktop's key from there, decisions `by: "desktop"`; a note, a question or a secret opens the main window on the approval. See [Desktop app](desktop.md) § Approvals in the feed.
- **Phone**: each new approval sends one ntfy notification (`ApprovalNotifications` in `approval_notify.go`). Its title is the approval's, its message names the task and the agent tool and never the command, and its click opens `legato://approval?url=<server>&id=<id>`. The Legato app opens the saved server at that address (or the only one saved) at `/?approval=<id>`. Off with `notifications.approvals: false`. The server's address is `web.public_url`, else `https://<web.tls.hostname>:<port>` when TLS has a host name, else the first local network address; without any the notification has no link. A CLI or `legato mcp serve` process that creates an approval works the address out the same way from the configuration.

## Signed approvals

Some decisions can be made only by a person at an enrolled phone, with a key the phone keeps in its secure hardware and unlocks with their fingerprint or PIN for each signature. An agent with the web token can call every API, but cannot touch the phone. The design is in `openspec/changes/archive/2026-10-29-signed-approvals`.

- **Which approvals.** An approval is `signed` when its source says so. A credential rule decides with its `signed`: `true` or `false` when written, and otherwise `true` for a rule with `approve: request` once any device is enrolled (`needsSignature`). Adding and removing devices is always signed. An allow without a valid signature is refused with `ErrApprovalNeedsSignature` (`403`); a deny never needs one. The TUI and Attention cannot allow a signed approval, and say to use the phone.
- **Devices** (`internal/service/approval_devices.go`, migration 072: `approval_devices`, `approval_challenges`, `instance_values`). A phone asks `POST /api/devices/challenge`, makes a key with that challenge and sends `POST /api/devices {name, kind: "android", chain, challenge}`. `internal/engine/attest` checks the chain: Google's roots, the revocation list (kept a day; a chain is refused while none was ever read), the challenge, a hardware security level, EC P-256 signing, authentication for each use, and the app (`dev.legato.mobile`, signed with one of `approval_devices.android_app_digests`; the default is the public debug key's, so set the digest of a private release key, see `mobile/README.md` § Building). The first device is enrolled at once. A later one is `pending` and raises an approval of source `devices`, which an enrolled device signs. `DELETE /api/devices/{id}` raises one too. `GET /api/devices` lists them; Settings › Devices in the web UI shows them.
- **Signing.** `POST /api/approvals/{id}/challenge {device}` returns the request in RFC 8785 form (`canonical`, base64: what Legato knows of it, with this server's ID) and a 32-byte challenge for that approval and device, good for 5 minutes and once. The device signs `{"challenge","device","option","request","v":1}` (ECDSA P-256, SHA-256, DER) and sends it in the decision as `signature: {device, challenge, value}`. `VerifyDecision` spends the challenge whatever the outcome, checks the request is unchanged and verifies the signature. The approval records `decided_device`, and the credential audit names the device. A decision records how the device knew it was the person in `decision.verification` (`ApprovalDecision.Verification`, from `SignedBy`): `verified` for a phone and for a passkey assertion with the UV flag (a PIN, a fingerprint, an unlocked password manager), `presence` for one with only the UP flag (a touch).
- **Verification enforced.** A request submitted with `ApprovalInput.Verified` (stored as `approval_requests.verified`, migration 100; it implies `signed`) refuses an allow whose verification is not `verified` with `ErrApprovalNeedsVerification`: a touch alone does not do. A denial needs nothing. The credential audit's grant and deny rows record the decision's `verification` (`credential_audit.verification`).
- **Passkeys.** A desktop signs with a passkey (`internal/service/approval_passkeys.go`, WebAuthn through `go-webauthn`; `web/src/lib/passkeys.ts`). Settings › Devices **Add a passkey for this computer** asks `POST /api/devices/passkey/options` (the page's origin from its `Origin` header) and sends the new credential to `POST /api/devices` as `{kind: "passkey", name, credential, origin}`; user verification is preferred, not required (a security key may sign with a touch alone; presence is always required), and attestation is not asked for, so a password manager's synced passkey (1Password) works, listed as `synced passkey`. A passkey belongs to the page it was made on, not to the server that checks it: the server keeps that page's `origin` and its host name as the relying party, and accepts assertions only from them, so a page of one Legato can enroll a passkey on another, headless one (each server gets its own entry under that page's host name). It needs a secure page at a host name (`localhost`, or HTTPS), never an IP address. A later passkey is a signed approval that names its origin. To decide, the page gets `passkey: {credential_id, rp_id, origin}` with the challenge, asks the passkey to sign the SHA-256 of the same message a phone signs, and sends the assertion's JSON as the signature's `value`; the server checks it and keeps the credential's counter and flags. The tray offers **◈ <option>** buttons on a page with an active passkey of its own (`usePasskeyDevice`). The [desktop app](desktop.md) is its own WebAuthn client: it enrolls one security-key credential per key, made for its own origin (`https://legato-desktop.invalid`), on every server, and signs with a touch. A server's page in the app hands signed approvals to its sheet as the Android app's does (`legatoShell`; see [Desktop app](desktop.md) § The bridge between a page and the shell). On a Mac it is also an authenticator itself: a Secure Enclave key that signs after a fingerprint, enrolled the same way as a `passkey` (not backed up) and recorded `verified`.
- **Attestation records.** A phone keeps the chain it enrolled with (migration 073, `attestation`); `legato device attestation <id>` prints it as PEM.
- **Lost phone.** `legato device list` lists the devices; `legato device reset` at the host removes them all, writes the audit log, sends an ntfy notice and records the reset (`instance_values` key `devices_reset`, with a sequence number). Attention shows the last reset (producer `devices`, signal `devices_reset`, high urgency, opening `/settings#devices`) until it is dismissed; enrolling a phone afterwards does not hide it, since that phone may be the one the reset was for, and the next reset is a new revision. The command sends `approval_changed` over IPC so a running Legato shows it at once. 
- **The phone's side** is in `mobile/README.md` § Signed approvals.

### The Reeve's change sets

Source `reeve_change_set`: the calls the Reeve proposed together for one step, at the level of the strictest (`signed`, `verified` set to match), every call in the details. Its resolver queues the set; the broker's process runs it. A session's pending ones expire when it ends. See [The Reeve](reeve.md) § Change sets. The tray shows it as `ChangeSetCard`: each change with its kind, route and before-and-after.

### The Reeve's secrets

Source `reeve_secret`: the Reeve asks for a secret by name. The tray's form stores the value through `POST /api/reeve/secrets/{id}`; the approval never holds it. See [The Reeve](reeve.md) § Restart, config edits and secrets as tools.

### Rules of the Reeve

Source `reeve_rules`: a change to one of the Reeve's own rules (`reeve.levels.*`, `reeve.rules`), always `signed`; its resolver applies the change once allowed. See [The Reeve](reeve.md) § Levels and rules.

### The Reeve's seed

Source `reeve_seed`: a new seed for the Reeve's engram, one of its own rules, always `signed`, refused with no device enrolled. The details hold the engram and the whole seed, which the tray shows as the summary; its resolver writes the seed into the engram once allowed. See [The Reeve](reeve.md) § Its engram.

## API

Behind the web token:

- `GET /api/approvals?status=pending|all&task=<id>` → `{approvals: [...]}`, oldest first (`pending` by default).
- `GET /api/approvals/{id}`.
- `POST /api/approvals/{id}/decision` with `{decision: "allow"|"deny"|"answer", option?, feedback?, answers?, by?}`: `200` with the approval, `409` when it is no longer pending, `400` for an option it does not have or answers to questions it does not ask, `502` when the agent refused it.
- `POST /api/approvals/{id}/dismiss` with `{note?, by?}`.
- `approvals_changed` on the WebSocket (`item_id`, `agent_id`, `kind`), and `approvals` in `/api/navigation-summary`.

## Wiring

`startApprovals` in `cmd/legato/approval_runtime.go` builds the service and the watcher for both runtimes; `attach` gives a web server the API, the WebSocket forwarder and the watcher's events; `registerApprovalAttention` adds the Attention producer (used by `wireAttention` and the TUI's own Attention instance).
