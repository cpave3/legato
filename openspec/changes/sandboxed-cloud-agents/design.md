> **Revised.** Decisions D5 to D9 were revised by the agent approvals and credentials plan (`.legato/plans/agent-approvals-credentials/plan.md`). Credentials now come from the credential broker (`credential-broker`), and pushes and pull requests go through its HTTP proxy with the `git` inspector under the `github` profile's rules (`credential-channels`, `github-credentials`). This change no longer adds `push_branch`, `open_pr`, a pull-request writer or `github.write`. Phase 3 of this change is phase 7 of that plan.

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
- Hold every GitHub credential that can write only in the host Legato process. An agent gets at most a read token for one repository and placeholders that work only through Legato.
- Let a sandboxed agent read sibling repositories, commit locally, ask for access, and push and open a pull request within the person's rules.
- Treat everything an agent writes as untrusted input to host code that holds credentials.
- Deliver in four phases, each usable on its own, with a Legato that has the sandbox off behaving as today.

**Non-Goals:**
- Federating two Legato instances (shared tasks, one board across hosts). Each instance keeps its own database.
- Multi-user or multi-tenant isolation. The sandbox protects credentials from prompt injection, not one person from another.
- Hiding the agent tool's own model credentials (Claude, Codex logins). They stay inside the sandbox. A model proxy is a later change.
- Network egress filtering. Agents keep network access for model APIs and package registries. They can leak code they can read, but not credentials.
- Merging pull requests, approving reviews, editing repository settings, or any GitHub write other than pushes and `pr.create` under the `github` profile.
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
| the execution's SSH agent socket (`credential-channels`) | as is |
| the `legato` binary | read-only |
| `agents.sandbox.mounts` from config, and resources given to the agent | read-only or read-write as configured |
| `config.yaml`, `data_dir` (database, auth token, certs, artifacts), `~/.config/gh`, `~/.ssh`, `~/.gitconfig` | **not mounted** |

The sandbox shares the host network namespace, so the bridges' loopback endpoints stay reachable from Legato, and the agent reaches Legato's credential proxy on loopback. A fresh PID namespace with `--die-with-parent` ties the agent's lifetime to the pane process.

Pasted images go to `<tmp>/legato-<uid>/pasted-images/<task>`. That folder is mounted read-only at the same path so agents can read them.

*Alternatives:* podman (heavier, but would also give a separate root filesystem); a separate Unix user (needs privilege escalation to spawn, and tmux ownership becomes awkward). bubblewrap is small, unprivileged with user namespaces, and already what some agent tools use. The backend is an interface, so podman can follow.

*Nested sandboxes:* the agent tools' own sandboxes limit the commands the model runs; Legato's hides credentials from the whole tool, its MCP servers and hooks. Where a tool's sandbox nests, both stay on.
- **Chimera** (`--sandbox`) runs tools in a Docker container. That needs the Docker or Podman socket, and whoever holds it can mount the whole home into a container, so Legato never mounts either socket into its sandbox and drops `--sandbox` (and `--sandbox-*`) from a sandboxed Chimera launch, with a log line. Legato's sandbox gives the same protection for credentials; Chimera's adds file isolation inside the working copy, which is lost.
- **Codex** (Landlock and seccomp, or bubblewrap in newer releases) should nest; verify. Claude Code's own bubblewrap sandbox nested in ours may fail where user namespaces cannot nest. Default to telling the tool not to sandbox itself when Legato's sandbox is on, and verify each tool (open question).

### D4a. The person can opt out per spawn; toolchains are mounted

The sandbox is the default once enabled, but the person can start one agent without it (**Run without sandbox** in the spawn dialogs, `sandbox: false` on the spawn endpoints). Mounts are fixed when bubblewrap starts, so an agent cannot be given a directory after it starts without a restart: adding a resource to a sandboxed agent restarts it, resuming its conversation, when the person agrees (`reload: true`), and is refused otherwise. An unsandboxed agent is how the person works across arbitrary directories without restarts. An agent cannot use the opt-out itself: spawning needs the web token, which the sandbox does not mount. Swarm and workflow agents are always sandboxed.

The person's toolchains are mounted read-only by default (`~/.local/bin`, mise, Go, linuxbrew, Rust, Bun), with package and build caches read-write, so agents can build and test. None of these hold credentials. `agents.sandbox.default_mounts: false` turns them off.

### D5. Repository cache, with one clone per job by reference

*Revised by `github-credentials`.*

`RepositoryService.Ensure(owner/name)` clones into `<data_dir>/repos/<owner>/<name>.git` as a bare mirror on first use, and fetches when it is older than `repositories.cache.max_age` (default 5 minutes), serialized per repository. Git runs on the host with a read token that the credential broker issues for that one repository (`git.read` under the `github` profile). Legato passes it to that one git process through a credential helper over an inherited file descriptor (`GIT_ASKPASS` pointing at `legato`). Nothing is written to disk.

A job gets `git clone --reference <cache> --no-checkout` into `<data_dir>/jobs/<execution_id>/<name>`, then a checkout of the requested ref. The job clone's `origin` stays `https://github.com/<owner>/<name>`. Inside the sandbox the `helper` channel of `github-credentials` gives git read tokens for fetches, and its `pushInsteadOf` sends every push to Legato's proxy. With no `github` credential, the job environment sends pushes to an unusable placeholder URL, so `git push` fails without contacting GitHub.

Inside the sandbox the cache is visible read-only. The `objects/info/alternates` path is the same inside and out because the cache is mounted at its host path, so the job clone reads shared objects and writes its own. Sibling repositories are readable as bare repositories (`git -C … log`, `git worktree` cannot be used against them). `ensure_repo` can add a checkout of a sibling into the job directory when an agent needs one.

*Alternative:* `git worktree` from a shared checkout (what `github.pr.prepare` does today). Rejected for sandboxed jobs: a worktree writes refs and objects into the main repository's `.git`, which would have to be writable to every job. `github.pr.prepare` keeps its worktrees for unsandboxed use.

### D6. The agent's working copy is untrusted: no host process with a credential runs git in it

*Revised by `github-credentials`.*

A git process that runs in the job clone reads `.git/config` and `.git/hooks`, which the agent controls (`core.hooksPath`, `core.sshCommand`, `credential.helper`, `pre-push`, `core.fsmonitor`). Such a process must never hold a credential that can write.

The agent pushes with its own `git push`. Its hooks and configuration run in its own processes, which hold no write credential. The push reaches Legato's HTTP proxy, where the `git` inspector reads only the ref update commands of the `git-receive-pack` request and passes the pack through unread. The write token is made for that one push inside the proxy and revoked after it (`github-credentials` D5 to D7). The host never runs git in the job clone to push.

The host still touches job clones in two places: the cleanup check for unpushed commits (D5, `repo-cache`) and `ensure_repo` checkouts of siblings. Both run git with `GIT_CONFIG_NOSYSTEM=1`, `GIT_CONFIG_GLOBAL=/dev/null`, `-c core.hooksPath=/dev/null`, `-c core.fsmonitor=false`, and no credential in their environment. The cleanup check reads ref tips as files where it can.

*Alternative:* the earlier design: read the branch tip from ref files, fetch its objects into a host-owned bare repository, check a fixed push policy, and push from there. Rejected now: it needed a second repository per repository and an object import from untrusted input, and the proxy sees the same ref updates without reading the agent's files.

### D7. Repository tools in `legato mcp serve`

*Revised by `github-credentials`.*

The tools forward to the broker, and the broker to `RepositoryService`:
- `ensure_repo {repo, ref?}` → clones the repository into the cache if needed and makes a job checkout. Returns the path.
- `refresh_repo {repo}` → fetches the cache.

There is no `push_branch` tool. An agent asks for `git.push` on its branch with `request_access` (from `credential-broker`) and then runs an ordinary `git push`. There is no `open_pr` tool. An agent opens a pull request with a REST request through the proxy (`pr.create`, D9).

The tools are listed only when a `github` credential is configured.

### D8. Push policy is the `github` profile's rules

*Revised by `github-credentials`.*

The fixed policy of `github.write` is replaced by the rules of the `github` profile, which the person can change:

| Earlier rule | Now |
| --- | --- |
| repository on `github.write.repositories` | repository in the credential's `settings.repositories`; others match no rule |
| branch starts with `legato/`, added by the tool | agents name branches `legato/<task-id>-<slug>` (guidance); `git.push` on `legato/**` is `session` |
| never the default branch or `main`, `master`, `release/*` | `git.push-default`, `git.push-tag` and `git.delete-ref` are `deny`; other branches are `request` and signed; a person adds `deny` rules for `release/*` |
| force push only with a lease on Legato's last push | dropped: the inspector cannot see history; the optional GitHub ruleset blocks force pushes |
| no file over 50 MiB | dropped: the pack passes through unread; GitHub's own file limit applies, and a `bytes` limit can cap a push |

Every push is recorded in the credential audit log, and as a task note naming the repository, branch, old and new tip.

### D9. Pull requests through the proxy, not a separate writer

*Revised by `github-credentials`.*

`POST /repos/{owner}/{repo}/pulls` through the proxy is the operation `pr.create`, scoped to the head branch. The proxy sends it with a `pull_requests: write` token made for that request and revoked after it. When GitHub answers with a new pull request, Legato links it to the task's PR tracking as a pinned PR.

The read client and its GET-only guard are unchanged and keep their tests. There is no `PRWriter`.

*Alternative:* the earlier `PRWriter` with its own transport guard. Rejected now: the proxy already makes per-request tokens and checks each write against a grant, and one write path is easier to state than two.

### D10. Job submission

`prompt` on `POST /api/agents/spawn` and `POST /api/tasks/{id}/agent` is sent after the session binds, through the same path the launch kickoff uses (native endpoint when the adapter binds one, typed otherwise). The built-in `legato-job` workflow has inputs `prompt` (required), `repo` (optional, `owner/name`), `ref` (optional), `agent` (optional) and `working_dir` (optional). It runs `repo.ensure` (a new executor, a no-op without `repo`) then `agent.run` in the job checkout, so a job has a run record, an execution and a place in Attention. The agent step has `keep_agent: true` and a `summary` output: the job's answer is in the run record, and the agent stays for the person to read it and go on. Built-in definitions are merged into every discovery (`workflowengine.WithBuiltIns`), and a file of the same name replaces one. An `agent.run` step whose agent, known from the inputs, is not a configured adapter is refused when the run starts: the agent service would otherwise run it on the default adapter without a word.

## Risks / Trade-offs

- [The broker becomes a single point of failure for sandboxed agents] → Hooks already tolerate a failed state report. The broker client times out in 2 seconds and the hook exits 0. MCP tools return a tool error that says Legato is unreachable.
- [Agent tools' own sandboxes conflict with bubblewrap] → Verify each tool. Turn the tool's own sandbox off through its flags when Legato's is on, and document it.
- [A host git process on a job clone runs agent-written config or hooks] → No such process holds a credential (D6), and each runs with system and global config disabled and hooks off. Tests plant `core.fsmonitor`, `core.hooksPath`, `core.sshCommand`, `credential.helper`, `uploadpack.packObjectsHook` and a `pre-push` hook and assert none runs.
- [A bug in the `git` inspector lets a push outside the grant through] → All-or-nothing pushes and refusal of anything unparsed (`github-credentials`), and the optional GitHub ruleset backstop.
- [The agent tool's model credential is inside the sandbox] → Accepted (non-goal). Recommend a separate account or key for the VM.
- [Code in the sandbox can reach the web API on loopback] → It needs the web token, which is not mounted. The broker token allows only its own execution.
- [Two migration paths (store-direct and broker) for every agent-side command] → One client interface with two implementations, and the same service calls behind both. Contract tests run each command against both.
- [Disk use of the cache and job clones] → Job clones are removed when their execution becomes terminal and has no unpushed commits. Otherwise they are kept for 7 days and listed in Attention.
- [The phase-1 VM with skip-permissions has broad power] → Documented as a dedicated, disposable VM with a fine-grained token and branch protection.

## Migration Plan

- Every new behaviour is behind configuration: `agents.sandbox.enabled` (default false) and a `github` credential (none by default). An upgraded Legato with neither behaves exactly as today.
- New migrations: an execution capability-token hash column and the job clone table (execution, repository, path, created, cleaned). All additive. Pushes are recorded in the credential audit log of `credential-broker`, not in a table of this change.
- Rollback: turn the sandbox off. Running sandboxed agents keep their broker tokens until they end. Job clones stay on disk until swept.

## Verified on the person's machine (2026-10-30)

Fedora Silverblue, bubblewrap 0.11, unprivileged user namespaces:
- Inside, none of `~/.ssh`, `~/.config/gh`, `~/.gitconfig`, Legato's configuration, op token file and database, `~/.1password`, the session bus, Podman or Docker sockets exists; of Legato's runtime folder only `broker/` is seen. `OP_SERVICE_ACCOUNT_TOKEN` is unset.
- Go, Node (mise), git (commits carry the person's identity from the environment), the network and the `legato` binary work.
- Claude Code 2.1.312 starts, its login works (a headless request answered), and its own state is read-write. A global hook of the person's pointing into `~/.var/app/…` (GitKraken, a Flatpak) fails harmlessly: that folder is not mounted.
- Codex 0.157.1 starts and is logged in.
- Chimera, linked from its checkout with `pnpm link`, needs that checkout: Legato finds each agent tool's installation at start (the folder a link leads to, or the git checkout a launcher script runs) and mounts it read-only.
- `/home` is a link to `var/home` there: every path is resolved before it is checked or mounted, so a path through the link can neither hide a credential nor clash with the system's own links.

### Found while building (2026-10-30)

- **ssh refuses root's files as nobody's.** bwrap is not setuid, so it always makes a user namespace in which root's files are owned by the overflow user; ssh refuses such a system configuration and did not start. Each sandbox gets copies of the system ssh configuration, owned by the person and mounted over the originals (following `Include`, which on Fedora reaches `/usr/share/crypto-policies`), with the SSH agent's `known_hosts` mounted where the copy names it.
- **ssh origins fetch with read tokens.** A clone whose `origin` is `git@github.com:…` fetched over ssh, which a sandbox has no key for, while the proxy serves pushes only. With a `github` credential a sandboxed session also rewrites the ssh forms to `https://github.com/` for fetches.
- **A typed line is cut at 4096 bytes.** The bwrap prefix grew past what a terminal takes in one line before the shell reads it, and the agent did not start. The options now go to a file of the execution's, read with `--args`.
- **One tool's installation is not another's business.** Mounting every tool's installation showed a Chimera checkout to Claude Code agents; installations are now mounted by agent kind.
- **Sessions must resume across the sandbox.** State folders are mounted at their host paths, including one moved by `CLAUDE_CONFIG_DIR` or `CODEX_HOME`, so a conversation started on either side resumes on the other. Agents of one tool share that state (all Claude Code transcripts are in `~/.claude`); the sandbox keeps secrets from agents, not agents from each other.

## Open Questions

- Does Claude Code's own sandbox run inside bubblewrap on a typical VM kernel, or must it always be turned off?
- Should pull requests opened through the proxy be marked as written by an agent (a footer), like the review-publishing skill does? The proxy would have to change the request body.
- Should the broker socket also serve the TUI and `serve` on the desktop (no sandbox), so there is one agent-side path everywhere? This would retire the store-direct mode later.
- Should jobs on the VM be visible from the desktop Legato (read-only remote agents list)? Out of scope here, but the broker protocol should not rule it out.
