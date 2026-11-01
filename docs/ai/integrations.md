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

`legato integration token create argus --scope owner:read,delegate,attention` prints a token once (`lgi_` and 52 base32 characters). The database keeps only its SHA-256 (`integration_tokens`). `token list` shows names, scopes and dates; `token revoke <name>` stops it at once. A name is unique among tokens that are not revoked.

The auth middleware (`internal/server/integrations.go`) lets a bearer token starting `lgi_` reach `/api/integrations/*` only:

- an unknown or revoked token gets 401 `unauthorized`;
- any other path gets 403 `forbidden`;
- a route whose scope it lacks gets 403 `missing_scope`.

The owner route needs `owner:read`, the attention routes `attention`, and everything else `delegate`. The WebSocket never takes one. The full web token works on the integration routes too, and acts as Argus there. A request made with a scoped token carries its integration's name in its context (`integrationFrom`).

Integration routes answer errors as `{"error": "<code>", "message": "<text>"}`. The other routes keep `{"error": "<message>"}`.

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
- **Credentials:** the execution is marked read-only (`executions.read_only`), and the credential broker refuses its `git.push*`, `git.delete-ref` and `pr.*` operations whatever the rules say (`readOnlyRefusal`).
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

It only shows: tokens are made and revoked with `legato integration token`, and the connection with `legato integration connect argus`.

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
