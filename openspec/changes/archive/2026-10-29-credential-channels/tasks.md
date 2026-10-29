Every task is test-first (red, green, refactor). This change needs `credential-broker` (phase 2). It does not need the sandbox.

## 1. HTTP proxy

- [x] 1.1 Migration `credential_placeholders`; store methods to create a placeholder hash, resolve it to a running execution and credential, and revoke an execution's placeholders; test that a terminal execution resolves to nothing and that the store holds no placeholder in plain form
- [x] 1.2 `credential.HTTPProxy` channel: `Open` makes a placeholder and returns the profile's variables (base URL and placeholder); `Close` revokes it; unit test of the returned environment
- [x] 1.3 Proxy handler with `httptest` upstreams: placeholder resolution (`401` for none, unknown, other credential), key replacement in a header, a bearer format and a query parameter; test that the upstream never receives the placeholder and the agent never receives the key
- [x] 1.4 Header hygiene: hop-by-hop and `Proxy-*` removed, redirects passed back unfollowed, a response header that echoes the key stripped; table test
- [x] 1.5 TLS to the upstream with certificate checks; test that a self-signed upstream is refused unless the test installs its CA
- [x] 1.6 Start the listener in `legato serve` and the TUI runtime; `credential_channels.http_proxy.listen` config with validation; shut down cleanly

## 2. The `rest` inspector and meters

- [x] 2.1 Route table parsing and matching (method, `{name}` segments, `**`), first match wins, resource templates; table test that an unmatched request is refused
- [x] 2.2 `characters` meter from a JSON path in the body, counting code points; `413` for a metered body over 1 MiB; tests with multi-byte text
- [x] 2.3 `requests` and `bytes` meters, bytes counted while the response streams; test with a chunked upstream response
- [x] 2.4 Migration `credential_meter_totals`; totals per credential, meter and period that survive a restart; test that a daily limit holds across a reopened store (one row per measured amount, summed over the rule's period before the use, as phase 2's request limits were)
- [x] 2.5 Pass amounts to `CredentialBroker.Authorize` before forwarding, and add response bytes after; test that a request past a limit falls to the next rule
- [x] 2.6 A request that needs the person is held up to 60 seconds, then answered `403` with the approval ID in a JSON body; test with an injected clock

## 3. Profiles `elevenlabs` and `api-key`

- [x] 3.1 Embed `elevenlabs.yaml`; profile validation test; route table tests for every shipped route, including the denied account and key routes
- [x] 3.2 Default rules of `elevenlabs` (auto under 20,000 characters a day, window after, management denied); service test against a fake upstream
- [x] 3.3 Embed `api-key.yaml` with `settings.upstream` and `settings.key`; config validation test that rejects a non-`https` upstream and a key place that is neither a header nor a query parameter
- [x] 3.4 Every proxied use reaches the audit log with method, path, operation, resource and amounts; test against the hash chain

## 4. SSH agent

- [x] 4.1 Agent protocol codec in `internal/engine/credential`: identities, sign request, extension, failure; tests against byte fixtures from draft-miller-ssh-agent (done with `golang.org/x/crypto/ssh/agent`, which owns the wire format; Legato's own code reads the session bind and the login data, tested with messages built as OpenSSH sends them, and checked against OpenSSH 10.2 in 4.9)
- [x] 4.2 `credential.SSHAgent` channel: one socket per execution under `<runtime dir>/legato/ssh/` (modes `0700` and `0600`), removed on close; `REQUEST_IDENTITIES` lists only the credentials' public keys; test that changes (`ADD_IDENTITY`, `LOCK`) are refused
- [x] 4.3 `session-bind@openssh.com`: parse, verify the server's signature over the session identifier, keep the binding per connection; tests with a forged signature and a second bind on one connection
- [x] 4.4 `ssh` inspector: parse `SSH_MSG_USERAUTH_REQUEST` sign data (user, service, method, host-bound variant), refuse a mismatched session identifier, refuse data that is not user authentication; table tests
- [x] 4.5 Host key to host names through `known_hosts`, plain and hashed entries, a `key:` resource for an unknown key; tests
- [x] 4.6 Hold a sign request up to 60 seconds, then `SSH_AGENT_FAILURE` with the approval left open; test with an injected clock
- [x] 4.7 Forwarded connections map to `ssh.forwarded-login`; test that the default rule denies them
- [x] 4.8 Embed `ssh.yaml` (window of 1 hour per host, forwarding denied); profile validation test
- [x] 4.9 Integration test (skipped without `ssh` and `sshd`): a test `sshd` on loopback, `ssh -o IdentitiesOnly=yes` through the execution's socket logs in when the rule allows and fails when it denies; a client without session bind is refused

## 5. Agent sessions and `applySSHAgent`

- [x] 5.1 `applySSHAgent` records the socket it chose; config test for each of its three sources (not needed: the session's `-e SSH_AUTH_SOCK` wins over the inherited value without it, and the three sources keep their existing tests)
- [x] 5.2 With an `ssh` credential configured, the agent service puts the execution's socket in the session environment; without one, the session keeps the inherited value; tests with `mockTmux`
- [x] 5.3 `[legato approvals]` message after a decision on a held sign request; service test

## 6. Docs

- [x] 6.1 `docs/ai/credentials.md`: the two channels, placeholders, meters, the three profiles, and the limits outside the sandbox
- [x] 6.2 `docs/ai/config.md`: `credential_channels`, and how `ssh_agent_socket` and 1Password auto-detection now apply to Legato's own processes only when an `ssh` credential exists
- [x] 6.3 Check in a scratch Legato: a shell agent's session gets a placeholder, the proxy's address and its own socket; `/v1/models` reaches ElevenLabs with the real key; account routes, unknown routes and a wrong key are refused; an `ssh` login waits for the person, logs in after approval, a second login in the window is signed without asking, a forwarded hop is refused; the socket goes within seconds of the agent ending

## 7. Follow-ups found in use

- [x] 7.1 `secrets.onepassword.token_file`, and agent sessions get `OP_SERVICE_ACCOUNT_TOKEN`, `LEGATO_SECRETS_PASSPHRASE` and proxied `env://` variables set empty; host processes drop the first two from their own environment
- [x] 7.2 Agent guidance for each credential by channel (MCP instructions for Claude Code, launch prompt for Codex and Chimera); a startup warning when `~/.ssh/config` names an `IdentityAgent` for every host; `docs/credentials.md` for setting up a host
- [x] 7.3 `credential_sources`: every SSH Key item of a 1Password vault is a credential, listed again while Legato runs; public keys from the item's field
- [x] 7.4 TUI approval overlay: `1`–`9` allow with that option; web approval tray: full-width 48 px choices on a phone
