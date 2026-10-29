Every task is test-first (red, green, refactor). This change needs `credential-broker` (phase 2) and the `http-proxy` channel of `credential-channels` (phase 3).

## 1. Issuers

- [x] 1.1 Template engine for issuer profiles: `{settings.*}`, grant values, `{now±seconds}`, JSON paths for results; tests that a template cannot name a host outside the profile and that unknown placeholders fail profile loading
- [x] 1.2 `jwt-exchange`: RS256 JWT from a `keypair`, through `Signer` when the store has one; test the claims and verify the signature with the public key (signed in Legato's process; signing through a store's `Signer` is not built)
- [x] 1.3 `jwt-exchange` lookup and exchange against an `httptest` fake of GitHub: installation lookup cached per repository, token and expiry read by path; test the request body names one repository and the operation's permissions
- [x] 1.4 Token cache: reuse while more than 5 minutes remain and scope matches; test with an injected clock
- [x] 1.5 Revocation with `DELETE /installation/token` using the issued token; test that a failed revocation is logged and retried once
- [ ] 1.6 (deferred) `oauth2` client credentials and refresh token with rotation written through `Putter`; config test that a rotating credential on a store without `Putter` is rejected
- [ ] 1.7 (deferred) `oauth2` RFC 8693 token exchange with a subject token from another credential; RFC 7009 revocation; tests against a fake token endpoint
- [ ] 1.8 (deferred) `ssh-cert`: fresh Ed25519 key, certificate with key ID = execution ID, principals from the grant, 30-second back-dating and 5-minute validity, `permit-pty` only; parse the result with `golang.org/x/crypto/ssh` in the test
- [ ] 1.9 (deferred) `ssh-agent` channel serves an issued certificate and re-issues after expiry; integration test (skipped without `sshd`) against an `sshd` with `TrustedUserCAKeys`
- [ ] 1.10 (deferred) Embed the `ssh-ca` profile; validation test

## 2. Reads

- [x] 2.1 `helper` channel environment: `GIT_CONFIG_COUNT` with the empty helper, Legato's helper, `useHttpPath` and the three `pushInsteadOf` entries; unit test of the variables
- [x] 2.2 `legato credential git get|store|erase`: parse git's request, answer for `github.com` paths with a read token and for the proxy host with the placeholder, ignore `store` and `erase`; tests with real `git` and a fake helper environment (the helper asks the host's proxy for read tokens, `POST /c/<credential>/token`, so the App key is read in one process and the helper works in a sandbox; tested with `git credential fill` and a fake proxy)
- [x] 2.3 Integration test with real `git`: a `~/.gitconfig` with another helper for `github.com` is not consulted when the channel's variables are set
- [x] 2.4 `gh` wrapper and `legato credential gh-token`: repository from `-R`, `--repo`, `GH_REPO` or `origin`; test that `GH_TOKEN` is set only in the wrapped process
- [x] 2.5 Reads of a repository not in `settings.repositories` are refused with a message naming the setting; service test

## 3. The git inspector and pushes

- [x] 3.1 pkt-line reader for receive-pack commands, capabilities, push options and flush; gzip bodies; SHA-1 and SHA-256 IDs; table tests from captured `git push` bodies, and fuzz tests that malformed input is refused
- [x] 3.2 Classification into `git.push`, `git.push-default`, `git.push-tag`, `git.delete-ref`, and refusal of other refs; default branch read and cached for 10 minutes; table tests
- [x] 3.3 All-or-nothing: a push with one command outside its grant forwards nothing; test against a fake upstream that records requests
- [x] 3.4 Refusal response: band 2 messages with `side-band-64k`, `ng` lines with `report-status`, plain text otherwise; integration test with real `git push` that the message appears in git's output
- [x] 3.5 Per-push write token: made at `info/refs`, used for the POST, revoked when the POST ends or after 2 minutes; test the token never appears in a response to the agent
- [x] 3.6 A push that needs the person is refused at once with `pending ap-…`; the agent receives `[legato approvals]` when decided; service test
- [x] 3.7 End-to-end test with real `git` against a local smart-HTTP server standing in for GitHub: push to a granted `legato/` branch succeeds; pushes to `main`, a tag and a deletion are refused (engine and service tests push with real `git` through the proxy to `git http-backend` over TLS)

## 4. REST and GraphQL writes

- [x] 4.1 `github` REST routes: `api.read` for repository GETs, `pr.create` with the head branch from the body, every other route refused; table tests
- [ ] 4.2 (deferred) `graphql` inspector with `gqlparser`: queries are reads, mutations classified by the profile table (`createPullRequest` only), subscriptions, batches and unknown mutations refused; table tests
- [x] 4.3 Per-request write token with only the needed permission, revoked after; test against the fake GitHub

## 5. Profile and guidance

- [x] 5.1 Embed the `github` profile with `settings.client_id`, `settings.installation` and `settings.repositories`; `{repositories}` expansion in rules; validation tests
- [x] 5.2 Default rules as in the design; service tests for each row, including a pattern request marked broader on the approval card
- [x] 5.3 Guidance section in `cli.MCPGuidance` and the `conductor.md` and `worker.md` prompts, present only when a `github` credential is configured; tests on the generated text (in the credential guidance every agent gets, MCP instructions or launch prompt; the swarm role prompts are not changed, since swarm agents get the same guidance)
- [x] 5.4 Docs: `docs/ai/credentials.md` (GitHub App setup, the branch convention, the ruleset backstop, and the limits outside the sandbox), `docs/ai/pr-tracking.md` (PR tracking keeps its own read client)

## Deferred

Deferred on 2026-10-29: not needed yet, to be picked up when a use for them appears.

- The `oauth2` issuer (1.6, 1.7): no configured service uses OAuth tokens yet.
- The `ssh-cert` issuer and `ssh-ca` profile (1.8–1.10): stored SSH keys from the vault cover today's hosts; certificates are worth building when servers trust a Legato CA.
- GraphQL writes (4.2): pull requests are opened through the REST route, and `gh` cannot use either route.
- Signing the App's JWT through a store's `Signer` (part of 1.2): the key is read into Legato's process to sign.
