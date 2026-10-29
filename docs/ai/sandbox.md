# Sandbox

With `agents.sandbox.enabled`, Legato starts each agent tool inside bubblewrap. The agent sees its work, its tool's own state, the person's toolchains, and Legato's sockets for it; it does not see the person's credentials, Legato's database or configuration, or sockets that give the host away. Its agent-side processes (hooks, `legato mcp serve`, the native bridges, the `legato` commands it runs) reach the host Legato through the broker instead (see [CLI](cli.md) § The host broker). The design is in `openspec/changes/sandboxed-cloud-agents` (phases 2 and 3).

The sandbox keeps secrets from agents. It does not filter the network, and it does not hide the agent tool's own model login, which the tool needs.

## How a spawn is wrapped

- `SpawnAgent` decides before any tmux session exists: a sandbox that cannot be built (no `bwrap`, a forbidden path) fails the spawn and never leaves an unsandboxed agent.
- The pane runs the person's shell, on the host, with the session's environment. The launch command typed into it is prefixed with `bwrap --args 3 3<<runtime>/legato/sandbox/<execution>/bwrap.args --`, so `$(cat $LEGATO_ROLE_PROMPT_FILE)` is still expanded by the host shell. A shell agent gets the same prefix before `${SHELL:-/bin/bash}`.
- The sandbox's options are in that file (NUL-separated, mode `0600`), not on the typed line: a terminal cuts a typed line at 4096 bytes before the shell reads it, and a sandbox's mounts easily pass that. bwrap closes the file before it starts the agent.
- The adapter is told its tool runs in the sandbox (`LEGATO_SANDBOXED=1` in the environment it builds the launch command from, not in the pane's). Chimera then drops its own `--sandbox` and `--sandbox-*` flags: its sandbox is a Docker container, and the Docker or Podman socket would undo Legato's.
- `bwrap` runs with the host's network and user, fresh PID, IPC, UTS and cgroup namespaces, and `--die-with-parent`: everything inside ends with the pane.
- `agent_sessions.sandboxed` (migration 075) records which sessions run inside.

## What a sandbox sees

Built by `service.BuildSandboxPlan` from the host's real paths (links resolved first: on a host whose `/home` is a link to `var/home`, every path is checked and mounted where it really is).

| Seen | How |
| --- | --- |
| `/usr`, `/etc`, `/opt`, `/run/systemd/resolve`, and `/bin`, `/lib*`, `/home` as the same links as on the host | read-only |
| The home folder | an empty, writable folder; only what is listed here is in it |
| `/tmp` | the execution's own folder on the host, `<runtime>/legato/sandbox/<execution>/tmp`, so the host can read what an agent writes there |
| The working directory, and a worktree's main `.git` | read-write |
| The agent tool's own state (`~/.claude`, `~/.claude.json`, `~/.codex`, `~/.chimera`, …), so sessions are the same files inside and out and resume either way | read-write |
| A state folder a variable moves elsewhere: `CLAUDE_CONFIG_DIR`, `CODEX_HOME` (the session's, such as a resumed session's storage folder, else the one Legato inherited) | read-write, made when missing; refused when inside a forbidden path |
| The agent tool's installation (where its command's link leads, or the git checkout a launcher script runs, as for Chimera linked with `pnpm link`) | read-only, found when Legato starts; only the installation of the tool the agent runs (`ToolInstalls`, by agent kind), so a Claude Code agent does not see a Chimera checkout |
| Toolchains (`~/.local/bin`, mise, `~/go/bin`, linuxbrew, Rust, Bun, …) | read-only, with `default_mounts` |
| Package and build caches (`~/go/pkg/mod`, `~/.cache/go-build`, npm, pnpm, Corepack, pip, uv, cargo registry, Playwright) | read-write, with `default_mounts` |
| `<data_dir>/claude-mod`, `<data_dir>/repos` | read-only |
| The broker's folder (`<runtime>/legato/broker/`), the execution's SSH agent socket, the `gh` wrapper folder and its empty configuration, the `legato` binary, the role prompt and brief folder, the task's pasted images | read-only |
| `agents.sandbox.mounts`, and the resources the agent is given | as configured |
| The system ssh configuration (`/etc/ssh/ssh_config` and every file it includes) | read-only, as copies the person owns in `<runtime>/legato/sandbox/<execution>/ssh/`, each mounted over the real path of its original (see SSH below) |
| `credential_channels.ssh_agent.known_hosts` | read-only, where the copied `ssh_config` names it as `UserKnownHostsFile` |

Never seen: Legato's configuration file and data folder, `~/.config/legato` (the 1Password token file), `~/.ssh`, `~/.config/gh`, `~/.gitconfig`, git credential stores, `~/.netrc`, `~/.1password`, `~/.config/op`, `~/.docker`, `~/.aws`, `~/.kube`, `~/.gnupg`, password stores and keyrings, `~/.npmrc`, `~/.pypirc`, the Docker and Podman sockets, and the person's runtime folder (`/run/user/<uid>`: the session bus, every Legato's IPC socket). A mount that is inside one of these is refused; one that holds one has it covered with an empty file or folder.

The environment loses the variables secrets come from (`OP_SERVICE_ACCOUNT_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN` unless it is Legato's stand-in, the session bus, `TMUX`, `DOCKER_HOST`, cloud and registry tokens, …) and `SSH_AUTH_SOCK` unless it is the execution's own socket. It gains `LEGATO_SANDBOXED=1`, `TMPDIR=/tmp`, and the person's git name and address (read from `git config --global` at start), since `~/.gitconfig` is not there.

## SSH and git

bwrap is not setuid on most systems, so it always makes a user namespace, and there every file root owns shows as owned by `nobody`. ssh refuses a system configuration file that belongs to neither root nor the person (`Bad owner or permissions on /etc/ssh/ssh_config.d/…`), so without help ssh does not start at all.

- **The configuration.** Before each sandboxed spawn, `writeSandboxSSHConfig` (`internal/service/sandbox_ssh.go`) copies `/etc/ssh/ssh_config` and every file it includes, following `Include` lines (relative to `/etc/ssh`, globs expanded, links followed, `~` skipped, 16 deep) as ssh would. The copies are the person's, mode `0600`, and each is mounted over the real path of its original (on Fedora: `ssh_config.d/50-redhat.conf`, the systemd proxy file, `/usr/share/crypto-policies/…/openssh.txt`).
- **Known hosts.** The copy of `ssh_config` starts with `UserKnownHostsFile <that folder>/known_hosts`, and `credential_channels.ssh_agent.known_hosts` is mounted there read-only: the file Legato's SSH agent names hosts by. It holds host keys, not secrets. A host it does not list cannot be verified, and the agent's `ssh` stops there.
- **Keys.** None: `~/.ssh` stays hidden. A login is signed by Legato's SSH agent socket for the execution, when an `ssh` credential covers the host (see [Credentials](credentials.md) § SSH agent); otherwise ssh starts and finds no key.
- **Git fetches from GitHub.** With a `github` credential, a sandboxed session also sends fetches from `git@github.com:` and `ssh://git@github.com/` to `https://github.com/` (`sandboxGitFetches`, added after the session's `GIT_CONFIG_*`), where Legato's credential helper gives read tokens; pushes from those forms already go to the proxy through `pushInsteadOf`. So a clone whose `origin` is an ssh URL fetches and pushes with no ssh key. The repository must be in the credential's `settings.repositories`. The proxy's `/git/` route serves pushes only and answers fetches `403`.

## What sandboxes share

Each sandbox has its own namespaces, `/tmp`, ssh copies and mounts; nothing is kept from one to the next. What two sandboxes see in common is what both are given from the host: the person's toolchains and caches, the system, and each tool's own state. All Claude Code agents share `~/.claude` read-write (their login, and every project's transcripts), as all Codex agents share `~/.codex`. The sandbox keeps secrets from agents; it does not keep agents of one tool from each other's state.

## Commands an agent runs

In the sandbox, `legato task|plan|review|swarm|learn …`, `credential request|get|list`, `agent peers|message|broadcast`, `workflow step|show` and a few read-only listings are forwarded through the broker (`cli.run`, `cmd/legato/cli_forward.go`): the host runs its own binary with the caller's task and execution from the token (never from the agent), the agent's working directory, its stdin when the command reads `-`, and paths under the sandbox's `/tmp` mapped to the session's host folder. `credential exec` fetches the value that way and runs its command inside. Host-only commands (`device`, `secret`, `audit`, `hooks`, `auth`, `serve`, …) are not forwarded, and `openStore` refuses in the sandbox.

## Leaving it

- **One agent outside.** The spawn dialogs offer **Run without sandbox** (web, unticked each time) and `ctrl+u` (TUI); the API takes `sandbox: false` on `POST /api/agents/spawn` and `POST /api/tasks/{id}/agent`. Such an agent is marked `unsandboxed` (`/api/agents`). Swarm and workflow agents are always sandboxed. **Reload agent** keeps the choice.
- **Folders added later.** A sandbox's mounts are fixed when it starts. Adding a resource to a running sandboxed agent is refused (`409`) unless the request asks to reload (`{names, reload: true}`): the agent is then restarted with its resources and the new ones, resuming its conversation. The web Resources button explains this and asks first.

## Verified

On Fedora Silverblue with bubblewrap 0.11: Claude Code 2.1.312, Codex 0.157.1 and Chimera start and are logged in inside; a sandboxed Claude Code session binds its bridge through the broker (native panel), uses Legato's MCP tools and forwarded commands, and finds `~/.ssh` and `~/.config/legato` absent. Integration tests with a real `bwrap` (skipped without it) check the same walls and that everything ends with the pane (`internal/service/sandbox_bwrap_test.go`).

## Known limits

- A global hook of the person's agent tool that runs something outside the mounts (a Flatpak's data folder, as GitKraken's `gk` hooks for Claude Code are) fails inside, on every tool call; the tool reports it as a hook error. Mount the folder with `agents.sandbox.mounts`, or remove the hook.
- An SSH socket or a socket file reopened by a new channel host while an agent runs is not seen by that agent until it is reloaded.
- Not built yet: the repository cache and job clones (phase 3 tasks 3.8–3.12), so `legato-job` cannot check a repository out yet, and repository tools (phase 4). Running Legato on a server of its own, with or without the sandbox: [Agents on a server of their own](../cloud-agents.md). Progress is kept in `openspec/changes/sandboxed-cloud-agents/tasks.md` § Status.
