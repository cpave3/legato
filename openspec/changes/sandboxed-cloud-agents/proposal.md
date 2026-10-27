## Why

Legato runs on a desktop workstation, so its agents stop when the desktop does. `legato serve` is already a complete headless runtime, so a Legato on an always-on VM could take jobs from the web UI, the phone app or a script, much like a hosted cloud agent. Two things stop that from being safe and useful:

- Jobs cannot be submitted in one call, and every repository must be cloned by hand before an agent can work in it.
- An unattended agent reads untrusted text (issues, PR comments, dependencies). Any credential in its environment or on its filesystem is one prompt injection away from misuse. Today every agent-side Legato process opens the database directly, so an agent can read everything Legato can, including the web auth token.

This change makes Legato the only holder of GitHub credentials and the broker for every operation that needs them. Agents run in a sandbox with no credentials, can read sibling repositories, and ask Legato to push and open pull requests through MCP tools that Legato checks against a policy.

## What Changes

- **Job submission by API.** `POST /api/agents/spawn` and `POST /api/tasks/{id}/agent` accept an optional `prompt`, delivered as the agent's first message. A built-in `legato-job` workflow (an `agent.run` step whose prompt and repository are inputs) gives jobs a durable run record.
- **Host broker.** The host Legato serves a broker socket. Activity hooks, `legato mcp serve` and the Claude Code and Codex bridges can act through it, authenticated by a capability token for one execution, instead of opening the store. A broker client can act only as its own execution.
- **Agent sandbox.** With `agents.sandbox.enabled`, Legato wraps each agent's launch command in a sandbox (bubblewrap first). The sandbox sees its working copy read-write, the repository cache read-only, the broker socket, and the agent tool's own state. It sees no Legato database, configuration, data directory, auth token or GitHub credential.
- **Repository cache.** Legato clones repositories on demand into `<data_dir>/repos/<owner>/<name>` with its own credentials and fetches them before use. Each job gets its own clone that borrows the cache's objects (`git clone --reference`). A repository named by `owner/name` no longer needs a `primary_dir`.
- **Git broker tools.** `legato mcp serve` gains `ensure_repo`, `refresh_repo`, `push_branch` and `open_pr`. Legato treats the agent's working copy as untrusted input: it copies the branch's objects into a repository only the host owns and pushes from there, so no hook or configuration the agent wrote ever runs with credentials.
- **Narrow pull-request writer.** A new GitHub writer, separate from the read client, can make one call: create a pull request in a repository on the allowlist. **BREAKING (spec):** the read-only GitHub boundary changes from "Legato never mutates GitHub" to "the read client never mutates GitHub; one separately guarded writer may create pull requests".
- **Delivery in phases**, each usable on its own: (1) a VM as the sandbox, with job submission; (2) the broker socket; (3) the sandbox launcher and repository cache; (4) the git broker tools and pull-request writer.

## Capabilities

### New Capabilities
- `agent-job-submission`: Starting an agent with a first prompt through the API, and the built-in `legato-job` workflow for jobs with a durable run record.
- `agent-host-broker`: The broker socket, capability tokens for each execution, and the operations agent-side processes perform through it instead of through the store.
- `agent-sandbox`: Wrapping agent launches in a sandbox, what the sandbox can and cannot see, and how sandboxed launches fail.
- `repo-cache`: Cloning and fetching repositories on demand with host credentials, and the clone each job gets.
- `git-broker-tools`: The MCP tools for repositories, pushes and pull requests, the push policy, and treating an agent's working copy as untrusted.

### Modified Capabilities
- `github-pr-client`: The read-only boundary allows one separate writer that creates pull requests in allowlisted repositories. The read client stays GET-only. Credential secrecy now extends to the git credential helper used for clones, fetches and pushes.

## Impact

- **Code:** `cmd/legato` (serve wiring, broker startup, sandbox launch wrapping); `internal/engine/ipc` or a new `internal/engine/broker` (socket server and client); `internal/engine/sandbox` (new: launch wrapping); `internal/engine/gitrepo` (new: cache, reference clones, branch import, push); `internal/engine/github` (new PR writer beside the guarded client); `internal/service` (agent spawn with prompt, broker operations, repository service, push policy); `internal/cli` (hooks, `mcp serve` and bridges gain a broker client mode); `internal/server` (spawn `prompt` field).
- **APIs:** `prompt` on two spawn endpoints; new MCP tools; a new broker socket protocol; a new built-in workflow.
- **Config:** `agents.sandbox` (enabled, backend, extra mounts), `repositories.cache` settings, `github.write` (allowlisted repositories, allowed branch prefix).
- **Dependencies:** bubblewrap on the host when the sandbox is on. No new Go modules expected.
- **Unchanged:** a Legato with the sandbox off behaves as today: agent-side processes keep opening the store directly, and the read client stays GET-only.
- **Operations:** a VM deployment guide covering systemd, tmux, agent tool logins, Tailscale and a fine-grained token.
