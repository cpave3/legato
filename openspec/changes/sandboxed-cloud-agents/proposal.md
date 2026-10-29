## Why

Legato runs on a desktop workstation, so its agents stop when the desktop does. `legato serve` is already a complete headless runtime, so a Legato on an always-on VM could take jobs from the web UI, the phone app or a script, much like a hosted cloud agent. Two things stop that from being safe and useful:

- Jobs cannot be submitted in one call, and every repository must be cloned by hand before an agent can work in it.
- An unattended agent reads untrusted text (issues, PR comments, dependencies). Any credential in its environment or on its filesystem is one prompt injection away from misuse. Today every agent-side Legato process opens the database directly, so an agent can read everything Legato can, including the web auth token.

This change runs agents in a sandbox that holds no Legato state and no credential that can write. Agents can read sibling repositories. They push and open pull requests through the credential broker, which checks each ref and each write against the person's rules (`credential-broker`, `credential-channels`, `github-credentials`). The agent approvals and credentials plan revised decisions D5 to D9 of this change; see design.md.

## What Changes

- **Job submission by API.** `POST /api/agents/spawn` and `POST /api/tasks/{id}/agent` accept an optional `prompt`, delivered as the agent's first message. A built-in `legato-job` workflow (an `agent.run` step whose prompt and repository are inputs) gives jobs a durable run record.
- **Host broker.** The host Legato serves a broker socket. Activity hooks, `legato mcp serve` and the Claude Code and Codex bridges can act through it, authenticated by a capability token for one execution, instead of opening the store. A broker client can act only as its own execution.
- **Agent sandbox.** With `agents.sandbox.enabled`, Legato wraps each agent's launch command in a sandbox (bubblewrap first). The sandbox sees its working copy read-write, the repository cache read-only, the broker socket, and the agent tool's own state. It sees no Legato database, configuration, data directory, auth token or GitHub credential.
- **Repository cache.** Legato clones repositories on demand into `<data_dir>/repos/<owner>/<name>` with read tokens from the credential broker and fetches them before use. Each job gets its own clone that borrows the cache's objects (`git clone --reference`). A repository named by `owner/name` no longer needs a `primary_dir`.
- **Repository tools.** `legato mcp serve` gains `ensure_repo` and `refresh_repo`. Legato treats the agent's working copy as untrusted input: no host process that holds a credential runs git in it.
- **Pushes and pull requests through the credential broker.** An agent asks with `request_access` and then runs an ordinary `git push`. The push goes to Legato's HTTP proxy, whose `git` inspector checks each ref against the grant and sends the push with a write token made for that push alone. A pull request is a scoped REST write (`pr.create`) through the same proxy. These come from `github-credentials`; this change adds no `push_branch` or `open_pr` tool.
- **GitHub writes only through the broker.** **BREAKING (spec):** the read-only GitHub boundary changes from "Legato never mutates GitHub" to "the read client never mutates GitHub; writes go only through the credential broker's proxy, under the `github` profile's rules". There is no separate pull-request writer.
- **Delivery in phases**, each usable on its own: (1) a VM as the sandbox, with job submission; (2) the broker socket; (3) the sandbox launcher and repository cache, which is phase 7 of the agent approvals and credentials plan; (4) the repository tools, with pushes and pull requests through `github-credentials`.

## Capabilities

### New Capabilities
- `agent-job-submission`: Starting an agent with a first prompt through the API, and the built-in `legato-job` workflow for jobs with a durable run record.
- `agent-host-broker`: The broker socket, capability tokens for each execution, and the operations agent-side processes perform through it instead of through the store.
- `agent-sandbox`: Wrapping agent launches in a sandbox, what the sandbox can and cannot see, and how sandboxed launches fail.
- `repo-cache`: Cloning and fetching repositories on demand with host credentials, and the clone each job gets.
- `git-broker-tools`: The MCP tools for repositories, how a sandboxed agent pushes and opens pull requests through the credential broker, and treating an agent's working copy as untrusted.

### Modified Capabilities
- `github-pr-client`: The read-only boundary allows GitHub writes only through the credential broker's proxy. The read client stays GET-only. Credential secrecy now extends to the git credential helper used for host clones and fetches.

## Impact

- **Code:** `cmd/legato` (serve wiring, broker startup, sandbox launch wrapping); `internal/engine/ipc` or a new `internal/engine/broker` (socket server and client); `internal/engine/sandbox` (new: launch wrapping); `internal/engine/gitrepo` (new: cache, reference clones); `internal/service` (agent spawn with prompt, broker operations, repository service); `internal/cli` (hooks, `mcp serve` and bridges gain a broker client mode); `internal/server` (spawn `prompt` field).
- **APIs:** `prompt` on two spawn endpoints; new MCP tools; a new broker socket protocol; a new built-in workflow.
- **Config:** `agents.sandbox` (enabled, backend, extra mounts), `repositories.cache` settings. Pushes and pull requests need a `github` credential (`github-credentials`), not a `github.write` block.
- **Dependencies:** bubblewrap on the host when the sandbox is on. No new Go modules expected. Phase 4 needs `credential-broker`, `credential-channels` (the HTTP proxy) and `github-credentials`.
- **Unchanged:** a Legato with the sandbox off behaves as today: agent-side processes keep opening the store directly, and the read client stays GET-only.
- **Operations:** a VM deployment guide covering systemd, tmux, agent tool logins, Tailscale and a fine-grained token.
