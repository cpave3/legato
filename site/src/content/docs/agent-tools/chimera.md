---
title: Chimera
description: How Legato starts Chimera, reports what it is doing on the board, and shows it in the web UI's native panel.
sidebar:
  order: 30
---

Legato starts Chimera in a tmux session for a task, shows on the card whether it is working or needs you, and shows the session in the web UI's native panel. Chimera serves its sessions over a local HTTP endpoint of its own, so Legato needs no bridge for it: it finds the endpoint and connects.

## Install the activity hooks

The hooks tell Legato when Chimera starts work, waits for you, fails and stops. They also save the Chimera session ID, which Legato needs to resume a conversation.

```bash
legato hooks install --tool chimera
```

Chimera's hooks are per user, so it does not matter which folder you run this in. The command writes one script for each event to `~/.chimera/hooks/<Event>/legato-*.sh`. `legato hooks uninstall --tool chimera` removes them.

The scripts do nothing outside a session that Legato started.

### What the card shows

| Event | Card |
| --- | --- |
| `UserPromptSubmit` | `⠹ running` |
| `PermissionRequest`, `UserInputRequired` | `◆ needs you` |
| `PermissionResolved` | `◆ needs you` while another permission waits, else `⠹ running` |
| `OnError` | `error` |
| `Stop`, `SessionEnd`, `Interrupt`, `Timeout` | `○ idle` |
| `SessionCreated` | No change: saves the session ID for resume |

If you installed the hooks with an older Legato, run `legato hooks install --tool chimera` again to get the current scripts.

## How Legato starts Chimera

When you start an agent on a task with `a` in the TUI or **Open agent** in the web UI, Legato opens a tmux session in the agent's folder and types `chimera`, with these additions:

- `--session-name <task ID>` for a task's agent, so that Legato can find the session again.
- `--prompt` with Legato's launch prompt. Chimera takes it as the first message of the conversation, so the agent starts work at once.
- `--mode <name>` for a swarm role, when you map roles to modes.
- `--mcp-config`, which adds [Legato's tools](/legato/agent-tools/mcp-tools/) for this session only.
- `--add-read-path` and `--add-write-path` for each path of the [resources](/legato/agent-tools/resources/) you gave the agent.
- Your `adapters.chimera.launch_args`.
- `--model`, when the spawn names a model, such as `swarm.worker.model` for a swarm.
- The `launch_args` of a tier, when a swarm conductor picks one for a worker.

The launch prompt depends on the agent:

- A plain agent gets a short prompt: it is a standalone agent, and it learns how Legato's plans, reviews and peer messages work.
- A [swarm](/legato/using/swarms/) conductor or worker gets its role prompt.
- Both get the guidance about Legato's tools, the resources, and notes for a fresh clone. Chimera does not read an MCP server's own instructions, so the guidance goes in the prompt.

```yaml
adapters:
  chimera:
    launch_args: ["--sandbox"]
    tiers:
      small:
        description: Quick, narrow changes
        launch_args: ["--model", "<a small model>"]
```

A tier's arguments come after `launch_args`, so a tier's `--model` wins. See the [configuration reference](/legato/reference/configuration/) for every setting.

### Modes for swarm roles

Legato can start each swarm role in a Chimera mode of your own. Legato ships no modes; name modes you have set up in Chimera.

```yaml
adapters:
  chimera:
    modes:
      conductor: planning
      worker: building
      frontend: ui-work
```

- A role that has an entry gets that mode.
- Any other worker role gets the `worker` entry.
- A plain agent, which has no role, gets no `--mode`.
- A `--mode` in `launch_args` wins over every entry.

### Legato's tools

Legato adds its tools with `--mcp-config`, which older Chimera releases do not have. At the first launch that needs it, Legato reads `chimera --help`. If the flag is not there, sessions start without the tools, and Legato logs that you should update Chimera.

If your own `launch_args` hold a `--mcp-config`, yours replaces Legato's, because Chimera takes the flag once. Chimera runs Legato's tools without asking. Subagents that Chimera starts with `spawn_agent` read only the global Chimera configuration, so they do not get Legato's tools.

## The native panel

After launch, Legato looks for the session's endpoint in `~/.chimera/instances/` for up to 30 seconds, and connects when exactly one live endpoint serves the task's session. The web UI then shows the agent in the native panel. In the panel you can:

- Read the conversation as it streams, with a card for each tool call. A `spawn_agent` call has a card that shows each subagent's work.
- Send a message, or queue one during a turn.
- Write `@path` to give the agent read access to a file or folder and attach its contents, or `#path` for write access. Put a path with spaces in double quotes: `@"My Vault/today.md"`.
- Run `!command` in the agent's folder. Its output goes to the session.
- Interrupt the turn.
- Switch the model and the Chimera mode, and change the model's settings. See [Model settings](/legato/agent-tools/model-settings/).
- See how full the context window is, and what the model reasoned.

When Chimera asks for a permission or asks a question, Legato raises it as an approval: in the web UI's tray, in [Attention](/legato/using/attention/), in the TUI (press `!`), and on your phone. The first answer, here or in the terminal, settles it.

If the panel did not connect, **Upgrade to native Chimera** in the terminal view's menu looks for the endpoint again. **Downgrade to tmux** in the panel's menu shows the agent through its terminal, without stopping it.

## Resume a conversation

The `SessionCreated` hook saves the Chimera session ID of each task's agent. When you start an agent on a task that has a saved conversation, the web dialog and the TUI offer to resume it or start a new one. Resume types `chimera --session <id>`, and does not send the launch prompt again.

**Reload agent** in the web UI, or `R` in the TUI's agents view, stops an agent and starts it again with the same folder and resources, resuming its conversation.

## Sandboxes

Chimera has a sandbox of its own, and Legato has [one for every agent](/legato/security/sandbox/). They interact in two ways:

- **Inside Legato's sandbox,** Legato removes Chimera's `--sandbox`, `--sandbox-*` and `--apply-on-success` flags from the launch command and logs that it did. Chimera's sandbox is a Docker container, and access to Docker would undo Legato's sandbox.
- **In Chimera's own sandbox,** tool calls cannot see the `legato` command, Legato's files or its environment. The role prompts tell the agent to run `legato` commands, and reads of Legato's files, in host mode.

## Limits

- Legato connects to Chimera's endpoint only on the same machine.
- Legato's tools need a Chimera that accepts `--mcp-config`.
- Chimera subagents do not get Legato's tools.

For more about Chimera's hooks and how Legato reads them, see the [CLI notes on GitHub](https://github.com/cpave3/legato/blob/main/docs/ai/cli.md#chimera-integration).
