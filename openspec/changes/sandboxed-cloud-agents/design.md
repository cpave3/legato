## Context

`legato serve` (`runServeCmd` in `cmd/legato/main.go`) already builds every service the TUI builds: agents on tmux, native endpoint recovery, swarms, plans, reviews, workflows and their triggers, Attention, Jira sync, GitHub PR tracking and ntfy. The web app and the phone app can switch between servers. A VM running `legato serve` is therefore already a remote Legato. What it lacks is described below.

**Jobs cannot be started in one call.** `POST /api/agents/spawn` takes a title, agent tool, working directory and resources, and no prompt. A job is a spawn followed by a WebSocket `agent_message`.

**Repositories must already be on disk.** Repository presets point at a `primary_dir`. `github.pr.prepare` requires one and makes a worktree from it. Nothing in Legato clones.

**Agents can read everything Legato can.** Several Legato processes run inside an agent's pane, as the same user, and open the SQLite store directly:

| Process | What it writes |
| --- | --- |
| Activity hooks (`legato agent state`, `agent stopped`, `session-created`, `permission-resolved`) | `agent_sessions.activity`, execution activity, resume IDs |
| `legato mcp serve` | UI items, image and video copies, task notes; reads task context; messages peers |
| `legato agent bridge`, `legato agent codex-bridge` | binds and unbinds the session's native endpoint, saves resume identity |
| `legato agent allow-read` | reads nothing from the store (unaffected) |

Each of them also reads `config.yaml`, which can hold `github.token`, `jira.api_token` and the Manticore key. The data directory holds the web auth token. An agent that is told to read those files can.

**The GitHub read client is GET-only by design**, enforced by a transport guard (`github-pr-client` spec, "Read-only GitHub boundary"). Credentials are held in memory and never persisted.

Stakeholders: one person running Legato for themselves, on a desktop and on a VM they own. This is not multi-tenant.

## Goals / Non-Goals

**Goals:**
- Submit a job (prompt, agent tool, repository) to a headless Legato in one HTTP call, and follow it in the web UI.
- Hold the GitHub credential only in the host Legato process. No agent process, file or environment visible to an agent contains it.
- Let a sandboxed agent read sibling repositories, commit locally, and ask Legato to push and open a pull request under a policy.
- Treat everything an agent writes as untrusted input to host code that holds credentials.
- Deliver in four phases, each usable on its own, with a Legato that has the sandbox off behaving as today.

**Non-Goals:**
- Federating two Legato instances (shared tasks, one board across hosts). Each instance keeps its own database.
- Multi-user or multi-tenant isolation. The sandbox protects credentials from prompt injection, not one person from another.
- Hiding the agent tool's own model credentials (Claude, Codex logins). They stay inside the sandbox. A model proxy is a later change.
- Network egress filtering. Agents keep network access for model APIs and package registries. They can leak code they can read, but not credentials.
- Merging pull requests, approving reviews, editing repository settings, or any GitHub write other than creating a pull request.
- Scheduled (time-based) workflow triggers. Separate change.
- macOS sandboxing. Linux (bubblewrap) only in this change.

## Decisions

### D1. Phase 1 needs no sandbox: the VM is the sandbox

Phase 1 adds `prompt` to the spawn endpoints, the `legato-job` workflow and a deployment guide. On a dedicated VM with a fine-grained token, agents run with their tools' skip-permission flags and the VM is the boundary. This proves the remote-jobs loop before the larger work starts.

*Alternative:* build the sandbox first. Rejected: it delays any usable result by the largest piece of work.

### D2. One broker socket served by the host, with a capability token per execution

The host Legato (TUI or `serve`) listens on a Unix socket at `<runtime dir>/legato/broker-<pid>.sock` (one per instance, like the broadcast sockets, and passed to each agent by the instance that spawned it). Requests are newline-delimited JSON (`{id, token, op, args}`), with replies `{id, result | error}`. This matches the IPC framing, but each connection is request/reply rather than fire-and-forget.

When Legato spawns an agent it mints a random 32-byte token for that execution and stores only its SHA-256 hash with the execution. It passes the token to the pane as `LEGATO_BROKER_TOKEN`, beside `LEGATO_BROKER_SOCKET`. The broker resolves a token to exactly one `(task_id, execution_id)` and rejects any request naming another identity. A token dies when its execution becomes terminal.

*Why a socket, not the web API:* the web API's token grants everything. A narrow socket with a token per execution gives least privilege, and a Unix socket can be bind-mounted into the sandbox without opening a TCP port to it.

*Why hash the token:* the database is out of the sandbox's reach, but hashing costs nothing and makes a leaked database copy useless for impersonation.

### D3. Agent-side processes choose a mode from their environment

A shared `brokerclient` decides how each agent-side command reaches Legato:
- `LEGATO_BROKER_SOCKET` and `LEGATO_BROKER_TOKEN` set: every store read and write goes through the broker. The process never opens the store or reads `config.yaml`.
- Not set: the current behaviour (open the store directly). This keeps unsandboxed sessions and already-installed hooks working.

Operations are named for what callers need, not for tables: `agent.state`, `agent.stopped`, `agent.session_created`, `agent.permission_resolved`, `endpoint.bind`, `endpoint.unbind`, `resume.save`, `ui.show`, `task.context`, `task.note`, `peers.list`, `peers.message`, `peers.broadcast`, plus the git operations of D7. Services already implement these. The broker is a thin dispatcher over them, so policy stays in one place.

Image and video files that `show_*` tools name are read inside the sandbox and must reach the host's copy store. The broker accepts file content inline (images, up to the existing 25 MiB limit), or a path under a mount the host can also see (videos, which can be up to 1 GiB). See D5.

*Alternative:* run the MCP server and hooks on the host and pipe stdio into the sandbox. Rejected: agent tools start MCP servers and hooks as their own children, so they would have to be redirected per tool. The broker client works for any tool.

### D4. Sandbox with bubblewrap, wrapping the launch command

`agents.sandbox.backend: bwrap` makes the agent service prefix the adapter's launch command with a `bwrap` invocation that Legato builds. The tmux session and the pane stay on the host. Only the agent tool and its children are inside. The mounts:

| Path | Access |
| --- | --- |
| `/usr`, `/bin`, `/lib*`, `/etc` (minus secrets), `/tmp` (private) | read-only system, private tmp |
| the job's working copy | read-write |
| `<data_dir>/repos` (the cache) | read-only |
| the agent tool's own state (`~/.claude`, `~/.codex`, `~/.chimera`, Manticore env) | read-write |
| the broker socket | as is |
| the `legato` binary | read-only |
| `agents.sandbox.mounts` from config, and resources given to the agent | read-only or read-write as configured |
| `config.yaml`, `data_dir` (database, auth token, certs, artifacts), `~/.config/gh`, `~/.ssh`, `~/.gitconfig` | **not mounted** |

The sandbox shares the host network namespace, so the bridges' loopback endpoints stay reachable from Legato. A fresh PID namespace with `--die-with-parent` ties the agent's lifetime to the pane process.

Pasted images go to `<tmp>/legato-<uid>/pasted-images/<task>`. That folder is mounted read-only at the same path so agents can read them.

*Alternatives:* podman (heavier, but would also give a separate root filesystem); a separate Unix user (needs privilege escalation to spawn, and tmux ownership becomes awkward). bubblewrap is small, unprivileged with user namespaces, and already what some agent tools use. The backend is an interface, so podman can follow.

*Nested sandboxes:* Codex (Landlock and seccomp) should nest. Claude Code's own bubblewrap sandbox nested in ours may fail where user namespaces cannot nest. Default to telling the tool not to sandbox itself when Legato's sandbox is on, and verify each tool (open question).

### D5. Repository cache, with one clone per job by reference

`RepositoryService.Ensure(owner/name)` clones into `<data_dir>/repos/<owner>/<name>.git` as a bare mirror on first use, and fetches when it is older than `repositories.cache.max_age` (default 5 minutes), serialized per repository. Git runs on the host with a credential helper that Legato supplies over a pipe for that one command (`GIT_ASKPASS` pointing at `legato` with the credential in an inherited file descriptor). Nothing is written to disk.

A job gets `git clone --reference <cache> --no-checkout` into `<data_dir>/jobs/<execution_id>/<name>`, then a checkout of the requested ref. The job clone's `origin` is rewritten to a placeholder (`legato-broker:owner/name`) so `git push` from inside the sandbox fails clearly instead of prompting.

Inside the sandbox the cache is visible read-only. The `objects/info/alternates` path is the same inside and out because the cache is mounted at its host path, so the job clone reads shared objects and writes its own. Sibling repositories are readable as bare repositories (`git -C … log`, `git worktree` cannot be used against them). `ensure_repo` can add a checkout of a sibling into the job directory when an agent needs one.

*Alternative:* `git worktree` from a shared checkout (what `github.pr.prepare` does today). Rejected for sandboxed jobs: a worktree writes refs and objects into the main repository's `.git`, which would have to be writable to every job. `github.pr.prepare` keeps its worktrees for unsandboxed use.

### D6. The agent's working copy is untrusted: push from a repository only the host owns

A naive `git push` run by the host inside the job clone would read `.git/config` and `.git/hooks` that the agent controls (`core.hooksPath`, `core.sshCommand`, `credential.helper`, `pre-push`, `core.fsmonitor`) with the credential in reach. Instead, `push_branch`:
1. Resolves the branch tip by reading `refs/heads/<branch>` and the packed refs as files, not by running git in the job clone.
2. Runs, in a host-owned bare repository (`<data_dir>/push/<owner>/<name>.git`, with no hooks and fixed config), `git -c core.hooksPath=/dev/null -c protocol.file.allow=always fetch --no-tags <job clone path> <sha>`. Only the object transfer touches the job clone. Legato also sets `GIT_CONFIG_NOSYSTEM=1` and `GIT_CONFIG_GLOBAL=/dev/null`, runs it without the credential in its environment, and runs `git fsck --connectivity-only` on the received objects. A local-path fetch runs `upload-pack` against the job clone; which of that repository's configuration it reads is settled by tests before this ships (Open Questions).
3. Checks the push policy (D8) against the target ref and the commit range.
4. Pushes from the host-owned repository with the brokered credential.

*Alternative:* have the agent produce a bundle (`git bundle create`) and import it. Viable and even narrower. Kept as the fallback if a local-path fetch turns out to read any configuration it should not (open question).

### D7. Git broker tools in `legato mcp serve`

The tools forward to the broker, and the broker to `RepositoryService` and `GitBrokerService`:
- `ensure_repo {repo, ref?}` → clones the repository into the cache if needed and makes a job checkout. Returns the path.
- `refresh_repo {repo}` → fetches the cache.
- `push_branch {repo, branch, force?}` → D6, under D8.
- `open_pr {repo, branch, base?, title, body, draft?}` → D9.

They are listed only when the host has a GitHub credential and `github.write.repositories` is not empty, and only in sandboxed sessions or when `github.write.unsandboxed: true`.

### D8. Push policy

- The repository is on `github.write.repositories`.
- The target branch starts with `github.write.branch_prefix` (default `legato/`). The tool adds the prefix when the name lacks it.
- Never the repository's default branch or a branch matching `github.write.protected` (default `main`, `master`, `release/*`).
- Force pushes only with `--force-with-lease` against the tip Legato pushed last for that branch. A branch Legato did not create cannot be force-pushed.
- The pushed commit range contains no file larger than 50 MiB (a guard against pushing caches by mistake).

Every push is recorded as a task note and an execution event, so the person can see what an agent pushed.

### D9. A separate pull-request writer beside the read client

`internal/engine/github` gains `PRWriter` with one method, `CreatePullRequest(repo, head, base, title, body, draft)`. It has its own transport guard: it allows only `POST /repos/{owner}/{repo}/pulls` where `{owner}/{repo}` is on the allowlist given at construction, and rejects every other method and path before dispatch. The read client and its GET-only guard are unchanged and keep their tests. The writer shares the in-memory credential source.

*Alternative:* lift the GET-only guard. Rejected: the read client's guarantee is easy to state and to test, and a second, narrow guard keeps it.

### D10. Job submission

`prompt` on `POST /api/agents/spawn` and `POST /api/tasks/{id}/agent` is sent after the session binds, through the same path the launch kickoff uses (native endpoint when the adapter binds one, typed otherwise). The built-in `legato-job` workflow has inputs `prompt` (required), `repo` (optional, `owner/name`), `ref` (optional), `agent` (optional). It runs `repo.ensure` (a new executor, a no-op without `repo`) then `agent.run` in the job checkout, so a job has a run record, an execution and a place in Attention.

## Risks / Trade-offs

- [The broker becomes a single point of failure for sandboxed agents] → Hooks already tolerate a failed state report. The broker client times out in 2 seconds and the hook exits 0. MCP tools return a tool error that says Legato is unreachable.
- [Agent tools' own sandboxes conflict with bubblewrap] → Verify each tool. Turn the tool's own sandbox off through its flags when Legato's is on, and document it.
- [A local-path `git fetch` from the job clone reads agent-written config or runs agent-written code] → Fixed config environment, no hooks path, fsck. Prove it with tests that plant `core.fsmonitor`, `core.hooksPath`, `uploadpack.packObjectsHook` and a `pre-push` hook. Fall back to bundles (D6) if any of them fires.
- [The agent tool's model credential is inside the sandbox] → Accepted (non-goal). Recommend a separate account or key for the VM.
- [Code in the sandbox can reach the web API on loopback] → It needs the web token, which is not mounted. The broker token allows only its own execution.
- [Two migration paths (store-direct and broker) for every agent-side command] → One client interface with two implementations, and the same service calls behind both. Contract tests run each command against both.
- [Disk use of the cache and job clones] → Job clones are removed when their execution becomes terminal and has no unpushed commits. Otherwise they are kept for 7 days and listed in Attention.
- [The phase-1 VM with skip-permissions has broad power] → Documented as a dedicated, disposable VM with a fine-grained token and branch protection.

## Migration Plan

- Every new behaviour is behind configuration: `agents.sandbox.enabled` (default false) and `github.write.repositories` (default empty). An upgraded Legato with neither set behaves exactly as today.
- New migrations: an execution capability-token hash column, the job clone table (execution, repository, path, created, cleaned), and a push log table. All additive.
- Rollback: turn the sandbox off. Running sandboxed agents keep their broker tokens until they end. Job clones stay on disk until swept.

## Open Questions

- Does Claude Code's own sandbox run inside bubblewrap on a typical VM kernel, or must it always be turned off?
- Exactly which repository configuration does `git fetch` from a local path read in the source repository? Settle with planted-config tests before choosing between fetch and bundle.
- Should `open_pr` bodies be marked as written by an agent (a footer), like the review-publishing skill does?
- Should the broker socket also serve the TUI and `serve` on the desktop (no sandbox), so there is one agent-side path everywhere? This would retire the store-direct mode later.
- Should jobs on the VM be visible from the desktop Legato (read-only remote agents list)? Out of scope here, but the broker protocol should not rule it out.
