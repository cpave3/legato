## Why

Argus watches the person's systems (CI first) and opens cases when something fails. Many of these failures are owned by a Legato task: a pull request an agent opened, or a branch it is working on. Argus needs to ask Legato who owns the work, hand the owning agent a brief, and hear back from that agent. Today Argus can do none of this without Legato's full-access web token, which can do everything the person can.

Argus's side is in the Argus repository (`openspec/changes/initial-build`, spec `legato-integration`, design § D12). This change is Legato's side of the contract that the two agreed.

## What Changes

- **Integration tokens.** Legato issues named bearer tokens with scopes. A scoped token reaches only `/api/integrations/*`, and only the routes its scopes allow. Argus gets one with `owner:read` and `delegate`.
- **Owner lookup.** `GET /api/integrations/owner?repo=&pr=&branch=&sha=` returns the tasks that own a pull request, branch or commit, best match first, each with its agents and their states.
- **Delegations.** `POST /api/integrations/delegations` gives a task's agent a brief from Argus. Legato chooses the route:
  - `message` to a running agent;
  - `resume` of a stopped one;
  - `start` of a new agent, in the task's checkout, the repository's main checkout, or a new worktree for a pull request.

  A `find_out` delegation that Legato starts itself runs in the sandbox, with the checkout read-only and pushes refused. Delivery can wait for the person's approval (`integrations.argus.delivery: ask`). Further messages from Argus (answers to an agent's question) go to the same agent. `GET /api/integrations/delegations/{id}` reports the delegation's state.
- **Replies.** Agents get a `reply_case` tool. Legato forwards a reply to Argus's `POST /api/cases/{caseId}/replies` through a durable outbox, as an OAuth client of Argus with scope `argus:reply`. `legato integration connect argus <url>` registers Legato with Argus and gets the person's consent.
- **Errors** of the integration routes are JSON `{error: "<code>", message}`.

Not in this change: other integrations than Argus (the token mechanism is general, the delegation flow is Argus's), pushing delivery state to Argus (it asks), and changing the permissions of an agent that is already running.

## Capabilities

### New Capabilities
- `integration-tokens`: Named, scoped, hashed bearer tokens; the commands that create, list and revoke them; where they are accepted.
- `owner-lookup`: Which tasks and agents own a repository's pull request, branch or commit.
- `delegations`: Receiving a brief, choosing and running its route, approval, read-only enforcement, follow-up messages, and status.
- `case-replies`: The `reply_case` tool, the OAuth connection to Argus, and the outbox that delivers replies.

### Modified Capabilities

None. Existing routes keep the full token. Agent tools gain one tool.

## Impact

- **Code:**
  - `internal/engine/store`: migrations for integration tokens, delegations and their messages, and the reply outbox.
  - `internal/engine/oauthclient` (new): discovery, dynamic client registration, PKCE with a loopback redirect, and token refresh. It knows nothing of Legato.
  - `internal/service`: `IntegrationTokenService`, `OwnerLookupService`, `DelegationService`, `CaseReplyService` (outbox and access tokens).
  - `internal/server`: the auth middleware learns scoped tokens; `integrations.go` serves the routes.
  - `internal/cli`: `legato integration token …`, `legato integration connect|status|disconnect`; the `reply_case` MCP tool.
  - `internal/service/sandbox_plan.go`: a read-only checkout for a spawn that asks for one.
  - `cmd/legato`: wiring in both host runtimes; the outbox runs in the host that holds the broker lock.
- **Config:** `integrations.argus.{url, delivery, agent}`.
- **Docs:** `docs/ai/integrations.md` (new), `docs/ai/config.md`, `docs/ai/cli.md`, `docs/ai/agent-mcp.md`, `docs/ai/web-ui.md` (auth), `docs/ai/database.md`.
- **Dependencies:** none new. OAuth is a few HTTP calls; `golang.org/x/oauth2` is not needed.
