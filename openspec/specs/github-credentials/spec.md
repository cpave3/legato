## ADDED Requirements

### Requirement: The github profile
Legato SHALL ship a profile named `github` that takes a GitHub App's private key as a `keypair` secret and settings `client_id`, `installation` (optional; looked up per repository when absent) and `repositories` (`owner/name` entries). It SHALL use the `jwt-exchange` issuer, the `helper` channel for reads, and the `http-proxy` channel with the `git`, `rest` and `graphql` inspectors for writes. Its default rules SHALL be: `git.read` and `api.read` `auto` for each listed repository; `git.push` and `pr.create` on `refs/heads/legato/**` `session`; `git.push` and `pr.create` on other branches `request` and `signed`; `git.push-default`, `git.push-tag` and `git.delete-ref` `deny`. A repository that is not listed SHALL match no rule.

#### Scenario: Read of a listed repository
- **WHEN** an agent fetches `acme/widget` and `acme/widget` is in `settings.repositories`
- **THEN** the read SHALL be granted without an approval request

#### Scenario: Repository not listed
- **WHEN** an agent fetches `acme/other`, which is not listed
- **THEN** the read SHALL be refused with a message that names `settings.repositories`

#### Scenario: Push to an agent branch
- **WHEN** an agent pushes to `legato/edu1ugzz-fix-sort` in a listed repository for the first time in its execution
- **THEN** Legato SHALL raise one approval request with the mode `session`

#### Scenario: Push to the default branch
- **WHEN** an agent pushes to the repository's default branch
- **THEN** the push SHALL be refused by the `deny` rule

### Requirement: Reads through a credential helper and a gh wrapper
The `helper` channel SHALL configure git through the agent's environment so that, for `https://github.com`, helpers found earlier are dropped and `legato credential git` is the helper, with `useHttpPath` on. `legato credential git get` SHALL answer with username `x-access-token` and a read token for the one repository in the request. The channel SHALL put a `gh` wrapper first on the agent's `PATH` that sets `GH_TOKEN` to a read token for the repository named by `-R`, `--repo`, `GH_REPO` or the `origin` remote, only for that call.

#### Scenario: git fetch
- **WHEN** an agent runs `git fetch` in a clone of a listed repository
- **THEN** git SHALL authenticate with a read token for that repository from `legato credential git`

#### Scenario: Another helper in the person's configuration
- **WHEN** `~/.gitconfig` sets `gh auth git-credential` as the helper for `github.com`
- **THEN** git in the agent's session SHALL NOT run it

#### Scenario: gh read
- **WHEN** an agent runs `gh pr list` in a clone of a listed repository
- **THEN** the wrapper SHALL run `gh` with `GH_TOKEN` set to a read token for that repository
- **AND** `GH_TOKEN` SHALL NOT be set in the agent's session environment

### Requirement: Pushes go through the proxy
The `helper` channel SHALL set `pushInsteadOf` so that pushes to `https://github.com/`, `git@github.com:` and `ssh://git@github.com/` go to the credential's proxy prefix. `legato credential git` SHALL answer for the proxy's host with the execution's placeholder. The proxy SHALL forward a push to GitHub only with a `contents: write` token that it makes for that push and revokes when the push ends or after 2 minutes. The token SHALL NOT be sent to the agent.

#### Scenario: Push rewritten
- **WHEN** an agent runs `git push origin legato/edu1ugzz-fix-sort` with `origin` at `git@github.com:acme/widget.git`
- **THEN** git SHALL send the push to the proxy

#### Scenario: Token revoked after the push
- **WHEN** a forwarded push ends
- **THEN** Legato SHALL revoke the write token it made for that push

### Requirement: The git inspector
The `git` inspector SHALL read the commands `<old-oid> <new-oid> <refname>` and the push options of a `git-receive-pack` request without reading the pack. It SHALL make one use per command with resource `github:<owner>/<repo>@<refname>` and operation `git.delete-ref` when the new ID is all zeros, `git.push-tag` for `refs/tags/`, `git.push-default` for the default branch, and `git.push` for other `refs/heads/` refs. It SHALL refuse any other ref and any body it cannot parse. A push SHALL be forwarded only when every command is within a grant.

#### Scenario: Branch within the grant
- **WHEN** a push updates only `refs/heads/legato/edu1ugzz-fix-sort` and a grant covers it
- **THEN** Legato SHALL forward the push

#### Scenario: One ref outside the grant
- **WHEN** a push updates a granted branch and `refs/heads/main`
- **THEN** Legato SHALL forward nothing

#### Scenario: Deletion
- **WHEN** a command's new ID is all zeros
- **THEN** its operation SHALL be `git.delete-ref`

#### Scenario: Malformed body
- **WHEN** the request body is not a valid sequence of pkt-lines
- **THEN** Legato SHALL refuse the push and forward nothing

### Requirement: Refused pushes explain how to ask
A refused push SHALL receive a `git-receive-pack` response that git prints: messages on side band 2 when the client asked for `side-band-64k`, and `ng <refname> <reason>` for each ref when it asked for `report-status`. The message SHALL name the ref, the rule that refused it, and how to ask with `request_access`. A push that needs the person SHALL be refused at once, with the approval request ID in the message.

#### Scenario: Message in git's output
- **WHEN** a push to `main` is refused
- **THEN** `git push` SHALL print a `remote:` line that names `refs/heads/main` and the `deny` rule

#### Scenario: Needs the person
- **WHEN** a push needs an approval that is not yet decided
- **THEN** the push SHALL be refused at once with a message that names `ap-…`
- **AND** the agent SHALL receive a `[legato approvals]` message when the person decides

### Requirement: REST writes
The proxy SHALL serve GitHub's REST API under the credential's prefix and give the agent `LEGATO_GITHUB_API_URL` and `LEGATO_GITHUB_TOKEN` (the placeholder). `GET /repos/{owner}/{repo}/**` SHALL be `api.read`. `POST /repos/{owner}/{repo}/pulls` SHALL be `pr.create` with the resource of its `head` branch. Every other request, GraphQL included, SHALL be refused. A write SHALL be sent with a token that has only the permission it needs, revoked after the request.

#### Scenario: Open a pull request from an agent branch
- **WHEN** an agent posts to `/repos/acme/widget/pulls` with `head: legato/edu1ugzz-fix-sort` and a grant covers that branch
- **THEN** Legato SHALL forward the request with a `pull_requests: write` token and return GitHub's response

#### Scenario: Other write
- **WHEN** an agent sends `PUT /repos/acme/widget/pulls/7/merge`
- **THEN** Legato SHALL refuse it and SHALL NOT forward it

#### Scenario: GraphQL
- **WHEN** an agent posts to the GraphQL endpoint
- **THEN** Legato SHALL refuse it (GraphQL reads and `createPullRequest` are deferred)

### Requirement: Branch convention in the agent guidance
When a `github` credential is configured, `cli.MCPGuidance` and the `conductor.md` and `worker.md` role prompts SHALL tell agents to work on a branch named `legato/<task-id>-<slug>`, to ask with `request_access` for `git.push` on that one branch before the first push, to push with `git push`, and to open pull requests through `LEGATO_GITHUB_API_URL`. Without such a credential the guidance SHALL not mention it.

#### Scenario: Guidance with a credential
- **WHEN** an agent is launched and a `github` credential is configured
- **THEN** its guidance SHALL name `legato/<task-id>-<slug>` with its own task ID

#### Scenario: Guidance without a credential
- **WHEN** no `github` credential is configured
- **THEN** the guidance SHALL NOT mention the branch convention
