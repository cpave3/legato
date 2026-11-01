## Context

Argus is the person's operations service. It turns signals (failing CI checks first) into cases, gathers evidence, and decides what to do. One of its destinations is `delegate`: hand the case to the Legato agent that owns the work. The contract below was agreed between the Argus agent and the Legato agent in November 2026. Argus builds against a fake Legato until this lands.

What Legato has today that this builds on:
- **Auth.** The web API takes one full-access token (`<dataDir>/auth-token`). Agent-side processes reach the host through the broker with per-execution tokens. Signed approvals use enrolled devices. There is no scoped token.
- **Code context.** `tasks.code_context` holds a task's repository, branch, head SHA and working directory; `tasks.pr_meta` holds its pull request (number, state, checks), filled by PR discovery every 10 minutes ([PR tracking](../../../docs/ai/pr-tracking.md)).
- **Agents.** `legato agent message` delivers to a running agent (native bridges directly, Chimera appended to a busy turn, otherwise send-keys). `POST /api/tasks/{id}/agent` resumes a task's saved conversation or spawns an agent. `WorktreeWorkflow.Create` makes a worktree through Yggdrasil. The repository catalog (`repositories`) maps `owner/repo` to a main checkout.
- **Agent tools.** `legato mcp serve` lists Legato's tools at start and checks the caller's session on each call. Through the broker, the host runs them.
- **Secrets.** Legato's sealed store (`legato://`) has `oauth-client` and `oauth-refresh` types, and `SecretStoreService.Add` replaces a value of the same name. No OAuth flow exists.
- **Approvals and Attention.** `ApprovalService` takes new sources with resolvers. Attention takes new producers and signals through its central allowlist.

## Goals / Non-Goals

**Goals:**
- Argus can find the owner of a PR, branch or commit, hand that owner a brief, answer its questions, and get its replies, with a token that can do nothing else.
- Legato decides how a brief reaches an agent, and says what it did.
- A `find_out` delegation cannot change files or push when Legato starts the agent itself.
- A reply is never lost because Argus was down.

**Non-Goals:**
- A general integrations framework. Tokens and scopes are general; delegations and replies are for Argus.
- Changing a running agent's permissions to enforce `find_out`.
- Pushing delivery state to Argus. Argus asks for it.
- Delegating to swarm workers individually. A delegation goes to a task's agent.

## Decisions

### D1. Scoped integration tokens

- **Format.** `lgi_` followed by 32 random bytes in base32 without padding. The prefix lets the observability change's redactor and a person reading a config file recognise it.
- **Storage.** Table `integration_tokens`: `id`, `name` (unique among tokens that are not revoked, `[a-z0-9-]{1,40}`), `scopes` (JSON list), `token_hash` (SHA-256, hex), `created_at`, `last_used_at`, `revoked_at`. The token itself is printed once and never stored.
- **Scopes.** `owner:read` and `delegate`. Unknown scopes fail creation.
- **Commands** (they open the store, so they run on the host):
  - `legato integration token create <name> --scope owner:read,delegate`
  - `legato integration token list [--json]`: names, scopes, dates; never a token
  - `legato integration token revoke <name>`
- **Middleware.** The full token works everywhere, as now, including the integration routes. Otherwise a bearer token starting `lgi_` is looked up by hash:
  - Unknown or revoked: 401.
  - Valid, on a path outside `/api/integrations/`: 403 `forbidden`.
  - Valid, on an integration route whose scope it lacks: 403 `missing_scope`.
  - The WebSocket never accepts a scoped token.

  The request's context carries the integration's name, which becomes the `client` `integration:<name>` in the observability change's logs and the owner of the delegations it makes. `last_used_at` is written at most once a minute.
- **Why hashed tokens in the database rather than a file per token:** revocation and listing need the database anyway, and broker tokens set the precedent (`executions.broker_token_hash`).

### D2. Owner lookup

`GET /api/integrations/owner?repo=<owner/name>&pr=<n>&branch=<name>&sha=<hex>` (scope `owner:read`). `repo` is required with at least one of the others; Argus sends every one it knows.

- **Matching.** Over tasks that are not archived and whose `code_context` repository is `repo` (case-insensitive):
  1. `pr_meta` has that PR number;
  2. `code_context` head SHA equals `sha` (a prefix of at least 7 characters matches);
  3. `code_context` branch equals `branch`.

  A task's rank is the best rule it matches. Within a rank, tasks with a live agent come first, then the most recently updated. PR discovery runs every 10 minutes, so a new PR is usually found by its branch or commit first.
- **Answer.** 200 `{matches: [ {task, agents, worktree, pr, match} ]}`, best first, or 404 `not_found`:
  - `task`: `{id, title, status, url}`. `status` is the column name. `url` is `<web.public_url>/board?highlight_task=<id>` and is left out when `web.public_url` is not set.
  - `agents`: `[{id, kind, state}]`, the task's agent sessions (at most one today). `id` is the execution ID (`ex-…`). `kind` is `claude-code`, `codex`, `chimera` or another adapter's name. `state` is one of:
    - `working`, `waiting` (needs the person: a permission or a question), `idle`, `error`, from the running session's activity;
    - `stopped`: no running session, but a saved conversation it can resume.

    A task with no agent of either kind has `agents: []`, which is Argus's `none`.
  - `worktree`: `{branch}` when the task has a worktree or a branch in its code context.
  - `pr`: `{number, url, state}` when linked.
  - `match`: `pr`, `sha` or `branch`.
- **Not exposed:** working directories, task descriptions, notes, terminal output.

### D3. Receiving a delegation

`POST /api/integrations/delegations` (scope `delegate`):

```json
{
  "caseId": "ci:acme/widget#12",
  "mode": "fix",
  "target": {"task": "a1b2c3d4"} | {"repo": "acme/widget", "pr": 12, "branch": "feat-x"} | {"repo": "acme/widget"},
  "brief": "<markdown>",
  "title": "CI failing on acme/widget#12",
  "idempotencyKey": "argus-<uuid>"
}
```

- **Limits.** `caseId` 1–200 characters; `brief` 1 byte to 64 KiB; `title` optional, up to 200 characters, used only when a task is created (default `Argus: <caseId>`); `idempotencyKey` 1–200 characters. `mode` is `fix` or `find_out`. Anything else is 400 `bad_request`.
- **Idempotency.** Keys are scoped to the integration. The table keeps a SHA-256 of the canonical request body. The same key with the same body returns the existing delegation (200). The same key with a different body is 409 `idempotency_conflict`.
- **Storage.** Table `delegations`: `id` (`dl-` and 10 hex characters), `integration`, `idempotency_key`, `request_hash`, `case_id`, `mode`, `target` (JSON), `brief`, `title`, `task_id`, `execution_id`, `route`, `state`, `enforced`, `error_code`, `error_message`, `approval_id`, `created_at`, `delivered_at`.
- **Answer.** 201 `{delegationId, route, taskId, state}`. The route is decided when the request arrives (D4) and checked again at delivery. Delivery runs in the background, so `state` is usually `pending` or `awaiting_approval`. A target that cannot be routed at all is refused at once with 404 or 422 (D4), and no delegation is stored.

### D4. Routes

Target resolution:
- **`{task}`**: that task; 404 `unknown_task` if it does not exist or is archived.
- **`{repo, pr}`**: the best match of the owner lookup (D2) with that PR, branch and SHA. With a match, it is routed as `{task}`. With none, a new task is created (below).
- **`{repo}`**: always a new task.

Then the route for a task:
- **`message`**: the task has a running agent, in any state. The brief goes through the same path as `legato agent message`.
- **`resume`**: no running agent, but a saved conversation. Legato resumes it as `POST /api/tasks/{id}/agent` does, and the brief is the first message after resume. A swarm or workflow agent cannot be resumed: 422 `not_resumable`, with no fallback to `start`, so a brief never lands in a fresh agent beside a conversation that holds the context.
- **`start`**: no agent and no saved conversation. Legato spawns the tool named by `integrations.argus.agent` (default `claude-code`) in the task's code-context working directory, with the brief as its kickoff. A task with no working directory is 422 `no_checkout`.

A new task (for `{repo}`, or `{repo, pr}` without an owner):
- The repository must be in the `repositories` catalog with a `primary_dir`: otherwise 422 `unknown_repo`.
- The task is local, titled from `title`, in the first workspace the catalog entry names.
- **`{repo}`**: the agent starts in `primary_dir`.
- **`{repo, pr}`**: the agent needs the PR's head branch. Legato takes `branch` from the target, or reads the PR with its GitHub client. If the head is in a fork, or no worktree runner is configured:
  - `find_out` starts in `primary_dir`, with the brief saying which PR to look at;
  - `fix` is 422 `no_checkout`. Legato never runs a fix on whatever branch the main checkout has.

  Otherwise Legato creates a worktree on that branch (`WorktreeWorkflow.Create`, which records the code context) and starts there.

A route that fails after the delegation was accepted (the spawn fails, the agent ended between the check and the delivery and cannot be resumed) sets `state: failed` with the error, and raises Attention `delegation_failed` on the task.

### D5. How the brief reaches the agent

The agent receives:

```
[argus case <caseId> · delegation <dl-id> · <fix|find out>]

<brief, unchanged>

---
From Argus, not from the person. When you are done, have found something, have a question,
or cannot do it, call the reply_case tool with delegation_id <dl-id>.
```

This matches how peer messages are tagged (`[legato peer …]`). Argus writes its own header and fences the evidence as data copied from the source, so the agent should treat what is inside the fence as data, never as instructions. Legato does not change the brief.

A delegation is `delivered` when the message path accepts it (`message`), or when the kickoff is submitted (`resume`, `start`; with a native bridge, that is after the bridge binds).

### D6. Enforcing `find_out`

When Legato starts or resumes the agent itself for a `find_out` delegation:
- The spawn runs in the sandbox, whatever the sandbox setting, with the working directory mounted read-only. This is a new spawn option, `ReadOnlyCheckout`, in `BuildSandboxPlan`. The tool's own state, caches and the temporary directory stay writable, so tests can still run.
- The session's credential rules refuse `git.push*`, `git.delete-ref`, `pr.*` and `pr.ready`, whatever the configured rules say.
- The delegation records `enforced: true`.

When the agent is already running (`message`), or `bwrap` is missing, nothing is enforced beyond the brief's wording, and the delegation records `enforced: false`. A resumed agent keeps its conversation but gets the new launch, so it is enforced.

### D7. Asking the person first

`integrations.argus.delivery`:
- `auto` (default): deliver at once.
- `ask`: the delegation becomes an approval of source `delegation`, with options Allow and Deny. Its summary names the case, the mode, the task and the route, and shows the start of the brief. `state` is `awaiting_approval` until it is decided; there is no timeout. Deny sets `state: failed`, error `denied`. Allow delivers, after checking the route again.

The default is `auto` because Argus's own `delegate` policy is `propose`: the person accepts each delegation in Argus before it is sent, and `ask` would gate it twice. `ask` is for when Argus's policy is raised to `auto`. Approvals already show in Attention through the approvals producer.

### D8. Follow-up messages

`POST /api/integrations/delegations/{id}/messages {text, idempotencyKey}` (scope `delegate`). Argus uses it to answer a `question` reply, with the person's answer or triage's.

- `text` 1 byte to 64 KiB. Idempotency as in D3, per delegation. Table `delegation_messages`: `id` (`dm-…`), `delegation_id`, `idempotency_key`, `request_hash`, `text`, `state`, `error_code`, `error_message`, `created_at`, `delivered_at`.
- The delegation must belong to the calling integration (404 `not_found` otherwise) and be `delivered` (409 `not_delivered` while pending or awaiting approval; 409 `closed` when failed).
- Delivered to the task's agent by `message`, or `resume` when it stopped since, with the tag `[argus case <caseId> · delegation <dl-id> · message]` and the same footer. `find_out` enforcement applies to a resume, as in D6.
- Answer: 201 `{messageId, state}`.

### D9. Status

`GET /api/integrations/delegations/{id}` (scope `delegate`; 404 for another integration's delegation):

```json
{"delegationId": "dl-…", "state": "pending|awaiting_approval|delivered|failed",
 "route": "message|resume|start", "taskId": "…", "agentId": "ex-…",
 "enforced": true, "error": {"code": "…", "message": "…"},
 "createdAt": "…", "deliveredAt": "…"}
```

`agentId` and `deliveredAt` appear once delivered, and `error` once failed.

### D10. The `reply_case` tool

A tool of `legato mcp serve`: `reply_case(delegation_id, outcome, text)`. `outcome` is `done`, `finding`, `question` or `cannot`. `text` is 1 byte to 16 KiB.

- **Listing.** Tools are listed when `mcp serve` starts, and a delegation can reach an agent that is already running. So the tool is listed in every session when the Argus integration is configured, not only in sessions that have a delegation. Its description says it is for answering an `[argus case …]` message and nothing else.
- **Checks on each call.** The delegation exists, is `delivered`, and its task is the caller's task. The execution may differ (a resumed agent). Otherwise it is a tool error that names the reason, and nothing is stored.
- **Sending.** The reply goes into the outbox (D12) with `{delegationId, outcome, text, agent: {taskId, agentId}}`, where `agentId` is the caller's execution ID. The host tries it at once, waiting up to 10 seconds. The tool answers with:
  - `sent` on 2xx;
  - a tool error with Argus's code and message on 4xx (`409` the delegation is not this case's or the case is closed, `404` unknown case); the outbox entry is marked failed;
  - `queued, will retry` on 5xx, a network error or a timeout.
- Replies appear in the task's activity, with the delegations.

### D11. Connecting to Argus

`legato integration connect argus <argus-url> [--no-browser]` runs on the host:
1. **Discovery.** `GET <url>/.well-known/oauth-protected-resource`, then the authorization server's `/.well-known/oauth-authorization-server`.
2. **Registration** (RFC 7591) as a public client: `redirect_uris: ["http://127.0.0.1/callback"]`, `token_endpoint_auth_method: none`, grant types `authorization_code` and `refresh_token`, `client_name: "Legato (<hostname>)"`. Argus matches a loopback redirect without its port (RFC 8252 §7.3), so the port can change between runs.
3. **Authorization.** Legato binds `127.0.0.1:0`, and opens `authorize?…&scope=argus:reply&resource=<resource>` with PKCE (S256) and a random `state` in the browser, or prints it with `--no-browser`. On a host without a browser, the person opens the address elsewhere. The redirect to `127.0.0.1` then fails in that browser, and the person pastes the address it tried into the command, which reads the code from it.
   `<resource>` is the `resource` of the protected-resource metadata (Argus's is `<origin>/mcp`; it refuses anything else but the bare origin with `invalid_target`). Legato sends the same value on every token request. The token it gets also works on `/api/cases/{id}/replies`.
4. **Exchange.** Code for an access token and a refresh token.
5. **Storage.** The refresh token goes in Legato's sealed store as `legato://integration-argus-refresh` (type `oauth-refresh`). Table `integrations`: `name`, `url`, `issuer`, `token_endpoint`, `client_id`, `connected_at`, `state` (`connected`, `disconnected`). This needs a master key source the host can read unattended, not `passphrase`; the command says so when it is not.

`legato integration status argus [--json]` reports the connection and the outbox. `legato integration disconnect argus` removes the refresh token and the row.

`internal/engine/oauthclient` holds steps 1–4 and refresh. It knows nothing of Legato or Argus.

### D12. Access tokens and the outbox

- **Where.** The outbox runs in the host that holds the broker lock (`cmd/legato/broker_runtime.go`). Exactly one host sends, and it takes over within 5 seconds when that host ends. `reply_case` calls arrive there through the broker. A call made without a broker (an unsandboxed session whose host broker did not answer) only writes the entry; the host sends it later.
- **Access tokens.** Kept in memory only. Refreshed when the token expires within 60 seconds, or on a 401. Refresh runs single-flight, and the new refresh token is written to the store before the new access token is used.

  Argus rotates refresh tokens on every use and answers `invalid_grant` for a rotated one, without revoking the family. So on `invalid_grant` the host reads the stored token again (a `connect` may have replaced it) and retries once if it changed. Otherwise the integration becomes `disconnected`, Attention raises `integration_disconnected`, and the outbox holds its entries until the person connects again.
- **Table `case_reply_outbox`**: `id` (`cr-…`), `delegation_id`, `case_id`, `body` (JSON), `state` (`pending`, `sent`, `failed`), `attempts`, `next_attempt_at`, `last_error`, `created_at`, `sent_at`.
- **Sending.** `POST <url>/api/cases/{caseId}/replies` with `Authorization: Bearer <access>` and `Idempotency-Key: <outbox id>`, so a repeat is harmless.
  - 2xx: `sent`. Argus answers 202 when it adds the reply, and 200 with the same entry for a repeated key.
  - 401: refresh once, then retry. A 401 after a fresh token, or a 403 (missing `argus:reply`), is a problem with the connection, not with the reply: the integration becomes `disconnected` as for `invalid_grant`, and the entry stays `pending`.
  - Any other 4xx: `failed`. Argus answers 409 `not_this_case` or `closed`, and 404 for an unknown case.
  - 5xx, network errors, timeouts: retry with backoff from 30 seconds, doubling to 1 hour.
  - An entry still unsent after 24 hours is `failed`, and Attention raises `case_reply_failed` on the task.

### D13. Errors

Integration routes answer errors as `{"error": "<code>", "message": "<text>"}`. The other API routes keep `{"error": "<message>"}`.

| Code | Status | When |
|---|---|---|
| `unauthorized` | 401 | no token, an unknown or revoked one |
| `forbidden` | 403 | a scoped token outside `/api/integrations/` |
| `missing_scope` | 403 | the token lacks the route's scope |
| `bad_request` | 400 | a malformed or oversized body or query |
| `not_found` | 404 | no owner; an unknown delegation or another integration's |
| `unknown_task` | 404 | target `{task}` does not exist or is archived |
| `unknown_repo` | 422 | the repository is not in the catalog with a `primary_dir` |
| `no_checkout` | 422 | a `fix` with no checkout to run in, or a task with no working directory |
| `not_resumable` | 422 | a stopped swarm or workflow agent |
| `idempotency_conflict` | 409 | the same key with a different body |
| `not_delivered` | 409 | a follow-up message before delivery |
| `closed` | 409 | a follow-up message to a failed delegation |
| `unavailable` | 503 | the integration is not configured, or the agent service is missing |

Errors recorded on a delegation after acceptance use the same codes, plus `denied` and `spawn_failed`.

### D14. Configuration

```yaml
integrations:
  argus:
    url: https://argus.lan     # required; where replies go and connect registers
    delivery: auto             # auto | ask
    agent: claude-code         # the tool a started agent runs
```

Without `integrations.argus`, the delegation routes answer 503 `unavailable` and `reply_case` is not listed. Owner lookup and tokens work without it. `url` must be `https`, or `http` to a loopback address. An unknown `delivery` or `agent` fails config loading.

## As built

Where the build differs from the decisions above, and why. `docs/ai/integrations.md` describes what was built.

- **D4, a PR's head branch.** Legato does not read it from GitHub. A `{repo, pr}` target without `branch` gets no worktree: `find_out` starts in `primary_dir`, `fix` is 422 `no_checkout`. Argus sends `branch` whenever the case has one. A fork's branch fails when the worktree is made, and the delegation then fails with `no_checkout`.
- **D5, when a delegation is delivered.** When `QueueMessage` returns, or when the spawn that carries the brief as its first message returns, rather than when a native bridge has bound.
- **D6, sandbox.** The spawn is forced into the sandbox only where `agents.sandbox` is configured. `enforced` is true only when the started session is sandboxed. The credential broker's refusal of pushes and PR writes (`executions.read_only`) applies either way.
- **D8, a message to an agent that ended without a saved conversation.** Recorded and answered with `state: failed` (201), not refused with 422.
- **D10, sending.** Only the process that holds the broker sends replies. A `legato mcp serve` that reaches no broker records the reply, and that process sends it within 15 seconds.
- **D12, delegations left pending.** The process that holds the broker delivers delegations that have been pending for more than 5 minutes (`ResumeStale`), for a Legato that stopped between accepting and delivering.
- **D13, error codes.** `message_failed` is added, for a message the agent service could not deliver.
- **Task activity in the web UI.** `GET /api/tasks/{id}/activity` returns delegations and replies, but the web UI does not show them yet.

## Risks / Trade-offs

- **A brief can carry text an attacker wrote** (a log line, a PR title). Argus fences evidence as data and writes the brief in code, and Legato tags the message as from Argus. Neither stops a model from following text it should not. The safeguards are scope: `find_out` is enforced when Legato starts the agent, a scoped token cannot approve anything, and `delivery: ask` lets the person read every brief.
- **A message to an agent the person is driving** lands in their conversation. With `delivery: auto` that is Argus's `propose` policy doing the gating. A tmux-only agent showing a permission dialog gets send-keys, as peer messages do today; native bridges deliver without typing.
- **`find_out` to a running agent is not enforced.** It is reported (`enforced: false`) rather than hidden.
- **Listing `reply_case` in every session** shows it to agents that never get a delegation. Its description limits it, and calls without a delegation to the caller's task are refused.
- **The loopback redirect on a headless host** needs the paste-back step. Without it, `connect` would need Legato's own web address registered as the redirect, which ties registration to Legato being reachable from the browser.
- **One sender.** If no host holds the broker lock (only short-lived commands running), replies wait in the outbox.

## Migration Plan

New tables only, in one migration. Nothing existing changes shape. Without `integrations.argus` and without tokens, Legato behaves as before. Rollback: drop the config block and revoke tokens; the tables can stay.

## Open Questions

None. Settled with the person:
- **Approval is a setting** (`integrations.argus.delivery`, D7), not fixed either way.
- **A `fix` for a PR goes on the PR's own branch** (D4), not a new branch from it: fixing a failing PR means making that PR pass. Another kind of case that wants a separate branch can add a target for it later.
