## Why

Agents push branches and open pull requests. Today they do it with the person's own `gh` login or SSH keys, which can push to any branch of any repository the person can reach, including `main`.

GitHub cannot limit a token to a branch. A fine-grained token or a GitHub App installation token gives `contents: write` for a whole repository. So the branch limit must be applied where Legato can see the push: in the request itself.

The `credential-broker` change has the rules and the grants, and `credential-channels` has the HTTP proxy. This change adds what GitHub needs: short-lived tokens from a GitHub App, a credential helper for reads, and an inspector that reads each ref a push updates.

## What Changes

- **`jwt-exchange` issuer.** A templated issuer: it signs a JWT with a stored key, sends it to a token endpoint, and reads the token and its expiry by JSON path. For GitHub it makes installation tokens narrowed to one repository and the permissions an operation needs, and revokes them with `DELETE /installation/token`.
- **`oauth2` issuer.** Client credentials, refresh tokens (with rotation written back to the store), and RFC 8693 token exchange.
- **`ssh-cert` issuer.** An OpenSSH user certificate for a fresh key, signed by a stored CA key, with key ID = the execution ID and a validity of 5 minutes. The `ssh-agent` channel serves it. A shipped `ssh-ca` profile uses it.
- **`helper` channel for reads.** `legato credential git` is a git credential helper. A `gh` wrapper on the agent's `PATH` sets `GH_TOKEN` for each call. Both get read-only tokens for one repository.
- **`git` inspector on `http-proxy`.** Pushes go to Legato's proxy through `pushInsteadOf`. The inspector reads the ref updates (`old new refname`) of `git-receive-pack`. A push within the grant is forwarded with a write token made for that push and revoked after it. A push outside the grant is refused, with a message in the agent's terminal that names how to ask for access.
- **REST and GraphQL writes through the proxy.** Opening a pull request from a branch is the operation `pr.create`, scoped to the head branch. Other writes are refused unless a rule names them.
- **`github` profile.** Default rules: reads are `auto` for the repositories the credential lists; pushes to `legato/**` are `session`; pushes to other branches are `request` and signed; tags, deletions and the default branch are `deny`.
- **Branch convention.** Agents work on `legato/<task-id>-<slug>`. The agent guidance (`cli.MCPGuidance`) and the conductor and worker role prompts say so when a `github` credential is configured.
- **Ruleset backstop (optional).** Docs for a GitHub ruleset that stops the App from writing outside `legato/**` even if Legato has a bug.

## Capabilities

### New Capabilities
- `credential-issuers`: The `jwt-exchange`, `oauth2` and `ssh-cert` issuers and their templates.
- `github-credentials`: The `github` profile, the `helper` channel for git and `gh`, the `git` inspector for branch-scoped pushes, REST and GraphQL writes, the `legato/` branch convention, and the `ssh-ca` profile.

### Modified Capabilities
- None in the main specs. `sandboxed-cloud-agents` (not yet archived) is revised to use this change for pushes and pull requests.

## Impact

- **Code:** `internal/engine/credential` (new files: `issuer_jwt.go`, `issuer_oauth2.go`, `issuer_sshcert.go`, `channel_helper.go`, `inspect_git.go`, `inspect_graphql.go`, `pktline.go`); `internal/engine/credential/profiles/` (`github.yaml`, `ssh-ca.yaml`); `internal/service` (installation lookup cache, per-push tokens, default-branch lookup); `internal/cli` (`legato credential git`, `legato credential gh-token`, guidance text); `internal/engine/hooks/prompts/` (`conductor.md`, `worker.md`).
- **APIs:** proxy prefixes `/c/<credential>/git/` and `/c/<credential>/api/`; agent variables `LEGATO_GITHUB_API_URL` and `LEGATO_GITHUB_TOKEN` (a placeholder); `GIT_CONFIG_*` variables in the agent environment.
- **Config:** a `github` credential's `settings` (`client_id`, `installation`, `repositories`); `oauth2` and `ssh-cert` settings in their profiles.
- **Dependencies:** `github.com/golang-jwt/jwt/v5` or a small RS256 signer on the standard library (to decide); `github.com/vektah/gqlparser/v2` to parse GraphQL documents; git 2.31 or later on the agent side for `GIT_CONFIG_COUNT`. A GitHub App created and installed by the person.
- **Unchanged:** PR tracking's read client and its credential. Outside the sandbox an agent can still use the person's own SSH keys and `gh` login; this change does not take them away.
