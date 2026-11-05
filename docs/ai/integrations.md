# Integrations (Argus)

Services outside Legato can look up who owns work, hand that work to a task's agent, hear back from the agent, and show their cases in Attention. Argus, the person's operations service, is the first. It opens cases when CI fails and delegates them to the agent that owns the pull request. The contract was agreed with Argus's side. Argus documents it as `design.md` § D12 and § D19 in its own repository. Legato's changes are `openspec/changes/argus-integration` and `openspec/changes/argus-attention`.

```
Argus ──(lgi_ token)──► /api/integrations/owner              who owns acme/widget#12?
      ──(lgi_ token)──► /api/integrations/delegations        here is a brief
      ──(lgi_ token)──► /api/integrations/attention/{key}    this case needs Cameron
                              │ message / resume / start
                              ▼
                        the task's agent ──reply_case──► outbox ──(OAuth, argus:reply)──► Argus /api/cases/{id}/replies
```

## Scoped tokens

`legato integration token create argus --scope owner:read,delegate,attention` prints a token once (`lgi_` and 52 base32 characters). The database keeps only its SHA-256 (`integration_tokens`). `token list` shows names, scopes and dates; `token revoke <name>` stops it at once. A token has a kind (migration 083): `argus` for the tokens the CLI makes, `main` for a paired main instance's (see [Main instances and workers](#main-instances-and-workers)). A name is unique among live tokens of its kind, so a main named `argus` does not collide with Argus's token; `token list`, `token revoke` and Settings › Integrations deal with `argus` tokens only.

The auth middleware (`internal/server/integrations.go`) lets a bearer token starting `lgi_` reach `/api/integrations/*` only:

- an unknown or revoked token gets 401 `unauthorized`;
- any other path gets 403 `forbidden`;
- a route whose scope it lacks gets 403 `missing_scope`.

The scope a route needs comes from the first segment of its path after `/api/integrations/` (`integrationScopes`): `owner` needs `owner:read`, `delegations` needs `delegate`, `attention` needs `attention`, and `handoff` needs `handoff`. A path in no group, such as `/api/integrations/ownership` or a new route nobody mapped, gets 403 `forbidden` whatever scopes the token holds: a new group must be given a scope before a scoped token can reach it.

`handoff` (`service.ScopeHandoff`) is for the routes a main instance uses to hand tasks to a worker instance. Only pairing a main gives it, alone (`IntegrationTokenService.CreateMain`): `token create` refuses it, and the CLI does not offer it. The WebSocket never takes a scoped token. The full web token works on the integration routes too, and acts as Argus there, but it is no main: handoff handlers refuse it (below). A request made with a scoped token carries its identity in its context (`integrationFrom`): kind, name, instance ID and scopes.

Integration routes answer errors as `{"error": "<code>", "message": "<text>"}`. The other routes keep `{"error": "<message>"}`.

## Main instances and workers

A main instance pairs with a worker instance by phrase, as a phone does, and gets a token that can only hand off ([GLOSSARY](../../GLOSSARY.md) § Instances and handoff). The spec is Burnlite LEG-15; this section covers pairing (LEG-18). Every Legato can be both.

**On the worker.** The person opens a pairing request for a main: Settings › Handoff › Main instances › **Pair a main instance**, `legato pair --main`, or `POST /api/pairing {"purpose":"main"}`. It differs from a phone's request (see [Web UI](web-ui.md) § Pairing by phrase) in four ways:

- **Its own SPAKE2 identity.** The main is party A as `legato-main` (`pairing.MainProtocol`), so a phone cannot complete a main's request, nor a main a phone's: each gets 403 and uses an attempt. The phone's identities, labels, AAD and `testdata/exchange.json` are unchanged.
- **The main must say who it is.** Its sealed field (the `phone` field of `/api/pair/confirm`, sealed under `pairing.LabelMain`) is `{"name", "instance_id"}`. A field that is missing or does not open, a name with nothing left after normalising, or an instance ID that is not `srv-` and 16 hex digits fails the confirm with 400 (`ErrPairingClient`). The name is lowercased and every run of other characters becomes one dash, at most 40 (`service.NormaliseMainName`).
- **It mints a token.** `service.PairingSecrets.Secret` is the pairing service's secret callback in both runtimes; for a main it calls `CreateMain(name, instanceID)`: an `lgi_` token of kind `main` holding `handoff` alone, carrying the main's instance ID. Re-pairing the same instance ID revokes its old token and stores the new one in one transaction (`store.ReplaceMainIntegrationToken`), so a lost confirmation or a renamed main just pairs again. Another live main with the same name is 409 (`ErrMainNameTaken`): unpair it first. The sealed secret is `{token, name, ca_certificate, instance_id}`, the last being the worker's instance ID (a phone's secret never has one).
- **No QR code.** The request's view has `purpose: "main"`, no `pair_uri`, and `address`: the address the browser used, else the first LAN address, for the person to type on the main.

A main is known by its instance ID, never its name. Settings › Main instances lists the live `main` tokens (`GET /api/settings/mains` → `{mains: [{name, instance_id, paired_at, last_pull_at?, open_handoffs}]}`, `last_pull_at` being the token's `last_used_at`; `open_handoffs` counts the main's handoffs not closed, `store.CountOpenHandoffsByMain`), and **Unpair** revokes one (`DELETE /api/settings/mains/{instance_id}`, 404 when not paired). `/api/meta` advertises `handoff_pairing: 1`, `handoff` (the protocol version, `service.HandoffProtocolVersion`: 2 since the Handoff call) and `sandbox` (whether agents can run in the sandbox here) when the server pairs mains.

**Handoff routes.** They live under `/api/integrations/handoff/` and act only for a paired main: a handler calls `requireHandoffMain(w, r)` (`internal/server/instance_pairing.go`), which returns the scoped identity (kind `main`, scope `handoff`, its `InstanceID`) or answers 403 `not_a_main` itself, for the full web token too. Handlers then act only on that main's records and answer `not_found` for another's. The first route is `GET /api/integrations/handoff/pairing` → `{worker_instance_id, main_instance_id, main_name, handoff}`: how the worker sees the pairing.

**On the main.** Settings › Handoff › Workers › **Add a worker** takes the worker's address (https assumed without a scheme), the four words and this main's name on the worker (`POST /api/settings/workers {address, phrase, name}`; `GET` lists `{workers, instance_id, main_name}`, the last being the suggested name). `service.WorkerService.Pair`:

1. reads the worker's `/api/meta` and refuses one without `handoff_pairing` or with an older `handoff` (`ErrWorkerTooOld`, "update the worker", 409);
2. runs SPAKE2 as `legato-main` with its normalised name and instance ID sealed; until then it does not check the worker's certificate, since the phrase authenticates the exchange and the certificate to trust arrives sealed;
3. refuses a secret whose token is not `lgi_` (an old or wrong worker sending its full web token) or that has no instance ID (`ErrWorkerRefused`, 409);
4. calls the pairing route with the new token over the worker's certificate and checks the worker's instance ID;
5. keeps the token sealed in Legato's own secret store as `legato://handoff-worker-<worker instance ID>` (see [Credentials](credentials.md)), and the worker in `workers` (instance ID, address, name, this main's name there, CA, the secret's name, sandbox, protocol version, paired at, last pull). Pairing the same worker again replaces its record.

A wrong phrase or a closed request is `ErrWorkerRefused` with the worker's words; no answer is `ErrWorkerUnreachable` (502). `WorkerService.Client(ctx, instanceID)` is the client of a paired worker for later calls: `Call(ctx, method, path, body, into)` sends the main's token over the pinned certificate, and `CheckPairing` checks the worker still accepts this main and is the same instance.

**Testing.** `internal/handofftest` runs a main and a worker in one Go test: each its own SQLite and real services, the worker's web server on an `httptest` TLS server (its certificate is the CA the main pins, the same at each restart), agents started in a `FakeTmux`, one injectable `Clock`. `StartWorker`, `NewMain`, `Pair(t, main, worker)`; `worker.Stop()` and `worker.Restart()` (same database, address and certificate); `WorkerOptions.Wire` gives the worker's server more routes before it serves. Its package comment shows how to use it.

## Handing off a task

A paired main hands one of its tasks to a worker, which makes a card for it and starts a briefed agent on it at once; the main keeps the task **away** and read-only. Spec LEG-15 § The protocol, § Accepting and starting on the worker, § The brief; this section covers the Handoff call (LEG-19): a local task, starting from a new branch. Pull and Events (LEG-24), return and recall (LEG-25), push grants (LEG-21), a pushed branch (LEG-22) and Jira-backed tasks (LEG-27) build on it.

**On the main.** `service.TaskHandoffService.HandOff(ctx, TaskHandoffRequest{TaskID, Worker, Purpose, Kind, Repo, AgentKind})` (no HTTP route yet; the web UI comes with LEG-20). Purpose is `make-change` or `find-out`; kind `new-branch` (the default; `pushed-branch` is refused for now); repository `owner/name`; an empty agent tool means the worker's default. Before anything is sent it refuses (`ErrHandoffInput`) an archived, ephemeral or provider-backed task and a bad purpose or repository, refuses a task already away (`ErrTaskAway`) or whose agent runs here (`ErrHandoffAgentRunning`, "stop it first"), and reads the worker's `/api/meta` over its certificate without the token (`WorkerClient.Meta`): no answer is `ErrWorkerUnreachable`, a `handoff` below `HandoffProtocolVersion` is `ErrWorkerTooOld` ("update the worker"), no sandbox is `ErrWorkerRefused`. Then it records the handoff in `away_tasks` as `sending` with a fresh handoff ID (`ho-` and 16 hex), sends the call, and on the worker's answer marks it `away` with the worker's task ID. A refusal deletes the row; no answer leaves it `sending`, which is not away, and the next handoff of that task to the same worker reuses its ID, so a worker that did get the first call answers with the card it made. `Away(ctx, taskID)` and `ListAway(ctx)` read them; the board's cards carry `Away` (`away: {worker_instance_id, worker, handoff_id}` in `GET /api/board`).

**Away is read-only.** `service.RefuseAway(ctx, db, taskID)` is a `*TaskAwayError` (`errors.Is(err, ErrTaskAway)`; "task X is away on vm-1: it can change only there until it comes back") for a task with an `away` handoff. It is asked where every surface changes a task: the board service's title, description, workspace, metadata, move (`moveCard`, so the sync service's `PushMove` too), reorder, delete and archive (`ArchiveDoneCards` and `store.ArchiveTasksByStatus` leave away tasks); the agent service's `SpawnAgent`; the CLI's `TaskUpdateFields` (`legato task update`, which agents reach through the broker; the other `legato task` setters go through the board service); and `HandOff`. The web API answers it 409 (`taskErrorStatus`), and `POST /api/tasks/{id}/agent` asks the board's `RefuseAway` before it starts a clone in the background. Notes can still be added: they stay on the main.

**The call.** `POST /api/integrations/handoff/tasks` (`service.HandoffCallPath`) with `service.HandoffCall`: `{handoff_id, main_task_id, title, details, jira?, notes: [{id, body, created_at}], repo, purpose, kind, branch, agent_kind}`, `branch` being `legato/<main task id, lowercased>`. It answers `{handoff_id, worker_task_id, worker_instance_id, state, failure?}`: 201 for a new card, 200 for the same handoff ID with the same body (the request hash), 409 `idempotency_conflict` for the same ID with another body. `GET /api/integrations/handoff/tasks/{handoff_id}` is one handoff as its main sees it; another main's is 404 `not_found`. Both need a main's handoff identity (`requireHandoffMain`). Refusals are 400 `bad_request`, or 422 with `no_sandbox`, `unknown_agent` (a tool the worker does not know, or cannot start with its prompts off; never a fallback), `cannot_clone` (`CloneChecker.CloneProblem`: the repository service, which needs a GitHub credential that can read the repository) or `unsupported` (a pushed branch or a Jira-backed task, for now); none makes anything.

**On the worker.** `service.HandoffWorkerService` (`internal/service/task_handoff_worker.go`; wired by `wireTaskHandoffs` in `cmd/legato/task_handoff_runtime.go` for both web servers). `Accept` makes, in one transaction (`store.CreateHandoffCard`), an ordinary card in the main's workspace (`store.EnsureMainWorkspace`: made on demand, named after the main, or with the end of its instance ID when the name is taken, and found by the instance ID afterwards, `main_workspaces`), in the column named Doing or In Progress, else the first; the main's notes as `task_notes` with `source` `main` and `remote_id` the main's note ID; and the handoff record, `pending`. Then it starts the agent in the background (`start`): it claims the start (`store.ClaimHandoffStart`, so two Legatos on one database do not both start it; a claim older than 15 minutes is stale), composes the brief, and calls `SpawnAgent` with the repository, the branch, the agent tool, the brief as `Assignment` and `Unattended` (below). Success makes the handoff `in_progress`; an error makes it `failed` with the error as its reason (a clone that failed, the sandbox missing). `ResumePending` starts the pending handoffs no Legato is starting; the process that serves the broker runs it at start and every minute. `Reconcile` makes an `in_progress` handoff `done` while its card is in a column named Done, and `in_progress` again when it leaves; `StartEventLoop` runs it on every card event and every 30 seconds. State changes publish `EventHandoffChanged` (payload: the worker's task ID). The board's cards carry `HandedOffFrom` (`handoff_from: {main, main_task_id}` in `GET /api/board`), which the web card shows as "from workstation · K7Q2M9XA".

**The agent.** `AgentSpawnOptions.Unattended` runs it in Legato's sandbox whatever `Unsandboxed` says, refused when the sandbox is off, with its tool's permission prompts off: an adapter that is a `service.PromptsOffAdapter` adds `PromptsOffArgs()` to its launch command (Claude Code `--dangerously-skip-permissions`; Codex `-c approval_policy="never" -c sandbox_mode="danger-full-access"`, configuration its native bridge also gives the app server; Chimera `--auto-approve all`). A tool that is not one cannot take a handoff. The brief (`service.HandoffBrief`, `task_handoff_brief.go`) is the text settled in LEG-7: work alone, decide and note the decision, no progress notes; for make-a-change, work on the branch, stack on `legato/<main task id>-<a few words>`, draft PRs only; for find-out, answer in notes and push nothing; finish with an outcome note and move the card to Done (`legato task update --status Done`), then stay. Its golden files are `internal/service/testdata/handoff_brief/`; `go test ./internal/service -run TestTheHandoffBrief -update` rewrites them. The brief says pushing is already allowed: the grants that make it so are LEG-21's.

**Testing.** The harness's worker now has a `HandoffWorkerService` (`worker.Handoffs`), a fake repository cache (`worker.Repos`: `Refuse(repo, why)`, `Fail(repo, err)`, `Clones()`; clones are empty folders), a fake sandbox backend (launch lines start `SANDBOX`), and two fake agent tools, `claude-code` (the default) and `codex`, whose launch command is their name and whose prompts-off argument is `--<name>-prompts-off`. `worker.Settle()` waits for agent starts, `worker.HoldStarts(true)` accepts without starting (a worker that stopped right after accepting), and `WorkerOptions.Handler` wraps the worker's server to stand in for an old worker or a lost answer. The main has `main.Handoffs`. See `internal/handofftest/handoff_test.go`.

## Owner lookup

`GET /api/integrations/owner?repo=acme/widget&pr=12&branch=feat-x&sha=0123abc` (`service.OwnerLookupService`). `repo` is required with one of the others. The lookup covers tasks that are not archived and whose code context or linked PR names the repository (`store.ListTasksForRepository`, case-insensitive). It matches by PR number, then by head SHA (a prefix of 7 or more characters), then by branch. Within a rule, tasks with a running agent come first, then the most recently updated.

The answer is `{matches: [{task: {id, title, status, url?}, agents: [{id, kind, state}], worktree?: {branch}, pr?: {number, url, state}, match}]}`, or 404 `not_found`.

- **Agent states:** `working` (`background` counts as working), `waiting` (needs the person), `idle` and `error` for a running session; `stopped` for a saved conversation with no running session. A task with neither has `agents: []`.
- **`id`** is the execution ID.
- **`url`** needs `web.public_url`.

Working directories, descriptions and notes are never in the answer.

## Delegations

`service.DelegationService`, `POST /api/integrations/delegations`:

```json
{"caseId": "ci:acme/widget#12", "mode": "fix" | "find_out",
 "target": {"task": "a1b2c3d4"} | {"repo": "acme/widget", "pr": 12, "branch": "feat-x"} | {"repo": "acme/widget"},
 "brief": "<markdown>", "title": "Argus: …", "idempotencyKey": "argus-<uuid>"}
```

Limits: `caseId`, `title` and `idempotencyKey` at most 200 characters, `brief` 64 KiB, a body of at most 128 KiB. The answer is 201 `{delegationId, route, taskId, state}` for a new delegation, and 200 with the same delegation for a repeat. A key is scoped to the integration. The table keeps a hash of the request, so the same key with a different body is 409 `idempotency_conflict`.

**Routes.** The route is decided before answering, and a target that cannot be routed is refused, with nothing stored:

| Target | Route |
|---|---|
| `{task}` with a running agent, in any state | `message`: through `QueueMessage`, as `legato agent message` |
| `{task}` stopped with a saved conversation | `resume`: a spawn with `session_policy: resume`, the brief as its first message |
| `{task}` with neither | `start`: the tool of `integrations.argus.agent` in the task's worktree or code-context working directory; 422 `no_checkout` without one |
| `{task}` missing or archived | 404 `unknown_task` |
| `{task}` whose stopped agent a swarm or workflow ran | 422 `not_resumable`, no fallback to `start` |
| `{repo, pr}` that the owner lookup finds | as `{task}` for the best match |
| `{repo, pr, branch}` nobody owns, with Yggdrasil worktrees | a new task, a worktree on `branch`, `start` there |
| `{repo, pr}` nobody owns, no worktree possible | `find_out`: a new task, `start` in `primary_dir`; `fix`: 422 `no_checkout` |
| `{repo}` | a new task, `start` in `primary_dir` |
| a repository not in `repositories` with a `primary_dir` | 422 `unknown_repo` |

A new task is local, titled from `title` (default `Argus: <caseId>`), in the column named Doing or In Progress (else the first), in the first workspace its catalog entry names. Its code context records the repository, the branch and the working directory (`repo_source: integration`).

Legato does not read a PR's head branch from GitHub: a `{repo, pr}` target without `branch` gets no worktree. Argus sends `branch` when the case has one.

**Delivery** runs in the background and checks the route again first, since the agent may have stopped or started in the meantime. The agent receives:

```
[argus case <caseId> · delegation <dl-id> · fix|find out]

<brief, unchanged>

---
From Argus, not from the person. When you are done, have found something, have a question, or cannot do it, call the reply_case tool with delegation_id <dl-id>.
```

A delegation is `delivered` when `QueueMessage` returns, or when the spawn that carries the brief as its first message returns. A failure sets `failed` with a code (`spawn_failed`, `message_failed`, `no_checkout`, `not_resumable`, `unknown_task`) and raises Attention `delegation_failed`. Every change publishes `EventDelegationChanged`.

The process serving the broker takes over delegations left `pending` for more than 5 minutes (`ResumeStale`, every minute): ones whose Legato stopped before delivering.

**Find out, enforced.** For `find_out`, a `resume` or `start` spawns with `AgentSpawnOptions.ReadOnlyCheckout`:

- **Sandbox:** the agent runs in the sandbox even if asked not to (when `agents.sandbox` is configured), and `BuildSandboxPlan` mounts the working directory and its git folder read-only. The tool's state, caches and `/tmp` stay writable.
- **Credentials:** the execution is marked read-only (`executions.read_only`), and the credential broker refuses its `git.push*`, `git.delete-ref`, `pr.*` and `issue.*` operations whatever the rules say (`readOnlyRefusal`).
- **`enforced`** is `true` only when the started agent's session is sandboxed.

A message to a running agent changes nothing it can do: the delegation records `enforced: false`.

**Asking first.** With `integrations.argus.delivery: ask`, a delegation raises an approval of source `delegation` (Deliver / Refuse) and stays `awaiting_approval` with no timeout. Deliver routes again and delivers; Refuse sets `failed` with `denied`, which does not reach Attention since the person chose it. The default is `auto`, because Argus's own `delegate` policy is `propose`: the person already accepts each delegation in Argus.

**Status:** `GET /api/integrations/delegations/{id}` returns `{delegationId, state, route, taskId, agentId?, enforced, error?: {code, message}, createdAt, deliveredAt?}`. Another integration's delegation is 404.

**Follow-up messages:** `POST /api/integrations/delegations/{id}/messages {text, idempotencyKey}` delivers to the same task by `message`, or `resume` if it stopped, tagged `[argus case … · delegation … · message]`.

- **201** `{messageId, state}`; **200** for a repeated key.
- **409** `not_delivered` before delivery, **409** `closed` after a failure.
- A message whose agent ended without a saved conversation is answered with `state: failed`: a new agent would not know what it answers.

Argus uses these to answer an agent's `question`.

## Replies

**The tool.** `reply_case(delegation_id, outcome, text)` is a tool of `legato mcp serve` (`internal/cli/mcp_reply.go`).

- **Listed** in every session once `integrations.argus` is set: tools are listed when a session starts, and a delegation can reach an agent that is already running.
- **Outcomes:** `done`, `finding`, `question`, `cannot`. `text` is at most 16 KiB.
- **Checks:** `service.CaseReplyService.Reply` refuses a delegation that is not `delivered` to the caller's task. The execution may differ, for a resumed agent.
- **Sending:** the reply goes into `case_reply_outbox`, and the host tries it at once for up to 10 seconds. The tool answers sent, queued, or Argus's refusal.

**Sending.** Only the process that serves the broker sends, every 15 seconds (`whileBrokerOwner`). A `legato mcp serve` that reaches no broker only records its replies.

Each send is `POST <url>/api/cases/{caseId}/replies` with `{delegationId, outcome, text, agent: {taskId, agentId}}`, the bearer access token, and `Idempotency-Key: <outbox id>`. What happens next depends on the answer:

- **2xx:** sent. Argus answers 202, or 200 for a repeated key.
- **401:** refresh the token once and try again.
- **401 after a fresh token, or 403:** the connection is broken, not the reply. The integration becomes `disconnected` and the reply waits.
- **Any other 4xx** (`not_this_case`, `closed`, 404): failed for good.
- **5xx, network errors, timeouts:** tried again after 30 seconds, doubling up to an hour.

A reply not taken within 24 hours fails and raises Attention `case_reply_failed`.

**Tokens.** Legato is a public OAuth client of Argus (`internal/engine/oauthclient`).

- **Access token:** kept in memory and refreshed when it has less than a minute left.
- **Refresh token:** in Legato's sealed store as `legato://integration-argus-refresh` (type `oauth-refresh`). Argus rotates it on every use, so the new one is stored before the new access token is used.
- **`invalid_grant`:** the store is read again, in case another process rotated the token, and retried once. Otherwise the integration becomes `disconnected`, Attention raises `integration_disconnected`, and replies wait.

## Connecting

`legato integration connect argus [url] [--no-browser]`. The URL defaults to `integrations.argus.url`.

1. **Discovery:** `/.well-known/oauth-protected-resource`, then `/.well-known/oauth-authorization-server`. `resource` is taken from the first, exactly as given (`<origin>/mcp` for Argus).
2. **Registration** as a public client with the redirect `http://127.0.0.1/callback`. Argus matches a loopback redirect without its port.
3. **Consent:** the authorize address goes to the browser, with PKCE, a random `state`, `scope=argus:reply` and `resource`. The command waits up to 10 minutes on a loopback listener at a free port. On a host without a browser the redirect fails in a browser elsewhere; pasting the address it tried into the command finishes the flow.
4. **Storage:** the refresh token is sealed. `integrations` holds the issuer, token endpoint, resource and client ID.

The command refuses when the store's master key is unlocked by a passphrase: the refresh token is rewritten by a Legato nobody is watching.

`legato integration status argus` shows the connection and the outbox counts. `legato integration disconnect argus` forgets both.

The connection is not Argus's alone. `IntegrationConnector.BeginWith` takes the integration's name, its scope and where the browser comes back to: `""` is the loopback listener above, any other address is one of Legato's web server's (`StartRedirectAuthorization`, no listener; the server's callback finds the connection by its `State` and hands it the address with `Paste`). Each integration's refresh token is `legato://integration-<name>-refresh` (`IntegrationRefreshSecret`). Legato registers as a public client (`token_endpoint_auth_method: none`), but a server may give it a `client_secret` anyway and refuse token requests without it, as Burnlite does: the secret is then kept as `legato://integration-<name>-client-secret` (`IntegrationClientSecret`) and sent in the body of every code exchange and refresh (`oauthclient.WithClientSecret`, `client_secret_post`); a registration without one removes an old one. `OAuthTokens` gives any connected integration's access token with the refresh, rotation and `invalid_grant` handling above. Burnlite, a ticket provider, connects this way; see [Sync](sync.md) § Burnlite.

## Attention

Producer `integrations` (`service.IntegrationAttentionAdapter`, reconciled on `EventDelegationChanged`), registered when `integrations.argus` is set. It covers the last 7 days:

- `delegation_failed`: a delegation that did not reach its agent, opening the task;
- `case_reply_failed`: a reply Argus refused or never took, opening the task;
- `integration_disconnected`: Argus refused Legato's tokens, urgency high, saying to connect again.

## Cases in Attention

Argus pushes one Attention item per open case (`service.CaseAttentionService`, table `integration_attention`). Only the integration named `argus` may push; other tokens get 403 `forbidden`.

- **`PUT /api/integrations/attention/{key}`** takes `{revision, signal, title, reason, detail, urgency, target: {repo, pr, branch, sha}, link}`.
  - The key is `case-` and up to 96 letters, digits and dashes.
  - `signal` is `argus_case` or `argus_needs_you`; `urgency` is `low`, `normal` or `high`.
  - `link` must be on the origin of `integrations.argus.url` (https, or http to loopback), else 422 `invalid_link`.
  - Title (200 characters), reason (its first line, 300 characters) and detail (4 KiB) are cut with `…`, never refused.
  - It answers `{itemId, taskId, status}`; `status` is the Attention item's `open`, `dismissed` or `resolved`, and `itemId` is absent until the item exists.
- **`POST …/attention/{key}/resolve`** `{revision, reason}` closes it and records the revision; an unknown key is 404 `not_found`. An upsert at that same revision then changes nothing, so a late retry cannot reopen a closed case. Another revision reopens it.
- **`POST …/attention/sync`** `{keys}` (at most 10,000; body up to 512 KiB) resolves every open item not listed, at its current revision, and answers `{resolved: n}`. One invalid key refuses the call. Argus re-upserts its open cases and then syncs, at startup and hourly.

The producer `argus` (category `argus`) shows the open rows. At each reconcile it runs owner lookup on the target and puts the item on the best task and its workspace, so an item attaches when a task later takes on the PR. With no target or no match, the item has no task. It reconciles after every push, on `EventPRStatusUpdated` and `EventCardsRefreshed`, and at startup. It is registered without `integrations.argus` too, and then shows nothing, so removing Argus resolves its items. Revoking the token does not.

Its navigation is `{kind: "external", target: link}`, and it has no actions: proposals are accepted in Argus. The web dashboard puts `argus_case` in Merge blockers and `argus_needs_you` in Needs intervention, with "Open in Argus" (a new tab) and, for an item on a task, "Open task". The TUI's Enter opens the link in the browser.

While an open case targets a tracked PR (same repository and number), the `pull_requests` producer leaves "CI failing" out of that PR's item, and raises none when that was its only reason. Its revision gains `:argus`. When the case closes, the PR's own item comes back.

## Task activity

`GET /api/tasks/{id}/activity` has `delegations`: each with its case, mode, route, state, `enforced`, error, brief and the agent's `replies` (outcome, text, state). The web UI does not show them yet.

## In the web UI's settings

The Integrations group of Settings (`/settings#integrations`, where the `integration_disconnected` Attention item leads) shows Argus and the tokens. It reads `GET /api/settings/integrations` (`service.IntegrationOverview`), which only the full web token reaches: it is outside `/api/integrations/`. It answers:
- `tokens`: name, scopes, created, last used and revoked, never a token or its hash;
- `argus`: `configured`, `url`, `delivery`, `agent`, the connection (`state` none, connected or disconnected, `connected_at`, replies by state) and `open_cases`.

It only shows Argus: tokens are made and revoked with `legato integration token`, and the connection with `legato integration connect argus`.

Burnlite (a ticket provider; see [Sync](sync.md) § Burnlite) is connected from here too. `burnlite` in the overview (`service.BurnliteSettings`, wired for both web servers in `wireIntegrations`) is `configured_url` (`burnlite.url`), `projects` (searched for import; empty is every one), `projects_from_config` and the `connection` (`state`, `url`, `connected_at`).

- `POST /api/settings/integrations/burnlite/connect` `{url}` begins a connection whose browser comes back to this server, `<scheme>://<host it was reached at>/oauth/callback`, and answers `{authorize_url}`, which the page opens. `400` for an address that is not one or where discovery fails, `409` when the secret store is unlocked by a passphrase.
- `GET /oauth/callback` takes no token: it finds the attempt by its `state` (kept in memory for 10 minutes, once), exchanges the code and redirects to `/settings?integration=burnlite&result=connected#integrations`, or `result=failed&error=<message>`.
- `PUT /api/settings/integrations/burnlite` `{projects}` saves the project keys (`409` when `burnlite.projects` is set, `400` for a key that is not one); `DELETE` disconnects.
- `GET /api/ticket-providers` lists the connected providers that have a site (`name`, `label`, `web_origin`, `key_pattern`), which the browser extension registers its import button on.

## Configuration

```yaml
integrations:
  argus:
    url: https://argus.lan   # https, or http to a loopback address
    delivery: auto           # auto | ask
    agent: claude-code       # claude-code | codex | chimera
```

Without the block, owner lookup and tokens still work. The delegation and attention routes answer 503 `unavailable`, `reply_case` is not offered, and nothing is sent.

Argus trusts Legato's self-signed CA through `NODE_EXTRA_CA_CERTS=<data_dir>/certs/ca.pem`, or reaches it through the person's proxy.

## Code

| Where | What |
|---|---|
| `internal/engine/store/integration_tokens.go`, `delegations.go`, `case_replies.go`, migration `076_integrations.sql` | Tokens, delegations and messages, the connection, the outbox, `executions.read_only` |
| `internal/engine/store/integration_attention.go`, migration `077_integration_attention.sql` | Argus's cases for Attention |
| `internal/engine/oauthclient` | Discovery, registration, PKCE with a loopback listener or a pasted address, exchange, refresh. Knows nothing of Legato |
| `internal/service/integration_tokens.go`, `owner_lookup.go`, `delegations.go`, `delegations_approval.go`, `case_replies.go`, `integration_connect.go`, `attention_integrations.go`, `case_attention.go` | The services; `attention_pr.go` leaves out what a case says |
| `internal/server/integrations.go` | Scoped-token middleware, the routes, error codes, the settings overview |
| `internal/service/integration_overview.go`, `web/src/components/IntegrationsSettings.tsx` | What Settings shows of integrations |
| `internal/cli/mcp_reply.go` | `reply_case` |
| `cmd/legato/integration_runtime.go`, `integration_contract.go` | Wiring in both runtimes, the commands |
| `internal/engine/pairing` (`MainProtocol`, `LabelMain`), `internal/service/pairing.go`, `workers.go`, `internal/engine/store/workers.go`, migration `083_handoff_pairing.sql` | Pairing a main with a worker: the main's identity, minting its token, the main's workers |
| `internal/server/instance_pairing.go`, `web/src/components/InstancePairingSettings.tsx`, `web/src/lib/workers.ts` | Settings › Handoff (Workers, Main instances), `requireHandoffMain`, the pairing route, `/api/meta`'s handoff fields |
| `cmd/legato/pairing_runtime.go`, `pair_phrase.go` | Wiring in both runtimes, `legato pair --main` |
| `internal/handofftest` | The two-instance test harness |
