Every task is test-first (red, green, refactor). This change needs `credential-broker` (phase 2). It does not need the sandbox.

## 1. HTTP proxy

- [ ] 1.1 Migration `credential_placeholders`; store methods to create a placeholder hash, resolve it to a running execution and credential, and revoke an execution's placeholders; test that a terminal execution resolves to nothing and that the store holds no placeholder in plain form
- [ ] 1.2 `credential.HTTPProxy` channel: `Open` makes a placeholder and returns the profile's variables (base URL and placeholder); `Close` revokes it; unit test of the returned environment
- [ ] 1.3 Proxy handler with `httptest` upstreams: placeholder resolution (`401` for none, unknown, other credential), key replacement in a header, a bearer format and a query parameter; test that the upstream never receives the placeholder and the agent never receives the key
- [ ] 1.4 Header hygiene: hop-by-hop and `Proxy-*` removed, redirects passed back unfollowed, a response header that echoes the key stripped; table test
- [ ] 1.5 TLS to the upstream with certificate checks; test that a self-signed upstream is refused unless the test installs its CA
- [ ] 1.6 Start the listener in `legato serve` and the TUI runtime; `credential_channels.http_proxy.listen` config with validation; shut down cleanly

## 2. The `rest` inspector and meters

- [ ] 2.1 Route table parsing and matching (method, `{name}` segments, `**`), first match wins, resource templates; table test that an unmatched request is refused
- [ ] 2.2 `characters` meter from a JSON path in the body, counting code points; `413` for a metered body over 1 MiB; tests with multi-byte text
- [ ] 2.3 `requests` and `bytes` meters, bytes counted while the response streams; test with a chunked upstream response
- [ ] 2.4 Migration `credential_meter_totals`; totals per credential, meter and period that survive a restart; test that a daily limit holds across a reopened store
- [ ] 2.5 Pass amounts to `CredentialBroker.Authorize` before forwarding, and add response bytes after; test that a request past a limit falls to the next rule
- [ ] 2.6 A request that needs the person is held up to 60 seconds, then answered `403` with the approval ID in a JSON body; test with an injected clock

## 3. Profiles `elevenlabs` and `api-key`

- [ ] 3.1 Embed `elevenlabs.yaml`; profile validation test; route table tests for every shipped route, including the denied account and key routes
- [ ] 3.2 Default rules of `elevenlabs` (auto under 20,000 characters a day, window after, management denied); service test against a fake upstream
- [ ] 3.3 Embed `api-key.yaml` with `settings.upstream` and `settings.key`; config validation test that rejects a non-`https` upstream and a key place that is neither a header nor a query parameter
- [ ] 3.4 Every proxied use reaches the audit log with method, path, operation, resource and amounts; test against the hash chain

## 4. SSH agent

- [ ] 4.1 Agent protocol codec in `internal/engine/credential`: identities, sign request, extension, failure; tests against byte fixtures from draft-miller-ssh-agent
- [ ] 4.2 `credential.SSHAgent` channel: one socket per execution under `<runtime dir>/legato/ssh/` (modes `0700` and `0600`), removed on close; `REQUEST_IDENTITIES` lists only the credentials' public keys; test that changes (`ADD_IDENTITY`, `LOCK`) are refused
- [ ] 4.3 `session-bind@openssh.com`: parse, verify the server's signature over the session identifier, keep the binding per connection; tests with a forged signature and a second bind on one connection
- [ ] 4.4 `ssh` inspector: parse `SSH_MSG_USERAUTH_REQUEST` sign data (user, service, method, host-bound variant), refuse a mismatched session identifier, refuse data that is not user authentication; table tests
- [ ] 4.5 Host key to host names through `known_hosts`, plain and hashed entries, a `key:` resource for an unknown key; tests
- [ ] 4.6 Hold a sign request up to 60 seconds, then `SSH_AGENT_FAILURE` with the approval left open; test with an injected clock
- [ ] 4.7 Forwarded connections map to `ssh.forwarded-login`; test that the default rule denies them
- [ ] 4.8 Embed `ssh.yaml` (window of 1 hour per host, forwarding denied); profile validation test
- [ ] 4.9 Integration test (skipped without `ssh` and `sshd`): a test `sshd` on loopback, `ssh -o IdentitiesOnly=yes` through the execution's socket logs in when the rule allows and fails when it denies; a client without session bind is refused

## 5. Agent sessions and `applySSHAgent`

- [ ] 5.1 `applySSHAgent` records the socket it chose; config test for each of its three sources
- [ ] 5.2 With an `ssh` credential configured, the agent service puts the execution's socket in the session environment; without one, the session keeps the inherited value; tests with `mockTmux`
- [ ] 5.3 `[legato approvals]` message after a decision on a held sign request; service test

## 6. Docs

- [ ] 6.1 `docs/ai/credentials.md`: the two channels, placeholders, meters, the three profiles, and the limits outside the sandbox
- [ ] 6.2 `docs/ai/config.md`: `credential_channels`, and how `ssh_agent_socket` and 1Password auto-detection now apply to Legato's own processes only when an `ssh` credential exists
