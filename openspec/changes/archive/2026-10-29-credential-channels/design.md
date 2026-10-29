## Context

This is phase 3 of the agent approvals and credentials plan (`.legato/plans/agent-approvals-credentials/plan.md`). It builds on two changes:

- `agent-approvals` (phase 1): durable approval requests, `service.ApprovalService`, and the `[legato approvals]` message to agents.
- `credential-broker` (phase 2): the `Channel` and `Inspector` interfaces in `internal/engine/credential`, `service.CredentialBroker` with `Request`, `Authorize` and `Credential`, profiles, rules with approval modes and limits, grants, and the hash-chained audit log.

Phase 2 ships the `vend` channel and the `legato credential get` helper. Both give the agent the secret. This change adds the first two channels that never do.

**HTTP API keys.** libretto's paid voices use ElevenLabs. Today the key is in the agent's environment. An agent that reads untrusted text can send it anywhere.

**SSH keys.** `config.applySSHAgent` sets `SSH_AUTH_SOCK` in Legato's own process. It uses `ssh_agent_socket` from the configuration, or the inherited value, or `~/.1password/agent.sock` when that file exists. Every tmux session, and so every agent, inherits it. 1Password asks the person for each new use, and an agent that runs at night waits for nobody.

Engine packages may not import each other (`TestEnginePackagesDoNotImportSiblingEnginePackages`). The channels and inspectors therefore live in `internal/engine/credential`, beside the interfaces they implement.

## Goals / Non-Goals

**Goals:**
- An agent uses an HTTP API key or an SSH key without holding it.
- Each use is classified, checked against a grant, metered and recorded.
- A use that needs the person reaches the approval service and waits a bounded time.
- Shipped profiles for ElevenLabs, any HTTP API key, and SSH, with safe default rules.

**Non-Goals:**
- Containment. Outside the sandbox an agent can still read `~/.1password/agent.sock`, `~/.ssh` and its own environment. The sandbox (phase 7) closes that.
- An HTTPS proxy that decrypts traffic (a man in the middle with a Legato CA). The agent talks plain HTTP to loopback, and Legato talks TLS upstream.
- SSH certificates. The `ssh-cert` issuer is in `github-credentials`. This change signs with a stored key.
- SSH host key checking for the agent. `ssh` still checks `known_hosts` as before.

## Decisions

### D1. One proxy listener for each runtime, one path prefix for each credential

The host Legato (TUI or `serve`) listens on `127.0.0.1:<port>` (random unless `credential_channels.http_proxy.listen` is set). Each credential that uses `http-proxy` has the prefix `/c/<credential>/`. The agent's base URL is `http://127.0.0.1:<port>/c/<credential>`. The upstream is fixed by the profile or the credential's settings. The agent cannot choose another host, so the proxy is not an open proxy.

Plain HTTP on loopback is acceptable: the traffic never leaves the machine, and the sandbox shares the host's network namespace.

*Alternative:* one listener for each execution. Rejected: many ports for no gain, because the placeholder key already names the execution.

### D2. Placeholder keys name an execution and a credential

When a channel opens for an execution, Legato makes a placeholder: `lgk_` and 32 random bytes in base32. It stores only the SHA-256 hash with `(execution_id, credential)`. The agent gets the placeholder in the variables the profile names (for `elevenlabs`: `ELEVENLABS_API_KEY` and `ELEVENLABS_BASE_URL`).

The proxy reads the placeholder from the place the profile names for the key. A request with no placeholder, an unknown one, or one for another credential gets `401`. A placeholder dies when its execution becomes terminal.

A placeholder is not a secret of the person. A leaked placeholder works only through Legato, only for its execution, only within its grants, and every use is in the audit log.

### D3. The proxy replaces the key and forwards nothing else of Legato's

For each request the proxy:
1. Resolves the placeholder to an execution and a credential.
2. Asks the profile's inspector to classify the request (D4).
3. Calls `CredentialBroker.Authorize` with the operation, resource and amounts.
4. Removes the placeholder and every header that holds the key, then adds the real key from `CredentialBroker.Credential`.
5. Forwards the request to the upstream over TLS with certificate checks, and streams the response back.

The proxy does not follow redirects. It passes a `3xx` back unchanged; a redirect to another host reaches that host without a key. It removes hop-by-hop headers and `Proxy-*` headers. It strips the key from any response header that echoes it.

### D4. The `rest` inspector uses a route table from the profile

A profile lists routes: method, path pattern, operation, and an optional resource template.

```yaml
inspector: rest
routes:
  - { method: POST, path: "/v1/text-to-speech/{voice}", operation: tts.speak, resource: "voice/{voice}", meter: { characters: "$.text" } }
  - { method: GET,  path: "/v1/voices", operation: voices.read }
  - { method: "*",  path: "/v1/user/**", operation: account.manage }
```

The first matching route wins. A request that matches no route is refused with `403` and a JSON body that names the method and path. The resource is `<upstream host>/<resource template>`, or the request path when the route has no template.

The inspector never decodes more than the meter needs. It reads a body of at most 1 MiB for a characters meter; a larger body is refused with `413` for that route.

### D5. Meters count requests, bytes and characters

Every proxied request counts one `request`. `bytes` is the request body length plus the response body length, counted as the response streams. `characters` is the number of Unicode code points at a JSON path of the request body, when the route names one.

Requests and characters are known before forwarding, so the broker checks them against a rule's `limit` before the request leaves. Response bytes are known only afterwards; they are added to the totals when the response ends, and they count toward the next request. Totals are kept for each credential and period in the store, so a restart does not reset a daily limit.

This change follows the limit semantics of `credential-broker`: a use that would exceed a rule's limit is decided by the next matching rule. The `elevenlabs` profile relies on that (D6).

### D6. Shipped profiles `elevenlabs` and `api-key`

`elevenlabs`: upstream `https://api.elevenlabs.io`, key in header `xi-api-key`, routes for text to speech (metered by characters), voices and models (reads), and account, workspace and key management. Default rules:

| Operation | Approve |
| --- | --- |
| `tts.speak` | `auto`, limit 20,000 characters a day |
| `tts.speak` past the limit | `window`, 1 hour |
| `voices.read`, `models.read` | `auto` |
| `account.manage`, `keys.manage` | `deny` |

`api-key`: the person sets `settings.upstream` (an `https` URL) and `settings.key` (`{header: <name>, format: "Bearer {secret}"}` or `{query: <name>}`). Routes: every `GET` and `HEAD` is `http.read`, every other method is `http.write`; the resource is the path. Default rules: `http.read` is `session`, `http.write` is `request`.

### D7. An SSH agent socket for each execution

When a credential uses the `ssh` profile, the channel opens `<runtime dir>/legato/ssh/<execution_id>.sock` (mode `0600`, directory `0700`) and sets `SSH_AUTH_SOCK` to it in the tmux session's environment. The socket serves the SSH agent protocol (draft-miller-ssh-agent):

- `REQUEST_IDENTITIES` lists the public keys of the `ssh` credentials that the execution may use. It never lists the person's other keys.
- `SIGN_REQUEST` is checked (D8, D9) and signed with the key from the store.
- `ADD_IDENTITY`, `REMOVE_IDENTITY`, `LOCK` and the other changes are refused.
- `EXTENSION` accepts `session-bind@openssh.com` and refuses the others.

The socket is removed when the execution becomes terminal.

### D8. Session binding names the host, the sign data names the user

OpenSSH 8.9 and later send `session-bind@openssh.com` on each agent connection before they ask for a signature. It carries the server's host key, the session identifier, the server's signature over the identifier, and whether the connection is forwarded. Legato verifies that signature with the host key, and keeps the binding for that agent connection.

A sign request for user authentication carries the session identifier and a `SSH_MSG_USERAUTH_REQUEST` that names the remote user. Legato:
1. Refuses a sign request on a connection with no session bind, or whose session identifier is not the bound one.
2. Refuses data that is not a `publickey` user authentication request for the `ssh-connection` service.
3. Resolves the host key to host names through `credential_channels.ssh_agent.known_hosts` (default: the person's `~/.ssh/known_hosts`). A hashed entry matches by its hash. A host key with no entry becomes `ssh://<user>@key:<SHA256 fingerprint>`.
4. Builds the resource `ssh://<user>@<host>` (with `:<port>` when not 22) and authorizes the operation `ssh.login`.

*Alternative:* trust the host name that the agent's `ssh` would print. Rejected: the agent controls its own command line; the host key is what the server proved.

### D9. A sign request waits up to 60 seconds

When `Authorize` needs the person, the `ssh` inspector is a Holder: the sign request is held. sshd's `LoginGraceTime` defaults to 120 seconds, so 60 seconds fits inside it. If the person approves in time, Legato signs. If not, Legato answers `SSH_AGENT_FAILURE`, and the approval request stays open. When the person decides, the agent receives `[legato approvals] ap-… approved` and can run `ssh` again.

### D10. Forwarding is denied by default

A session bind with the forwarding flag set means a remote host is using this agent through `ssh -A`. The `ssh` profile maps such a sign request to `ssh.forwarded-login` and denies it by default. A person can add a rule for it.

### D11. Agent sessions and `applySSHAgent`

`applySSHAgent` keeps setting `SSH_AUTH_SOCK` in Legato's own process, so Legato's host-side git keeps working. It also records the socket it chose. When the configuration has at least one `ssh` credential, the agent service sets the execution's socket in the session environment (`tmux new-session -e`), which wins over the inherited value. Agents then never see 1Password prompts. With no `ssh` credential, nothing changes.

Legato's socket does not pass requests on to the person's agent. A pass-through would give every agent the person's keys again.

## Risks / Trade-offs

- [An agent ignores `SSH_AUTH_SOCK` and uses `~/.1password/agent.sock`] → Not preventable outside the sandbox. The UI and docs say so. 1Password still asks the person for that use.
- [A client without session binding (OpenSSH before 8.9, some libraries)] → Refused with a message in the agent's terminal that names the minimum version. Correctness wins over coverage.
- [A host key that resolves to several host names] → The resource lists the first; the rule matching uses every name, and the most restrictive decision wins.
- [A bug in a route table allows a use outside a grant] → Unknown routes are refused. Table tests cover every shipped route. The audit log records the raw method and path beside the classification.
- [A client that times out before 60 seconds] → The approval stays open, and the `[legato approvals]` message tells the agent to retry.
- [A leaked placeholder used by another local process] → It acts only as its execution, within its grants, and is recorded.
- [Response bytes counted after the fact can overshoot a byte limit by one response] → Documented. Characters and requests are checked before forwarding.

## Migration Plan

- Additive. No credential with an `http-proxy` or `ssh-agent` profile means no listener and no socket.
- Migrations: `credential_placeholders` (hash, execution, credential, created, revoked), `credential_meter_totals` (credential, meter, period start, amount).
- A person who moves an API key from `vend` to `elevenlabs` changes the credential's profile; agents started afterwards get placeholders.
- Rollback: remove the credential or change its profile. Placeholders of running agents stop working at once.

## Open Questions

- Do the ElevenLabs SDKs and libretto read a base URL from the environment? If not, libretto needs a flag or a variable for it.
- Can the proxy tie a loopback TCP connection to the calling process (for example through `/proc/net/tcp` on Linux) to refuse placeholders used by another process?
- Which `known_hosts` should be the default: the person's own, or a file Legato keeps for agents?
- Should `ssh` with `ProxyJump` be one resource (the last hop) or one per hop? Each hop sends its own session bind; the current design authorizes each hop.
