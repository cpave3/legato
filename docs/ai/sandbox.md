# Sandbox

With `agents.sandbox.enabled`, Legato starts each agent tool inside bubblewrap. The agent sees its work, its tool's own state, the person's toolchains, and Legato's sockets for it; it does not see the person's credentials, Legato's database or configuration, or sockets that give the host away. Its agent-side processes (hooks, `legato mcp serve`, the native bridges, the `legato` commands it runs) reach the host Legato through the broker instead (see [CLI](cli.md) § The host broker). The design is in `openspec/changes/sandboxed-cloud-agents` (phases 2 and 3).

The sandbox keeps secrets from agents. It does not filter the network, and it does not hide the agent tool's own model login, which the tool needs.

## How a spawn is wrapped

- `SpawnAgent` decides before any tmux session exists: a sandbox that cannot be built (no `bwrap`, a forbidden path) fails the spawn and never leaves an unsandboxed agent.
- The pane runs the person's shell, on the host, with the session's environment. The launch command typed into it is prefixed with `bwrap <args> --`, so `$(cat $LEGATO_ROLE_PROMPT_FILE)` is still expanded by the host shell. A shell agent gets `bwrap <args> -- ${SHELL:-/bin/bash}`.
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
| The agent tool's own state (`~/.claude`, `~/.claude.json`, `~/.codex`, `~/.chimera`, …) | read-write |
| The agent tool's installation (where its command's link leads, or the git checkout a launcher script runs, as for Chimera linked with `pnpm link`) | read-only, found when Legato starts |
| Toolchains (`~/.local/bin`, mise, `~/go/bin`, linuxbrew, Rust, Bun, …) | read-only, with `default_mounts` |
| Package and build caches (`~/go/pkg/mod`, `~/.cache/go-build`, npm, pnpm, Corepack, pip, uv, cargo registry, Playwright) | read-write, with `default_mounts` |
| `<data_dir>/claude-mod`, `<data_dir>/repos` | read-only |
| The broker's folder (`<runtime>/legato/broker/`), the execution's SSH agent socket, the `gh` wrapper folder and its empty configuration, the `legato` binary, the role prompt and brief folder, the task's pasted images | read-only |
| `agents.sandbox.mounts`, and the resources the agent is given | as configured |

Never seen: Legato's configuration file and data folder, `~/.config/legato` (the 1Password token file), `~/.ssh`, `~/.config/gh`, `~/.gitconfig`, git credential stores, `~/.netrc`, `~/.1password`, `~/.config/op`, `~/.docker`, `~/.aws`, `~/.kube`, `~/.gnupg`, password stores and keyrings, `~/.npmrc`, `~/.pypirc`, the Docker and Podman sockets, and the person's runtime folder (`/run/user/<uid>`: the session bus, every Legato's IPC socket). A mount that is inside one of these is refused; one that holds one has it covered with an empty file or folder.

The environment loses the variables secrets come from (`OP_SERVICE_ACCOUNT_TOKEN`, `GITHUB_TOKEN`, `GH_TOKEN` unless it is Legato's stand-in, the session bus, `TMUX`, `DOCKER_HOST`, cloud and registry tokens, …) and `SSH_AUTH_SOCK` unless it is the execution's own socket. It gains `LEGATO_SANDBOXED=1`, `TMPDIR=/tmp`, and the person's git name and address (read from `git config --global` at start), since `~/.gitconfig` is not there.

## Commands an agent runs

In the sandbox, `legato task|plan|review|swarm|learn …`, `credential request|get|list`, `agent peers|message|broadcast`, `workflow step|show` and a few read-only listings are forwarded through the broker (`cli.run`, `cmd/legato/cli_forward.go`): the host runs its own binary with the caller's task and execution from the token (never from the agent), the agent's working directory, its stdin when the command reads `-`, and paths under the sandbox's `/tmp` mapped to the session's host folder. `credential exec` fetches the value that way and runs its command inside. Host-only commands (`device`, `secret`, `audit`, `hooks`, `auth`, `serve`, …) are not forwarded, and `openStore` refuses in the sandbox.

## Leaving it

- **One agent outside.** The spawn dialogs offer **Run without sandbox** (web, unticked each time) and `ctrl+u` (TUI); the API takes `sandbox: false` on `POST /api/agents/spawn` and `POST /api/tasks/{id}/agent`. Such an agent is marked `unsandboxed` (`/api/agents`). Swarm and workflow agents are always sandboxed. **Reload agent** keeps the choice.
- **Folders added later.** A sandbox's mounts are fixed when it starts. Adding a resource to a running sandboxed agent is refused (`409`) unless the request asks to reload (`{names, reload: true}`): the agent is then restarted with its resources and the new ones, resuming its conversation. The web Resources button explains this and asks first.

## Verified

On Fedora Silverblue with bubblewrap 0.11: Claude Code 2.1.312, Codex 0.157.1 and Chimera start and are logged in inside; a sandboxed Claude Code session binds its bridge through the broker (native panel), uses Legato's MCP tools and forwarded commands, and finds `~/.ssh` and `~/.config/legato` absent. Integration tests with a real `bwrap` (skipped without it) check the same walls and that everything ends with the pane (`internal/service/sandbox_bwrap_test.go`).

## Known limits

- A global hook of the person's agent tool that runs something outside the mounts (a Flatpak's data folder) fails inside; it is reported by the tool as a hook error.
- An SSH socket or a socket file reopened by a new channel host while an agent runs is not seen by that agent until it is reloaded.
- Not built yet: the repository cache and job clones (phase 3 tasks 3.8–3.12), repository tools (phase 4), and job submission by API (phase 1).
