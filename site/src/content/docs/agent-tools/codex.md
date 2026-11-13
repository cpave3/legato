---
title: Codex
description: How Legato starts Codex, reports what it is doing on the board, and connects it to the web UI's native panel.
sidebar:
  order: 20
---

Legato starts Codex in a tmux session for a task, shows on the card whether it is working or needs you, and connects the session to Legato directly. With that connection you read and drive the conversation in the web UI's native panel, and you answer Codex's approvals and questions from Legato, your phone or the terminal.

Codex needs no plugin for this. Codex can run its conversation in an app server that more than one client shares. Legato starts one for each session: the Codex terminal is one client, and Legato's bridge is the other.

## Install the activity hooks

The hooks tell Legato when Codex starts work, waits for you, and stops. Without them a card shows no activity. The bridge adds only what no hook reports: a turn that was interrupted or failed.

1. Go to the folder that should hold the hooks. Your home folder installs them for every project, in `~/.codex/`. A project folder installs them for that project, in `<project>/.codex/`.
2. Run:

   ```bash
   legato hooks install --tool codex
   ```

3. Start `codex` and run `/hooks`. Review Legato's entries and trust them. Codex does not run hooks that you have not trusted.

The command writes four scripts to `.codex/hooks/legato-*.sh` and adds them to `.codex/hooks.json`. Other entries in `hooks.json` are kept. `legato hooks uninstall --tool codex` removes only Legato's entries and scripts.

The scripts do nothing outside a session that Legato started.

### What the card shows

| Card | Meaning | Set by |
| --- | --- | --- |
| `⠹ running` (green) | Codex is working. | `UserPromptSubmit`, `PostToolUse` |
| `◆ needs you` (amber) | Codex waits for your approval. | `PermissionRequest` |
| `○ idle` (dim) | The session is alive, and nothing will wake it but a message. | `Stop`; the bridge, for an interrupted turn |
| `error` (red) | The turn failed. | The bridge, for a failed turn |

## How Legato starts Codex

When you start an agent on a task with `a` in the TUI or **Open agent** in the web UI, Legato opens a tmux session in the agent's folder. With the native panel on, which is the default, it types:

```bash
legato agent codex -- <codex arguments>
```

`legato agent codex` starts the session's app server and Legato's bridge, then becomes `codex`, attached to that app server. The pane runs `codex` as usual. If any step fails, for example a Codex too old to share an app server, it says why and starts plain `codex <codex arguments>`.

The Codex arguments are:

- `-c developer_instructions=…` with Legato's launch prompt: the role prompt of a [swarm](/legato/using/swarms/) conductor or worker, the guidance about [Legato's tools](/legato/agent-tools/mcp-tools/), the [resources](/legato/agent-tools/resources/) you gave the agent, and notes for a fresh clone.
- `-c mcp_servers.legato.…` entries that add Legato's tools for this session. Nothing is written to `~/.codex/config.toml`.
- Your `adapters.codex.launch_args`.
- `--model`, when the spawn names a model, such as `swarm.worker.model` for a swarm.
- The `launch_args` of a tier, when a swarm conductor picks one for a worker.

```yaml
adapters:
  codex:
    launch_args: ["--search"]
```

Tiers are set under `adapters.codex.tiers`, each with a `description` and `launch_args`, as for [Claude Code](/legato/agent-tools/claude-code/#how-legato-starts-claude-code). A tier's arguments come after `launch_args`, so a tier's `--model` wins. See the [configuration reference](/legato/reference/configuration/) for every setting.

:::note
Legato's `developer_instructions` replaces a `developer_instructions` of your own in `~/.codex/config.toml`, for the sessions Legato starts.
:::

Codex opens with a menu when a newer version is out, and nothing gets past it until someone answers. An agent may have nobody at its terminal, so Legato starts Codex with `-c check_for_update_on_startup=false`. To keep the check, add `["-c", "check_for_update_on_startup=true"]` to `launch_args`: a later override wins.

## The native panel

The web UI shows a Codex agent in the same native panel it uses for Chimera and Claude Code, from the moment its pane starts. In the panel you can:

- Read the conversation as it streams. A command is a shell card. A file change is a card with a diff for each file.
- Send a message. During a turn, the message joins that turn. A message sent before Codex is ready shows at once and goes to Codex when its conversation opens.
- Interrupt the turn.
- Pick a model and a reasoning effort. The selector lists the models Codex reports. Your choice applies from the next turn Legato starts, and holds for the conversation.
- See how full the context window is.

The terminal and the panel show the same conversation, and either can drive it. `/new` in the terminal starts a new conversation, and the panel follows it.

### Approvals and questions

When Codex asks to run a command or change files, Legato raises an approval: in the web UI's tray, in [Attention](/legato/using/attention/), in the TUI (press `!`), and on your phone. Codex's own dialog stays open in the terminal. The first answer, from either side, settles it.

A command approval offers the choices Codex gives: **Allow once**, **Allow for this session**, and **Allow, and don't ask again for commands that start with …**. If you deny with a note, Legato sends the note as your next message.

A question from Codex shows as a card with its options and a box for your own answer.

Some prompts are only in the terminal, such as a trust question for a new folder or its hooks. When such a prompt is a numbered menu, the panel shows it as **Codex is asking in its terminal**, with a button for each option. Anything else you answer in the terminal.

### See the model's reasoning

Codex sends a summary of its reasoning only when you ask for one. Without it, the panel shows no thinking. Turn summaries on in `~/.codex/config.toml`:

```toml
model_reasoning_summary = "detailed"   # or "concise"
```

Or only for the sessions Legato starts:

```yaml
adapters:
  codex:
    launch_args: ["-c", "model_reasoning_summary=\"detailed\""]
```

### Turn the panel off

```yaml
adapters:
  codex:
    native_bridge: false
```

With `native_bridge: false`, Legato types plain `codex <codex arguments>`, and drives the session through its terminal only. The hooks and Legato's tools still work.

In the web UI, **Downgrade to tmux** in the panel's menu shows one agent through its terminal. **Upgrade to native panel** connects it again while its bridge runs.

## Resume a conversation

Legato saves the Codex conversation ID and folder of each task's agent. When you start an agent on a task that has a saved conversation, the web dialog and the TUI offer to resume it or start a new one. Resume starts `codex resume <id>` in the saved folder, and does not send the launch prompt again.

If the transcript is gone, Codex says so in the terminal. Legato does not start a fresh conversation in its place: start a new session yourself.

**Reload agent** in the web UI, or `R` in the TUI's agents view, stops an agent and starts it again with the same folder and resources, resuming its conversation.

After a crash, an agent whose session is gone is kept as **Interrupted** and resumes the same way, from its saved conversation. See [After a crash: interrupted agents](/legato/using/agents/#after-a-crash-interrupted-agents).

## Limits

- The app-server interface of Codex is experimental and changes between releases. Legato was checked against Codex 0.157.1.
- A model or effort chosen in the panel applies from the next turn Legato starts, not at once.
- The panel does not show subagent conversations.
- Requests for wider permissions and MCP servers' questions are answered in the terminal.
- Chimera-style modes are not available for Codex.

For how the bridge works inside, see [Codex native integration](https://github.com/cpave3/legato/blob/main/docs/ai/codex-native.md).
