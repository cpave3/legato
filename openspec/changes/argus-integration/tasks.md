Every task is test first (the tdd skill). Keep `docs/ai/integrations.md` (new) current as each part lands: the routes, the contract with Argus, the commands and the setup.

## 1. Tokens

- [x] 1.1 Migration: `integration_tokens`; store methods to create, find by hash, list, revoke and touch `last_used_at` (at most once a minute). Validation: the store tests (`docs/ai/database.md`)
- [x] 1.2 `IntegrationTokenService` and `legato integration token create|list|revoke`. Validation: a token is printed once, only its hash is stored, unknown scopes and bad names are refused, a revoked name can be reused (`docs/ai/cli.md`)
- [x] 1.3 Auth middleware: scoped tokens on `/api/integrations/*` only, scope per route, 403 `forbidden`/`missing_scope`, no WebSocket, the integration's name in the context. The integration error writer `{error, message}`. Validation: a middleware table test over the full token, a scoped token, a revoked one and none, on the board, the WebSocket and each integration route (`docs/ai/web-ui.md` § Authentication)

## 2. Owner lookup

- [x] 2.1 Store query: tasks that are not archived, by repository in `code_context`, with PR number, SHA prefix and branch. Validation: matches and ranks against real SQLite rows
- [x] 2.2 `OwnerLookupService`: ranking, agent states from the agent service (`waiting`, `stopped` from a saved conversation), `url` from `web.public_url`. Validation: service tests with `mockTmux`
- [x] 2.3 `GET /api/integrations/owner`. Validation: handler tests for 200, 400 and 404, and no working directory in the JSON

## 3. Delegations

- [x] 3.1 `integrations.argus` config block (`url`, `delivery`, `agent`) with validation. Validation: config tests (`docs/ai/config.md`)
- [x] 3.2 Migration: `delegations`, `delegation_messages`; store methods with idempotency by `(integration, key)` and the request hash
- [x] 3.3 `DelegationService` routing: target resolution, the `message`/`resume`/`start` choice, new tasks from the catalog, the worktree for a PR head, and the 404/422 refusals. Validation: a table test of every row of design D4
- [x] 3.4 Delivery: the tagged message and footer, `delivered` when the message path accepts it or the kickoff is submitted, `failed` with Attention `delegation_failed` (added to the central allowlist). Validation: service tests through the agent service's message, resume and spawn paths
- [x] 3.5 `find_out` enforcement: the `ReadOnlyCheckout` spawn option in `BuildSandboxPlan`, session credential rules that refuse push and PR writes, and `enforced`. Validation: a sandbox plan test, a broker rule test, and a scratch sandboxed spawn where a write and a push fail (`docs/ai/sandbox.md`)
- [x] 3.6 `delivery: ask`: the `delegation` approval source and its resolver. Validation: allow delivers after routing again, deny fails with `denied` (`docs/ai/approvals.md`)
- [x] 3.7 Follow-up messages and status: `POST …/messages`, `GET …/{id}`. Validation: handler tests for each state and for another integration's delegation
- [x] 3.8 `POST /api/integrations/delegations` and wiring in both host runtimes. Validation: handler tests for 201, 200 on repeat, 409 on conflict and 503 without config
- [x] 3.9 Task activity lists delegations and their replies (`GET /api/tasks/{id}/activity`)
- [ ] 3.10 The web activity view shows them

## 4. Replies

- [x] 4.1 `internal/engine/oauthclient`: discovery, registration, PKCE authorization with a loopback listener and a pasted redirect, code exchange, refresh. Validation: tests against an `httptest` authorization server, including a `state` mismatch and a loopback port that differs from the registered one
- [x] 4.2 Migration: `integrations`, `case_reply_outbox`
- [x] 4.3 `legato integration connect|status|disconnect argus`. Validation: refuses the `passphrase` master key, stores the refresh token sealed, never prints it
- [x] 4.4 `CaseReplyService`: access tokens in memory, single-flight refresh that stores the rotation first, the `invalid_grant` re-read, `disconnected` with Attention `integration_disconnected`. Validation: service tests against a fake Argus that rotates and rejects reused tokens
- [x] 4.5 The outbox sender in the broker-lock host: `Idempotency-Key`, the 401/4xx/5xx rules, backoff, failure after 24 hours with Attention `case_reply_failed`. Validation: tests with a fake clock and a fake Argus
- [x] 4.6 The `reply_case` MCP tool: listed when Argus is configured, checks the delegation's task, first attempt within 10 seconds, `sent`/`queued`/error answers, relayed through the broker. Validation: `internal/cli/mcp` tests through the broker and without it (`docs/ai/agent-mcp.md`)

## 5. Together

- [x] 5.1 A scratch Legato with a fake Argus (no agent was spawned: the delegation was seeded as delivered, so the reply went through `legato mcp serve` and the host's outbox) (Node or Go `httptest`) running the whole loop: create a token, look up the owner, delegate `find_out` to a started agent, reply `question`, send a follow-up message, reply `finding`. Stop the scratch instance and remove its folders afterwards
- [x] 5.2 Tell the Argus agent the final paths and any change from this design; `docs/ai/integrations.md` complete
