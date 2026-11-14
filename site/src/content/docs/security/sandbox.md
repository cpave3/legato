---
title: Sandbox
description: Run agents inside a sandbox (bubblewrap on Linux, Seatbelt on macOS), so they see their work and their tools but not your credentials or Legato's files.
sidebar:
  order: 30
---

Legato can start each agent inside a sandbox: made with [bubblewrap](https://github.com/containers/bubblewrap) on Linux, and with Seatbelt (`sandbox-exec`, built into macOS) on a Mac. Inside, the agent sees its work, its agent tool, your toolchains and Legato's sockets for it. It does not see your SSH keys, your logins, your password stores, or Legato's database and configuration.

Use it when agents should not be able to reach your credentials, for example when you let them run without permission prompts. Together with the [credential broker](/legato/security/credential-broker/), it makes the approved way to use a key the only way.

:::note
The sandbox keeps secrets from agents. It does not filter the network, and it does not hide the agent tool's own login to its model, which the tool needs.
:::

## Turn it on

1. On Linux, install bubblewrap, which needs a system that allows unprivileged user namespaces (most do):

   ```bash
   sudo dnf install bubblewrap    # Fedora
   sudo apt install bubblewrap    # Debian, Ubuntu
   ```

   On macOS there is nothing to install. If you use Claude Code, give it a login it can use inside the sandbox (see [On macOS](#on-macos)).

2. Turn the sandbox on in `config.yaml`:

   ```yaml
   agents:
     sandbox:
       enabled: true
   ```

3. Restart Legato.

From then on every agent Legato starts runs inside the sandbox. If `bwrap` (or, on macOS, `/usr/bin/sandbox-exec`) is missing, or the sandbox cannot be built, the spawn fails with the reason. Legato never falls back to starting the agent outside.

## What an agent sees

| Inside the sandbox | Access |
| --- | --- |
| The system: `/usr`, `/etc`, `/opt` | read-only |
| The working directory, and a worktree's main `.git` | read and write. An agent that may only look (a find-out delegation) gets them read-only; a reviewer gets them beneath a throwaway layer: it can build and run tests, and what it writes is gone when it ends. That layer is on disk, and needs bubblewrap 0.10 or later; with an older one the reviewer's checkout is read-only. On macOS the reviewer works in a copy-on-write copy of the checkout instead, made in its scratch folder (APFS), with your checkout read-only beside it |
| The agent tool's own state, such as `~/.claude`, `~/.codex` or `~/.chimera`, so sessions resume inside and out | read and write |
| The agent tool's installation | read-only |
| Your toolchains, such as `~/.local/bin`, mise, `~/go/bin`, Rust and Bun | read-only |
| Package and build caches for Go, npm, pnpm, Yarn, Bun, pip, uv, Cargo, Gradle, Maven and others | read and write |
| A home folder | an empty folder of the session's own, on disk, with only the entries above in it |
| `/tmp` | a folder of the session's own, on disk |
| Legato's sockets for the session: the SSH agent socket, the `gh` wrapper and the host broker | read-only |
| The `legato` binary | read-only |
| For an agent started as an [engram](/legato/agent-tools/engrams/): the engram binary, first on `PATH`, and engram's store, so the agent's own engram notes and memories are written | binary read-only, store read and write |
| Your `known_hosts` and the system's ssh configuration | read-only |

Agents never see:

- Legato's configuration, database and data folder, and `~/.config/legato`.
- `~/.ssh`, `~/.gnupg`, `~/.1password`, `~/.config/op`, `~/.config/gh`, `~/.gitconfig`, git credential stores and `~/.netrc`.
- `~/.aws`, `~/.azure`, `~/.kube`, `~/.docker`, and the Docker and Podman sockets (with [Docker in the sandbox](#docker-in-the-sandbox) on, an agent gets a filtered socket of its own instead).
- Package registry logins: `~/.npmrc`, `~/.pypirc`, `~/.yarnrc`, Gradle's `gradle.properties`, Maven's `settings.xml`, Composer's `auth.json`.
- Password stores and keyrings, the session bus, and your runtime folder.

The environment loses the variables secrets come from, such as `GITHUB_TOKEN`, `OP_SERVICE_ACCOUNT_TOKEN`, cloud and registry tokens, and `DOCKER_HOST`. It gains your git name and email address, since `~/.gitconfig` is not there.

The sandbox shares the host's network and your user. It has its own process, IPC and hostname namespaces, and everything in it ends when the agent's pane closes.

## How agents still get their work done

- **Legato commands.** Commands an agent runs, such as `legato task note` or `legato credential exec`, and Legato's own tools and hooks reach the host Legato through a socket called the host broker. The host runs them for the agent's own task, and nothing else.
- **SSH.** `~/.ssh` stays hidden, so the agent has no keys of its own. A login works when an `ssh` credential covers the host: Legato's SSH agent signs it.
- **GitHub.** With a `github` credential, `git` fetches with read tokens even when the repository's `origin` is an ssh address, and pushes go through Legato's proxy. `gh` is always Legato's wrapper inside the sandbox.
- **Azure.** With an `azure` credential, `az` inside the sandbox is Legato's wrapper: it signs in as the credential's app before the first command that needs an account, once you allow it. See [Azure](/legato/security/credential-broker/#azure).
- **Packages.** A package already in a shared cache installs as it does outside. One that is not needs a registry that works without your login.

## Add folders

Give every sandbox more paths with `agents.sandbox.mounts`. A path is absolute or starts with `~/`, and access is `read` (the default) or `write`:

```yaml
agents:
  sandbox:
    enabled: true
    mounts:
      - { path: ~/Documents/specs }
      - { path: ~/scratch, access: write }
```

Legato refuses a mount inside a path it hides, such as `~/.ssh`.

To give one agent a folder, use a [resource](/legato/agent-tools/resources/) instead. To give every agent that works in one repository a folder, use [resources for a repository](/legato/agent-tools/resources/#resources-for-a-repository). A sandbox's mounts are fixed when it starts, so adding a resource to a running sandboxed agent restarts it, resuming its conversation. The web UI asks first.

To leave out your toolchains and caches:

```yaml
agents:
  sandbox:
    default_mounts: false
```

## Scratch space

Each agent's `/tmp`, its home folder and a reviewer's throwaway layer are folders on disk, not in memory: `<data_dir>/sandbox/<execution>/`, readable only by you. A build cache an agent puts in `/tmp` uses disk space, not the memory the build needs. Legato removes the folder when the agent ends, however it ends, and sweeps the folders of agents that ended while it was not running. Folders of agents that are still running are never touched, and no agent sees another's.

To keep scratch elsewhere, on a bigger disk say:

```yaml
agents:
  sandbox:
    scratch_dir: /mnt/big/legato-scratch
```

Nothing limits how much an agent writes there, so a runaway agent can fill that disk.

## Docker in the sandbox

The sandbox hides your Docker and Podman sockets. A Docker socket is root on your machine when the daemon runs as root: any container could mount `~/.ssh`, which would undo the sandbox. To let sandboxed agents run containers anyway, such as a project's development stack with `docker compose`, turn on Legato's filtered Docker socket:

```yaml
agents:
  sandbox:
    enabled: true
    docker:
      enabled: true
      cap_add: [SYS_PTRACE]   # capabilities containers may add; none by default
      publish: loopback       # published ports bind 127.0.0.1 (the default), or any
```

Restart Legato. Each sandboxed agent then gets a Docker socket of its own, with `DOCKER_HOST` set to it. Legato passes what the agent asks to your Docker daemon (your `DOCKER_HOST`, else your Docker context, else `/var/run/docker.sock`; `agents.sandbox.docker.host` names another), but only what keeps within the agent's sandbox:

- **Its own things.** Every container, network and volume the agent makes is labelled with its run (`legato.execution`). `docker ps`, `docker network ls`, `docker volume ls`, events and prunes show it only its own. Inspecting, starting, stopping, removing, `exec`, logs, `cp` and joining a network work only on its own. Two agents can run stacks side by side without seeing each other's, as long as their compose project names and published ports differ.
- **Only what its sandbox sees.** A bind mount (`-v`, `--mount type=bind`, a volume that binds a folder, `--volumes-from`) may name only a path the agent's sandbox shows, with no more access: something it sees read-only is mounted read-only, and anything hidden is refused, `~/.ssh` and the Docker socket among them. Links are resolved on the host first, so a link in the worktree to `~/.ssh` is refused too. The sandbox's `/tmp` and home are its own scratch folders, so a container mounting them gets those. Folders given as [resources](/legato/agent-tools/resources/), including [resources for a repository](/legato/agent-tools/resources/#resources-for-a-repository), can be mounted into the agent's containers at the same access.
- **Nothing that is root on your machine.** Refused: `--privileged`, devices and GPUs, `--cap-add` beyond `cap_add`, the host's network, PID, IPC, UTS, user or cgroup namespace, another agent's container's namespace, security options that turn off seccomp, AppArmor or SELinux, a cgroup parent, sysctls that are not per container, other runtimes, and `systempaths=unconfined`. A build on the host network is refused. Swarm, services, secrets, configs, plugins and nodes are refused.
- **Shared images.** Pulling, building and listing images work as usual. An agent may remove only images it built.
- **Ports.** Published ports bind `127.0.0.1` unless `publish` is `any`. Agents share your network, so they reach their own services there.

A refusal reads like any Docker error and starts with `legato:`, so the agent sees why:

```text
docker: Error response from daemon: legato: privileged containers are not allowed: they are root on the host
```

When the agent ends, however it ends, Legato removes its containers, networks and volumes. Reloading an agent starts a new run, so its containers go too; start the stack again after a reload. Things left by an agent that ended while Legato was not running are removed when Legato next checks its agents. Another Legato's containers are never touched.

Agents run without the sandbox, and your own shells, are unchanged: they use Docker exactly as before, with nothing filtered.

:::tip
Run Docker rootless if you can. The filtered socket is the wall, but if anything ever got past it, a rootless daemon gives away only your user, not root. Point `agents.sandbox.docker.host` at the rootless socket (`unix:///run/user/1000/docker.sock`); Podman's socket works too.
:::

Private registries need a login, and `~/.docker` stays hidden, so agents can pull only public images for now.

## Run one agent outside

You can start one agent without the sandbox:

- **Web UI**: tick **Run without sandbox** in the spawn dialog. It is unticked each time.
- **TUI**: press `ctrl+u` in the spawn overlay.
- **API**: send `"sandbox": false` to `POST /api/agents/spawn` or `POST /api/tasks/{id}/agent`.

Such an agent is marked unsandboxed, in the web UI and in the TUI's agents view. Swarm and workflow agents always run in the sandbox, and so does [the Reeve](/legato/using/reeve/), which cannot be started or moved out of it. The Reeve gets its own folder, `~/.local/state/legato/reeve`, read-write. Its engram's store is inside that folder, and its sandbox has the engram binary first on `PATH`, read-only, as for any agent started as an engram.

To move a running agent, press `U` in the TUI's agents view, or in the web UI choose **Reload without sandbox** (or **Reload in sandbox**) in the agent's **More actions** menu or its right-click menu in the sidebar. Legato restarts it on the other side of the sandbox, resuming its conversation. **Reload agent** keeps whichever side the agent is on. Legato tells the agent that the sandbox was added or removed, and the web UI shows that as a message from Legato.

## On macOS

On a Mac the sandbox is Seatbelt, the same mechanism Claude Code and Codex use for their own sandboxes there. Legato writes a profile for each agent from the same list of what it may see, and starts the agent with `sandbox-exec`. Seatbelt can allow or deny each path and system service; it cannot show a folder somewhere else, an empty home folder, or a separate set of processes. So some things differ from Linux:

- **The agent's home folder.** `HOME` is a folder of the agent's own in its scratch space, with links to what the sandbox shows of your home (`~/.claude`, your toolchains, your caches). What an agent writes to its home lands there. Programs that find your home without `HOME`, as `ssh` does, are refused your files instead.
- **Hidden means refused, not absent.** A hidden file still exists: its name and size can be seen, its content cannot be read or written. Hard links, copy-on-write copies and renames cannot get round this.
- **`/tmp`.** The agent's `TMPDIR` is its own scratch folder. The shared `/tmp` is closed: it holds the tmux server's socket, macOS's SSH agent and every Legato's sockets. A tool that writes to `/tmp` by name, not through `TMPDIR`, fails.
- **The Keychain** is closed, so nothing inside can read an item from it, including `gh`'s token. Claude Code keeps its login there, so sandboxed Claude Code agents log in with a token instead:

  ```bash
  claude setup-token                        # prints a token that lasts a year
  legato secret add claude-code-token       # paste the token
  ```

  ```yaml
  agents:
    sandbox:
      claude_code_token: legato://claude-code-token
  ```

  Without it, a sandboxed Claude Code agent is not started, so an agent left to run unattended never waits on a login. Codex works when its login is in `~/.codex/auth.json`, the default; with `cli_auth_credentials_store = "keyring"` it is not started.
- **Their own sandboxes.** A program in a Seatbelt sandbox cannot start a sandbox of its own, so inside Legato's, Claude Code's sandbox is off and Codex runs with `sandbox_mode = "danger-full-access"`. Legato's sandbox holds them; Codex still asks for approvals as you set it.
- **Processes.** An agent cannot signal, inspect or debug a process outside its sandbox, but it can see that one runs (its name and number, not its arguments or environment). `ps` does not run inside at all, as no setuid program does; agents can use `pgrep`. A process an agent starts and detaches keeps running when the pane ends; Legato ends every process that still carries the agent's run when the agent ends.
- **Starting things outside.** `open`, `osascript`, `launchctl`, the pasteboard (`pbcopy`, `pbpaste`) and `sudo` do not work inside: each would start something outside the sandbox or reach your session.
- **Reviewers** work in a copy-on-write copy of the checkout, made in their scratch folder; your checkout is read-only beside it.

To see what the sandbox refused, while an agent runs:

```bash
log stream --style compact --predicate 'eventMessage CONTAINS "legato:"'
```

Each line names the run, the operation and the path or service. Apple marks `sandbox-exec` as deprecated, but it is still part of macOS and is what these tools rely on; if a future macOS removes it, Legato refuses to start sandboxed agents rather than start them outside.

## Limits

- What two sandboxes share is what both are given from your machine. All Claude Code agents share `~/.claude`, and all Codex agents share `~/.codex`, with each other's sessions in them.
- A global hook of your agent tool that runs a program outside the mounts fails on every tool call. Add its folder with `agents.sandbox.mounts`, or remove the hook.
- Inside Legato's sandbox, Legato drops Chimera's own `--sandbox` flags. Chimera's sandbox is a container, and the container socket it needs would undo Legato's. This is so even with [Docker in the sandbox](#docker-in-the-sandbox) on.
- With Docker in the sandbox, BuildKit's own settings for a build sent over its session (`docker buildx`, `compose build`) are applied by the Docker daemon, not by Legato. The daemon refuses insecure builds unless you allow them in its configuration; do not.
- An agent cannot change the hooks of its checkout's git folder, which your own git runs outside the sandbox. It can change the checkout's git config, as setting an upstream needs; when the agent ends, Legato looks for config it added that makes git run a command (`core.fsmonitor`, `core.hooksPath`, an alias starting with `!`, a filter, an include) and shows each such change in [Attention](/legato/using/attention/), with **Remove them**. Hooks the repository itself tracks, such as a `.husky` folder, are files like any other.
- An agent keeps your terminal pane as its own, but it cannot type into it. The sandbox refuses the `TIOCSTI` and `TIOCLINUX` requests, which push input into a terminal. Without this, an agent could leave a command that your shell in the pane would run, outside the sandbox, after the agent exits.
- An agent that is running does not see a new SSH socket until it is reloaded, for example after another Legato on the machine takes over serving the credential channels.
- bubblewrap runs on Linux only, and Seatbelt on macOS only: `agents.sandbox.backend` is the host's own unless you name one.

For how the sandbox is built, see [`docs/ai/sandbox.md`](https://github.com/cpave3/legato/blob/main/docs/ai/sandbox.md).
