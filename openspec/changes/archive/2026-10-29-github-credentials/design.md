## Context

This is phase 4 of the agent approvals and credentials plan (`.legato/plans/agent-approvals-credentials/plan.md`). It builds on:

- `credential-broker` (phase 2): the `Issuer`, `Channel` and `Inspector` interfaces, `service.CredentialBroker`, profiles, rules, grants, `request_access`, and the audit log.
- `credential-channels` (phase 3): the `http-proxy` channel, placeholder keys, the `rest` inspector, and the `ssh-agent` channel.
- `agent-approvals` (phase 1): approval requests and the `[legato approvals]` message.

GitHub's token model sets the shape of this change:

- A GitHub App installation token can be narrowed to some repositories and some permissions (`POST /app/installations/{id}/access_tokens`). It lasts one hour and can be revoked (`DELETE /installation/token`).
- No token can be narrowed to a branch. `contents: write` writes every branch, tag and the default branch.

So reads can use a narrow token that the agent holds. A write token must never reach the agent: Legato must see each ref a push changes, and decide.

PR tracking keeps its own read client (`internal/engine/github`) and its own credential. Nothing here changes it.

## Goals / Non-Goals

**Goals:**
- An agent reads repositories on the credential's list without a prompt.
- An agent pushes only to the branches it was granted, and never holds a write token.
- An agent opens a pull request from a granted branch.
- Issuers that are not about GitHub: `oauth2` and `ssh-cert`.

**Non-Goals:**
- Merging, approving reviews, editing settings, or any GitHub write other than pushes and `pr.create`.
- `gh` writes (`gh pr create`). See D8 and Open Questions.
- Taking the person's own `gh` login and SSH keys away from an unsandboxed agent.
- Pushes over SSH to GitHub. Pushes go over HTTPS through the proxy.

## Decisions

### D1. `jwt-exchange` is a template, not GitHub code

The issuer runs a short, fixed program that the profile fills in:

```yaml
issuer: jwt-exchange
jwt: { alg: RS256, claims: { iss: "{settings.client_id}", iat: "{now-60}", exp: "{now+540}" } }
lookup:                                   # optional, cached per repository for 1 hour
  url: "https://api.github.com/repos/{owner}/{repo}/installation"
  set: { installation: "$.id" }
exchange:
  url: "https://api.github.com/app/installations/{installation}/access_tokens"
  body: { repositories: ["{repo}"], permissions: "{permissions}" }
  token: "$.token"
  expires_at: "$.expires_at"
revoke: { method: DELETE, url: "https://api.github.com/installation/token", auth: issued }
```

Placeholders are replaced with plain values from the grant, the credential's settings and the clock. No template can run code or reach a host the profile does not name. The JWT is signed with the credential's `keypair` secret; when the store is a `Signer`, the private key never leaves the store.

`{permissions}` comes from the profile's map from operation to permissions: `git.read` and `api.read` → `{contents: read, metadata: read}`, `git.push` → `{contents: write}`, `pr.create` → `{pull_requests: write}`.

A cached token is reused while it has more than 5 minutes left and covers the same repository and permissions.

### D2. The `oauth2` issuer

Grant types: `client_credentials` (secret type `oauth-client`), `refresh_token` (`oauth-refresh`), and RFC 8693 `token-exchange` with a subject token from another credential. Client authentication is `client_secret_basic` or `client_secret_post`. The profile maps a grant's operations and resources to `scope`, `audience` and `resource` parameters.

When a provider rotates the refresh token, the issuer writes the new one back through the store's `Putter`. A store without `Putter` cannot hold such a credential; Legato says so when it loads the configuration. Revocation uses RFC 7009 when the profile names a revocation endpoint.

### D3. The `ssh-cert` issuer and the `ssh-ca` profile

For a grant on `ssh://<user>@<host>`, the issuer makes a fresh Ed25519 key in memory and an OpenSSH user certificate for it, signed with the CA `keypair` from the store:

- key ID: the execution ID;
- principals: the users the grant covers;
- valid from 30 seconds before issue (clock skew) to 5 minutes after;
- extensions: `permit-pty` only; no agent, port or X11 forwarding;
- critical option `source-address` when `settings.source_address` is set.

The `ssh-agent` channel lists the certificate and signs with the fresh key. It still authorizes each sign request. The `ssh-ca` profile is the `ssh` profile with this issuer. Servers trust the CA through `TrustedUserCAKeys`.

### D4. Reads: a git credential helper and a `gh` wrapper

The `helper` channel sets git configuration through the environment (`GIT_CONFIG_COUNT`, `GIT_CONFIG_KEY_n`, `GIT_CONFIG_VALUE_n`):

```
credential.https://github.com.helper =                       # empty: drop helpers found earlier
credential.https://github.com.helper = !<legato> credential git
credential.https://github.com.useHttpPath = true
```

`legato credential git get` reads git's request, asks the broker for `git.read` on `github:<owner>/<repo>`, and answers `username=x-access-token` and the token. The token reads one repository for at most an hour.

`gh` has no credential helper. The channel puts a directory first on the agent's `PATH` with a `gh` wrapper that runs `legato credential gh-token` and then the real `gh` with `GH_TOKEN` set. The repository comes from `-R`/`--repo`, `GH_REPO`, or the `origin` remote of the current directory.

A read token in the agent's hands is accepted: it reads one repository the person listed, and it expires.

### D5. Pushes go to the proxy through `pushInsteadOf`

The same environment adds:

```
url.http://127.0.0.1:<port>/c/<credential>/git/.pushInsteadOf = https://github.com/
url.http://127.0.0.1:<port>/c/<credential>/git/.pushInsteadOf = git@github.com:
url.http://127.0.0.1:<port>/c/<credential>/git/.pushInsteadOf = ssh://git@github.com/
```

Fetches still go to GitHub with read tokens. A push goes to the proxy, and `legato credential git` answers for the proxy's host with the execution's placeholder. The proxy forwards `info/refs?service=git-receive-pack` and `git-receive-pack` to `https://github.com/<owner>/<repo>.git/`.

When the proxy receives the `info/refs` request, it makes a `contents: write` token for that repository. It uses the token for that request and the `git-receive-pack` POST that follows, and revokes it when the POST ends or after 2 minutes. The token never leaves the proxy.

### D6. The `git` inspector reads the ref updates

A `git-receive-pack` request body starts with pkt-lines `<old-oid> <new-oid> <refname>`, the first followed by a NUL and capabilities, then a flush packet, an optional push-options section, and the pack. The inspector reads the commands and the push options, and passes the pack through without reading it. It handles a gzip request body, SHA-1 and SHA-256 object IDs, and refuses anything else it cannot parse.

Each command becomes one use, with resource `github:<owner>/<repo>@<refname>`:

| Command | Operation |
| --- | --- |
| new OID all zeros | `git.delete-ref` |
| ref under `refs/tags/` | `git.push-tag` |
| the repository's default branch | `git.push-default` |
| any other ref under `refs/heads/` | `git.push` |
| any other ref | refused |

The default branch is read with the read token and cached for 10 minutes.

A push is all or nothing. If any command is outside a grant, the proxy forwards nothing.

### D7. Refusing a push in git's own terms

A refused push gets a well-formed `git-receive-pack` response, so git prints the reason. When the client asked for `side-band-64k`, the proxy sends the message on band 2, which git prints as `remote: …`. With `report-status`, every ref gets `ng <refname> <reason>`. The message names the ref, the rule, and the request ID when one was made:

```
remote: Legato: refs/heads/main is outside your grant (default branch: deny).
remote: Legato: to push legato/edu1ugzz-fix-sort, ask with request_access
remote:         (credential github, operation git.push), then push again.
```

A push that needs the person is refused at once with `pending ap-…`. The agent pushes again after the `[legato approvals]` message. Git has no way to wait, so the proxy does not hold a push.

### D8. REST and GraphQL writes

The proxy also serves `/c/<credential>/api/` for `https://api.github.com/`. The agent gets `LEGATO_GITHUB_API_URL` and `LEGATO_GITHUB_TOKEN` (the placeholder).

REST routes (the `rest` inspector): `GET /repos/{owner}/{repo}/**` is `api.read`. `POST /repos/{owner}/{repo}/pulls` is `pr.create`, with the resource `github:<owner>/<repo>@refs/heads/<head>` taken from the body's `head`. Every other route is refused.

`POST /graphql` (the `graphql` inspector): a `query` is `api.read` on the repositories it names; a `mutation` is classified by its top-level fields through the profile's table, which ships only `createPullRequest`. A subscription, a batch, or an unknown mutation is refused.

Each write gets a token with only the permission it needs, revoked after the request.

`gh pr create` cannot use this, because `gh` cannot point at a plain-HTTP API for `github.com`. The guidance shows the request with `curl`.

### D9. Default rules of the `github` profile

| Operation | Resource | Approve |
| --- | --- | --- |
| `git.read`, `api.read` | each repository in `settings.repositories` | `auto` |
| `git.push`, `pr.create` | `…@refs/heads/legato/**` | `session` |
| `git.push`, `pr.create` | `…@refs/heads/**` | `request`, `signed` |
| `git.push-default`, `git.push-tag`, `git.delete-ref` | `github:*@**` | `deny` |

A repository not in `settings.repositories` matches no rule and is refused. A rule names the list with `{repositories}`, which the profile expands to one rule per entry.

### D10. The `legato/` branch convention

When a `github` credential is configured, `cli.MCPGuidance` and the `conductor.md` and `worker.md` role prompts gain a section: work on `legato/<task-id>-<slug>`; ask with `request_access` for `git.push` on that one branch before the first push; push with plain `git push`; open the pull request through `LEGATO_GITHUB_API_URL`. Asking for one branch, not a pattern, keeps the grant small.

### D11. An optional ruleset backstop

The docs describe a repository ruleset that targets every branch and tag except `legato/**`, restricts creations, updates and deletions, and lets the person's role bypass it. The App cannot then write outside `legato/**` even if an inspector has a bug.

## Risks / Trade-offs

- [The person's own git credential helper or `gh` login answers before Legato's] → The empty `credential.helper` entry drops earlier helpers. Outside the sandbox the agent can still undo that; the docs say so.
- [An inspector bug lets a push through] → All-or-nothing pushes, refusal of anything unparsed, table tests of real `git push` captures, and the ruleset backstop.
- [GitHub changes the receive-pack framing or adds a capability] → Unknown capabilities pass through; unparsable commands are refused, never forwarded.
- [Per-push tokens add two API calls for each push] → Accepted. Installation tokens are cheap, and revocation keeps a token's life to seconds.
- [Installation tokens count against the App's rate limit] → Reads reuse cached tokens (D1).
- [A force push to a granted `legato/` branch] → Allowed within the grant. The inspector cannot tell a force push without the repository's history; the ruleset can block force pushes.

## Migration Plan

- Additive. Without a `github` credential there are no helper variables, no proxy prefixes and no guidance section.
- The person creates a GitHub App (contents read and write, pull requests write, metadata read), installs it on the repositories, and stores its private key (`legato secret add` or a 1Password reference).
- Rollback: remove the credential. Running agents lose the helper at their next git call.

## Open Questions

- Does an empty `credential.<url>.helper` from `GIT_CONFIG_*` drop helpers set for all URLs in `~/.gitconfig`? Verify against git 2.31 and current git.
- Does GitHub require write permission for `info/refs?service=git-receive-pack`, or would a read token serve the advertisement?
- Does git send a gzip body to `git-receive-pack`, and in which versions?
- Can `gh` be made to use the proxy for writes, for example through `GH_HOST` and a loopback HTTPS listener with a Legato CA?
- Do repository rulesets let the person bypass while blocking the App, and are they available for private repositories on the person's plan?
- Should `ssh-ca` be a setting of the `ssh` profile instead of a profile of its own?
- Should the JWT `iss` be the App ID or the client ID? GitHub accepts both today.
