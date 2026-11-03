---
title: Resources
description: Give agents the files and folders you use often, such as a notes vault or a folder of specs, by name.
sidebar:
  order: 60
---

A resource is a named set of files and folders that you give agents often: an Obsidian vault, a folder of specs, a templates folder. You do not type the paths into every session. You tick the resource when you start an agent, or add it to an agent that is already running, and the agent learns where the files are and what it may do with them.

Each path of a resource is given to **read** or to **write**.

Resources are not workspaces. A workspace groups tasks on the board; a resource is files for agents.

## Define a resource

You can define resources in the configuration file, in the web UI, or with the `legato` command.

### In the configuration

```yaml
resources:
  - name: Obsidian vault
    paths:
      - path: ~/Documents/My Vault   # spaces are fine
        access: write
      - path: /srv/templates         # read is the default
```

- Each resource needs a name and at least one path. Names must be different, ignoring case.
- A path is absolute or starts with `~/`. Anything else fails config loading.
- `access` is `read` or `write`. Leave it out for `read`.

Legato lists these resources everywhere, marked `config.yaml`. You change them only in the file.

### In the web UI

Open **Settings › Resources**. Add, change and remove resources there, with **Read** or **Write** for each path.

### With the command line

```bash
legato resource add "Obsidian vault" ~/Documents/My\ Vault --write
legato resource list
legato resource remove "Obsidian vault"
```

`legato resource add` makes a resource, or adds paths to a saved one. `--write` lets agents change the paths in that command. A resource from the configuration file cannot be removed this way.

Legato checks that each path exists when you save a resource. A saved resource cannot take the name of one in the configuration. If the file gains a name that a saved resource already has, the file's resource wins and the saved one is not listed.

## Give resources to a new agent

- **Web UI:** the spawn dialog lists the resources under **Resources**, when there are any. Tick the ones to give.
- **TUI:** the spawn overlay has a **resources** line between **agent** and **where**, when there are any. Move with `h` and `l`, and tick with `space`. A resource that lets the agent write a path is marked `✎`.

## Add a resource to a running agent

In the web UI, open the agent's native panel and press **Resources** in its status bar, beside the model selector. The list marks the resources the agent already has. Tap one to give it.

This needs the native panel. An agent that the web UI shows only through its terminal cannot get a resource while it runs.

The agent is told in a message that starts with `[legato resources]`. A busy agent reads it at its next step. An idle Chimera agent answers it.

You cannot take a resource back from a running agent. To give an agent less, start it again without the resource.

## What the agent gets

Every agent gets a **Resources** section in its launch prompt. It names each resource, its paths, and whether each path is `read only` or `read and write`. A resource added later arrives as the message above.

What else the agent gets depends on its tool:

| Agent tool | Access |
| --- | --- |
| [Chimera](/legato/agent-tools/chimera/) | Legato grants each path to the session: `--add-read-path` or `--add-write-path` at launch, and the same grants for a resource added later. |
| [Claude Code](/legato/agent-tools/claude-code/), [Codex](/legato/agent-tools/codex/) | The paths as text only. The tool's own rules decide access. Claude Code asks you before it reads a file outside its working folder, as it does for any path. |

Some details:

- A path that does not exist when the agent starts, or when you add the resource, is left out. A resource with no paths left is skipped. This is because Chimera does not start when it is given a missing path.
- When one resource gives a path to read and another gives it to write, the agent can write it.
- A resumed conversation gets no launch prompt. Chimera still gets the path grants at launch, and keeps the paths of the session it resumes.
- Paths with spaces stay one path everywhere. In a message to a Chimera agent, write such a path in double quotes, `@"My Vault/today.md"`, or escape the spaces, `@My\ Vault/today.md`.

## Resources and the sandbox

When agents run in [Legato's sandbox](/legato/security/sandbox/), the sandbox shows them only certain folders. Legato adds the paths of the agent's resources to those folders, read-only or writable as the resource says.

The sandbox's folders are fixed when the agent starts. So when you add a resource to a running sandboxed agent, Legato asks first, then reloads the agent: it stops and starts again with the new resource, and its conversation resumes.
