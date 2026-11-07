---
title: Sandbox
description: Run agents inside bubblewrap, so they see their work and their tools but not your credentials or Legato's files.
sidebar:
  order: 30
---

Legato can start each agent inside a sandbox made with [bubblewrap](https://github.com/containers/bubblewrap). Inside, the agent sees its work, its agent tool, your toolchains and Legato's sockets for it. It does not see your SSH keys, your logins, your password stores, or Legato's database and configuration.

Use it when agents should not be able to reach your credentials, for example when you let them run without permission prompts. Together with the [credential broker](/legato/security/credential-broker/), it makes the approved way to use a key the only way.

:::note
The sandbox keeps secrets from agents. It does not filter the network, and it does not hide the agent tool's own login to its model, which the tool needs.
:::

## Turn it on

1. Install bubblewrap, which needs a Linux system that allows unprivileged user namespaces (most do):

   ```bash
   sudo dnf install bubblewrap    # Fedora
   sudo apt install bubblewrap    # Debian, Ubuntu
   ```

2. Turn the sandbox on in `config.yaml`:

   ```yaml
   agents:
     sandbox:
       enabled: true
   ```

3. Restart Legato.

From then on every agent Legato starts runs inside the sandbox. If `bwrap` is missing, or the sandbox cannot be built, the spawn fails with the reason. Legato never falls back to starting the agent outside.

## What an agent sees

| Inside the sandbox | Access |
| --- | --- |
| The system: `/usr`, `/etc`, `/opt` | read-only |
| The working directory, and a worktree's main `.git` | read and write. An agent that may only look (a find-out delegation) gets them read-only; a reviewer gets them beneath a throwaway layer: it can build and run tests, and what it writes is gone when it ends. That layer needs bubblewrap 0.10 or later; with an older one the reviewer's checkout is read-only |
| The agent tool's own state, such as `~/.claude`, `~/.codex` or `~/.chimera`, so sessions resume inside and out | read and write |
| The agent tool's installation | read-only |
| Your toolchains, such as `~/.local/bin`, mise, `~/go/bin`, Rust and Bun | read-only |
| Package and build caches for Go, npm, pnpm, Yarn, Bun, pip, uv, Cargo, Gradle, Maven and others | read and write |
| A home folder | an empty folder, with only the entries above in it |
| `/tmp` | a folder of the session's own |
| Legato's sockets for the session: the SSH agent socket, the `gh` wrapper and the host broker | read-only |
| The `legato` binary | read-only |
| Your `known_hosts` and the system's ssh configuration | read-only |

Agents never see:

- Legato's configuration, database and data folder, and `~/.config/legato`.
- `~/.ssh`, `~/.gnupg`, `~/.1password`, `~/.config/op`, `~/.config/gh`, `~/.gitconfig`, git credential stores and `~/.netrc`.
- `~/.aws`, `~/.kube`, `~/.docker`, and the Docker and Podman sockets.
- Package registry logins: `~/.npmrc`, `~/.pypirc`, `~/.yarnrc`, Gradle's `gradle.properties`, Maven's `settings.xml`, Composer's `auth.json`.
- Password stores and keyrings, the session bus, and your runtime folder.

The environment loses the variables secrets come from, such as `GITHUB_TOKEN`, `OP_SERVICE_ACCOUNT_TOKEN`, cloud and registry tokens, and `DOCKER_HOST`. It gains your git name and email address, since `~/.gitconfig` is not there.

The sandbox shares the host's network and your user. It has its own process, IPC and hostname namespaces, and everything in it ends when the agent's pane closes.

## How agents still get their work done

- **Legato commands.** Commands an agent runs, such as `legato task note` or `legato credential exec`, and Legato's own tools and hooks reach the host Legato through a socket called the host broker. The host runs them for the agent's own task, and nothing else.
- **SSH.** `~/.ssh` stays hidden, so the agent has no keys of its own. A login works when an `ssh` credential covers the host: Legato's SSH agent signs it.
- **GitHub.** With a `github` credential, `git` fetches with read tokens even when the repository's `origin` is an ssh address, and pushes go through Legato's proxy. `gh` is always Legato's wrapper inside the sandbox.
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

To give one agent a folder, use a [resource](/legato/agent-tools/resources/) instead. A sandbox's mounts are fixed when it starts, so adding a resource to a running sandboxed agent restarts it, resuming its conversation. The web UI asks first.

To leave out your toolchains and caches:

```yaml
agents:
  sandbox:
    default_mounts: false
```

## Run one agent outside

You can start one agent without the sandbox:

- **Web UI**: tick **Run without sandbox** in the spawn dialog. It is unticked each time.
- **TUI**: press `ctrl+u` in the spawn overlay.
- **API**: send `"sandbox": false` to `POST /api/agents/spawn` or `POST /api/tasks/{id}/agent`.

Such an agent is marked unsandboxed, in the web UI and in the TUI's agents view. Swarm and workflow agents always run in the sandbox.

To move a running agent, press `U` in the TUI's agents view. Legato restarts it on the other side of the sandbox, resuming its conversation. **Reload agent** keeps whichever side the agent is on.

## Limits

- What two sandboxes share is what both are given from your machine. All Claude Code agents share `~/.claude`, and all Codex agents share `~/.codex`, with each other's sessions in them.
- A global hook of your agent tool that runs a program outside the mounts fails on every tool call. Add its folder with `agents.sandbox.mounts`, or remove the hook.
- Inside Legato's sandbox, Legato drops Chimera's own `--sandbox` flags. Chimera's sandbox is a container, and the container socket it needs would undo Legato's.
- An agent that is running does not see a new SSH socket until it is reloaded, for example after another Legato on the machine takes over serving the credential channels.
- bubblewrap runs on Linux only.

For how the sandbox is built, see [`docs/ai/sandbox.md`](https://github.com/cpave3/legato/blob/main/docs/ai/sandbox.md).
