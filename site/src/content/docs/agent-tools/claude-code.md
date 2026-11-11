---
title: Claude Code
description: How Legato starts Claude Code, reports what it is doing on the board, and connects it to the web UI's native panel.
sidebar:
  order: 10
---

Legato starts Claude Code in a tmux session for a task, shows on the card whether it is working or needs you, and connects the session to Legato directly. With that connection you read and drive the conversation in the web UI's native panel, and you answer Claude Code's permission prompts and questions from Legato, your phone or the terminal.

Three parts make this work:

- **Activity hooks** that you install once. They set the state on the card.
- **The Legato mod**, which Legato loads into every Claude Code session it starts. It gives the native panel and lets Legato send messages and answer prompts.
- **The `legato-tools` plugin**, which gives the agent [Legato's tools](/legato/agent-tools/mcp-tools/) while `agents.mcp.enabled` is on, as it is by default. It also lets the agent read images you paste for it without asking you.

Legato installs the mod and the plugin by itself. Only the hooks need a command from you.

## Install the activity hooks

The hooks tell Legato when Claude Code starts work, waits for you, and stops. Without them a card shows no activity, even when the native panel works.

1. Go to the folder whose `.claude/` folder should hold the hooks. A project folder gives the hooks to that project. Your home folder gives them to every project, because Claude Code reads `~/.claude/settings.json` everywhere.
2. Run:

   ```bash
   legato hooks install --tool claude-code
   ```

The command writes five scripts to `.claude/hooks/legato-*.sh` and adds them to `.claude/settings.json`. It fails when the folder has no `.claude/` folder.

Legato adds its scripts to the `UserPromptSubmit`, `PostToolUse`, `Stop`, `PermissionRequest` and `SessionEnd` events. Your own hooks, on those events or others, and the rest of `settings.json` stay as they are. Running the command again replaces Legato's entries and does not add a second copy. `legato hooks uninstall --tool claude-code` removes only Legato's entries and scripts.

The scripts do nothing outside a session that Legato started. Inside one, they report to every running Legato.

### What the card shows

| Card | Meaning | Set by |
| --- | --- | --- |
| `⠹ running` (green) | Claude is working. | `UserPromptSubmit`, `PostToolUse` |
| `◆ needs you` (amber) | Claude waits for your approval. | `PermissionRequest` |
| `◐ background` (cyan) | The turn ended, but a shell or subagent it started still runs. The session wakes by itself when that finishes. | `Stop` |
| `○ idle` (dim) | The session is alive and not working. | `Stop`, `SessionEnd` |

A card shows `ready for review` in place of `idle` when the agent marked its [review](/legato/using/reviews/) ready.

If you installed the hooks before the `background` state existed, run `legato hooks install --tool claude-code` again. Until you do, the card goes to idle at every stop.

## How Legato starts Claude Code

When you start an agent on a task with `a` in the TUI or **Open agent** in the web UI, Legato opens a tmux session in the agent's folder and types `claude` into it, with these additions:

- `--append-system-prompt` with Legato's launch prompt, when there is one: the role prompt of a [swarm](/legato/using/swarms/) conductor or worker, the [resources](/legato/agent-tools/resources/) you gave the agent, and notes for a fresh clone.
- Your `adapters.claude-code.launch_args`.
- `--model`, when the spawn names a model, such as `swarm.conductor.model` or `swarm.worker.model` for a swarm.
- The `launch_args` of a tier, when a swarm conductor picks one for a worker.

```yaml
adapters:
  claude-code:
    launch_args: ["--verbose"]
    tiers:
      small:
        description: Quick, narrow changes
        launch_args: ["--model", "claude-haiku-5-5"]
      large:
        description: Hard design work
        launch_args: ["--model", "claude-opus-5-5"]
```

A tier's arguments come after `launch_args`, so a tier's `--model` wins. See [Swarms](/legato/using/swarms/) for how a conductor picks tiers, and the [configuration reference](/legato/reference/configuration/) for every setting.

The session's environment holds `LEGATO_TASK_ID` and `LEGATO_EXECUTION_ID`, which the hooks and tools use, and `CLAUDE_CODE_PLUGIN_DIRS`, which loads the mod and the plugin. A `claude` that you start by hand in the same pane loads them too.

### The folder trust question

Claude Code asks whether you trust a folder the first time it starts there. An agent may have nobody at its terminal, so Legato answers for it: before it types `claude`, it marks the folder as trusted in `~/.claude.json`, as accepting the dialog does. If that fails, Legato logs why and Claude Code asks as usual.

## The native panel

With the mod loaded, the web UI shows a Claude Code agent in the same native panel it uses for Chimera and Codex, a few seconds after Claude Code starts. In the panel you can:

- Read the conversation as it streams, with a card for each tool call and subagent.
- Send a message. A message sent during a turn joins that turn when Claude next finishes a tool call, and shows in the conversation from then. If the turn ends first (Claude's last reply calls no tool, or you interrupt it), the message becomes Claude's next prompt. Either way Claude reads it once. `/name args` runs a slash command of the session.
- Run `!command` in the agent's folder. Its output goes to the session for the model's next request, and no turn starts.
- Interrupt the turn.
- Pick a model and a reasoning effort for this session only. Legato does not change Claude Code's saved defaults.
- See how full the context window is.

The terminal stays live beside the panel. Anything you do in the terminal shows in the panel, and the other way round.

### Permissions, plans and questions

When Claude Code asks for a permission, Legato raises it as an approval: in the web UI's tray, in [Attention](/legato/using/attention/), in the TUI (press `!`), and on your phone. Claude Code's own dialog stays open in the terminal. The first answer, from either side, settles it.

- **Permissions.** Allow, deny with a note for the agent, or take Claude Code's "don't ask again" choice when it offers one. Legato saves that choice where the terminal would.
- **Plans.** A plan from plan mode shows as Markdown. You approve it and pick how edits go afterwards: accept edits without asking, or ask before each edit.
- **Questions.** When Claude asks a question with options, the panel shows a card with the options, a box for your own answer, and **Dismiss**.

Some prompts are only in the terminal, such as a picker opened with `/resume` or `/model`. When such a prompt is a numbered menu, the panel shows it as **Claude Code is asking in its terminal**, with a button for each option. Anything else you answer in the terminal.

### Turn the panel off

```yaml
adapters:
  claude-code:
    native_bridge: false
```

With `native_bridge: false`, Legato does not load the mod. Sessions are driven only through their terminal, and messages are typed into the pane. The hooks and Legato's tools still work. A session started before the change keeps what it had until you start it again.

In the web UI, **Downgrade to tmux** in the panel's menu shows one agent through its terminal. **Upgrade to native panel** connects it again while its session runs.

### Models in the selector

The model selector lists the session's own model and the models you name:

```yaml
adapters:
  claude-code:
    models:
      - claude-opus-5-5
      - claude-sonnet-5-5
      - claude-haiku-5-5
```

Use full model IDs, not aliases such as `sonnet`: the choice is applied to each request, where Claude Code no longer reads aliases. To use a model that is not listed, type `/model <id>` in the panel's message box. `/model default` goes back to the session's own model. Effort offers `low`, `medium`, `high`, `xhigh` and `max`.

The terminal's status line still shows the session's configured model. The panel shows the model the requests use.

## Resume a conversation

Legato saves the Claude Code session ID and folder of each task's agent. When you start an agent on a task that has a saved conversation, the web dialog and the TUI offer to resume it or start a new one. Resume types `claude --resume <id>` in the saved folder, and does not send the launch prompt again.

If Claude Code deleted the transcript, it says so in the terminal. Legato does not start a fresh conversation in its place: start a new session yourself.

**Reload agent** in the web UI, or `R` in the TUI's agents view, stops an agent and starts it again with the same folder and resources, resuming its conversation. Use it after you change plugins, MCP servers or hooks.

After a crash, an agent whose session is gone is kept as **Interrupted** and resumes the same way, from its saved conversation. See [After a crash: interrupted agents](/legato/using/agents/#after-a-crash-interrupted-agents).

## Token usage in Manticore

With a `manticore:` block in the configuration, Legato loads a second mod, `legato-manticore`, into each Claude Code session. It reports the token counts of every model request to your Manticore instance, and never the prompt or the reply. It works with or without the native panel. See [Manticore](/legato/integrations/manticore/).

## Limits

- The mod uses an early-access interface of Claude Code, which changes between releases. Legato was checked against Claude Code 2.1.296. If a hook of the mod fails, Claude Code skips it, and the session works through its terminal.
- The mod does not report activity. A session without the activity hooks shows no state on its card.
- A session that you send to the background with `/background` keeps its task. A `/fork` serves no task.
- Legato's tools in a backgrounded session can act for another task, or offer nothing.
- The panel shows thinking only when the model gives text for it, which current Claude models seldom do.

For how the mod and its bridge work inside, see [Claude Code native integration](https://github.com/cpave3/legato/blob/main/docs/ai/claude-code-native.md).
