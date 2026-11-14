---
title: Chimera
description: How Legato connects Chimera sessions, state and tools through its mod.
---

Legato starts Chimera in a tmux session and connects it to the web UI's native panel. With a Chimera that supports mods v1, Legato's mod connects as soon as the session starts, reports its state and saves the conversation for resume. You do not need activity hook scripts. Running `legato hooks install --tool chimera` now removes Legato's old scripts and leaves your own hooks alone. Set `native_bridge: false` first if you still need to install scripts for an older Chimera.

With Legato's MCP tools enabled, each launch checks Chimera's mod capability. An unsupported build fails before starting a terminal, with an error asking you to upgrade Chimera or set `adapters.chimera.native_bridge: false` for legacy tools. Launch arguments that disable mods are also refused in this mode.

## The Legato mod

Legato loads its mod into every Chimera it starts, including a `chimera` you start by hand in the same pane. It updates the installed copy at Legato startup only when its content changed. Subagents do not take over the parent session's panel or activity.

The card shows **working** during a turn, **needs you** for a permission prompt or question, **background** while shells or background subagents continue after a turn, and **error** when a turn fails. Attaching a file or running `!command` does not start a turn. The background jobs are listed with the agent.

To use the legacy integration instead:

```yaml
adapters:
  chimera:
    native_bridge: false
```

Older Chimera builds need `native_bridge: false` when Legato's MCP tools are enabled. With tools disabled, an old build can still ignore the mod and use lockfile discovery, which starts after ten seconds without a mod binding. Existing hook scripts still work for legacy sessions; the mod's reports take precedence when it is present.

## How Legato starts Chimera

When you start an agent on a task with `a` in the TUI or **Open agent** in the web UI, Legato opens a tmux session in the agent's folder and types `chimera`, with these additions:

- `--session-name <task ID>` for a task's agent, so that Legato can find the session again.
- `--prompt` with Legato's launch prompt. Chimera takes it as the first message of the conversation, so the agent starts work at once.
- `--mode <name>` for a swarm role, when you map roles to modes.
- Legato's mod, loaded through the pane's environment, which bundles [Legato's tools](/legato/agent-tools/mcp-tools/) for the session.
- `--add-read-path` and `--add-write-path` for each path of the [resources](/legato/agent-tools/resources/) you gave the agent.
- Your `adapters.chimera.launch_args`.
- `--model`, when the spawn names a model, such as `swarm.worker.model` for a swarm.
- The `launch_args` of a tier, when a swarm conductor picks one for a worker.

The launch prompt depends on the agent:

- A plain agent gets a short prompt: it is a standalone agent, and it learns how Legato's plans, reviews and peer messages work.
- A [swarm](/legato/using/swarms/) conductor or worker gets its role prompt.
- Both get the resources and notes for a fresh clone. A mod-capable Chimera reads Legato's tools guidance from the MCP server's instructions.

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

The mod bundles `legato mcp serve` and lets Chimera run Legato's tools without asking. The launch command adds no `--mcp-config`, and your own MCP configuration remains untouched. `agents.mcp.enabled: false` leaves the tools out of the mod.

With `native_bridge: false`, Legato uses the older `--mcp-config` path, first checking that Chimera accepts the flag. A flag of your own in `launch_args` replaces Legato's on that path.

## The native panel

The mod reports the session's endpoint directly; startup messages wait for that connection instead of being typed into a TUI that is not ready. A task message sent at launch also waits for the initial role turn to finish, then starts its own turn with the original text. If binding or delivery fails, Legato keeps the message and records the failure in its log instead of typing it into the composer. Pending launch messages for live agents are retried when Legato next starts; a role turn that stays busy beyond the 30-second delivery budget leaves the message pending. A session switch inside Chimera moves the panel to the new conversation. Exiting Chimera releases the panel. Legato keeps live native bindings across its own restarts. The web UI then shows the agent in the native panel. In the panel you can:

- Read the conversation as it streams, with a card for each tool call. A `spawn_agent` call has a card that shows each subagent's work.
- Send a message, or queue one during a turn.
- Write `@path` to give the agent read access to a file or folder and attach its contents, or `#path` for write access. Put a path with spaces in double quotes: `@"My Vault/today.md"`.
- Run `!command` in the agent's folder. Its output goes to the session.
- Interrupt the turn.
- Switch the model and the Chimera mode, and change the model's settings. See [Model settings](/legato/agent-tools/model-settings/).
- See how full the context window is, and what the model reasoned.

When Chimera asks for a permission or asks a question, Legato raises it as an approval: in the web UI's tray, in [Attention](/legato/using/attention/), in the TUI (press `!`), and on your phone. The first answer, here or in the terminal, settles it.

For a question, choose an option or type an answer of your own, then **Send answer**. You can answer from the native panel, the approvals tray or focus mode. **Dismiss** tells Chimera you declined to answer.

If the panel did not connect, **Upgrade to native Chimera** in the terminal view's menu looks for the endpoint again. **Downgrade to tmux** in the panel's menu shows the agent through its terminal, without stopping it.

## Resume a conversation

The mod saves the actual Chimera session ID on every start, resume, fork or switch. When you start an agent on a task that has a saved conversation, the web dialog and the TUI offer to resume it or start a new one. Resume types `chimera --session <id>`, and does not send the launch prompt again.

**Reload agent** in the web UI, or `R` in the TUI's agents view, stops an agent and starts it again with the same folder and resources, resuming its conversation.

After a crash, an agent whose session is gone is kept as **Interrupted** and resumes the same way, from its saved conversation. See [After a crash: interrupted agents](/legato/using/agents/#after-a-crash-interrupted-agents).

## Sandboxes

Chimera has a sandbox of its own, and Legato has [one for every agent](/legato/security/sandbox/). They interact in two ways:

- **Inside Legato's sandbox,** Legato removes Chimera's `--sandbox`, `--sandbox-*` and `--apply-on-success` flags from the launch command and logs that it did. Chimera's sandbox is a Docker container, and access to Docker would undo Legato's sandbox.
- **In Chimera's own sandbox,** tool calls cannot see the `legato` command, Legato's files or its environment. The role prompts tell the agent to run `legato` commands, and reads of Legato's files, in host mode.

## Local server security

Legato gives each Chimera launch its own server secret. It reads the secret from a private file at launch, never from typed text or tmux's environment, and deletes that file before Chimera starts. The bootstrap stays outside the sandbox and is hidden from the agent. The panel, messages and permissions use the authenticated local endpoint. An old Chimera without secret support cannot provide this protection.

Inside Legato's sandbox, the installed mod and your Chimera `mods` and `hooks` folders are read-only; conversation state stays writable.

Chimera's own commands can still read the server secret from its process environment (`/proc/<pid>/environ`), including inside Legato's sandbox. Passing the secret by file descriptor or pipe is a pending Chimera fix (CHIM-86).

## Limits

- Legato connects to Chimera's endpoint only on the same machine.
- The mod needs Chimera mods v1. With the mod disabled, tools need a Chimera that accepts `--mcp-config`.

For implementation details and the current verification limits, see [Chimera native integration on GitHub](https://github.com/cpave3/legato/blob/main/docs/ai/chimera-native.md).
