## Why

The `credential-broker` change gives an agent a credential through `vend` or `legato credential get`. Both hand the agent the secret itself. After that, Legato cannot see how the agent uses the secret, cannot limit it, and cannot take it back.

Two kinds of credential are used most by Legato's agents: HTTP API keys (ElevenLabs for libretto voices, and others) and SSH keys. Today `config.applySSHAgent` points every agent at the person's 1Password SSH agent. Each signature makes 1Password ask the person, and an agent that runs all the time cannot wait for that prompt.

This change adds two channels that keep the secret in Legato and check each use: an HTTP proxy for API keys and an SSH agent for SSH keys.

## What Changes

- **`http-proxy` channel.** Legato listens on loopback. An agent gets a base URL and a placeholder key that names its execution. The proxy replaces the placeholder with the real key and forwards the request to the credential's fixed upstream over TLS. The agent never holds the key.
- **`rest` inspector.** It classifies an HTTP request into an operation and a resource with a route table from the profile. It refuses a request that matches no route.
- **Meters.** The proxy counts `requests`, `bytes` and, where the profile names a field, `characters`. Rules can limit each meter for a period. The audit log records the amounts.
- **Shipped profiles `elevenlabs` and `api-key`.** `elevenlabs` puts the key in `xi-api-key` and meters the characters of text to speech. `api-key` is the same proxy for any HTTP API: the person gives the upstream and the place of the key.
- **`ssh-agent` channel.** Each execution gets its own agent socket as `SSH_AUTH_SOCK`. Legato reads the host key from OpenSSH's `session-bind@openssh.com` extension and the remote user from the sign request. It refuses a sign request that has no session bind, and it refuses agent forwarding by default.
- **`ssh` inspector and shipped `ssh` profile.** The inspector makes each sign request a use of `ssh.login` on `ssh://<user>@<host>`. A sign request that needs the person waits up to 60 seconds, and then Legato refuses it and messages the agent.
- **Agent sessions and 1Password.** When a credential uses the `ssh` profile, agent sessions get Legato's socket in place of the socket that `config.applySSHAgent` chose. Legato's own processes keep that socket.

## Capabilities

### New Capabilities
- `http-proxy-channel`: The loopback HTTP proxy, placeholder keys tied to an execution, the `rest` inspector, meters, and the `elevenlabs` and `api-key` profiles.
- `ssh-agent-channel`: The per-execution SSH agent socket, session binding, the `ssh` inspector, the hold of a sign request, forwarding, and the `ssh` profile.

### Modified Capabilities
- None. The channels and the inspectors implement interfaces that the `credential-broker` change defines.

## Impact

- **Code:** `internal/engine/credential` (new files: `httpproxy.go`, `inspect_rest.go`, `meter.go`, `sshagent.go`, `inspect_ssh.go`; engine packages may not import each other, so the channels and inspectors live beside the interfaces); `internal/engine/credential/profiles/` (embedded `elevenlabs.yaml`, `api-key.yaml`, `ssh.yaml`); `internal/service` (channel wiring at spawn, placeholder keys, meter totals for limits); `config/config.go` (`applySSHAgent` remembers the socket it chose); `cmd/legato` (proxy listener in both runtimes).
- **APIs:** a loopback HTTP listener for each runtime; an SSH agent socket for each execution; agent environment variables named by each profile.
- **Config:** `credentials[].settings` for `api-key` (`upstream`, `key`); `credential_channels.http_proxy.listen` (default: a random loopback port) and `credential_channels.ssh_agent.known_hosts` (default: `~/.ssh/known_hosts`).
- **Dependencies:** `golang.org/x/crypto/ssh` (new module) for key parsing, signatures and the agent wire format. OpenSSH 8.9 or later on the agent side for session binding.
- **Unchanged:** agents of a Legato with no `ssh` credential keep the `SSH_AUTH_SOCK` that `applySSHAgent` chose. `vend` and `legato credential get` keep working.
