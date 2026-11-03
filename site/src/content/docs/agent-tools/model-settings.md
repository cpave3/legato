---
title: Model settings
description: Thinking, reasoning effort and generation controls for a native agent.
sidebar:
  order: 40
---

Model settings let you change how the model of one agent session thinks and writes, such as its reasoning effort, from the web UI while you talk to it. A change applies to that session only: it does not change your configuration or other agents.

The controls are in the native agent panel, the conversation view of an agent that Legato drives directly (see [Agents](/legato/using/agents/)). A terminal session does not have them.

## Opening the settings

1. In the native agent panel, click the model chip in the message box. It shows the model, its context window and the effort in force.
2. The menu lists the models you can switch to and, when the model takes one, the effort levels. Click a level to change the effort at once.
3. Click **More session settings…** to open **Model settings**.

The form names the model and the model it resolved to. Only the settings that this model supports appear:

- **Thinking** and **Reasoning effort** come first.
- **Advanced** holds the rest the model supports: **Thinking budget**, **Reasoning summary**, **Verbosity**, **Service tier**, **Output limit**, **Temperature**, **Top-p** and **Parallel tool calls**.

Setting **Thinking** to Off also turns off **Reasoning effort** and **Thinking budget**.

## Applying a change

- Your edits stay in the form until you click **Apply**. They then apply to this session.
- **Cancel**, or Escape, closes the form and discards your edits.
- **Reset to defaults** clears every setting you made for this model in this session. It takes effect when you click **Apply**.
- You cannot apply settings while the agent is working on a turn. The form says so; wait for the turn to finish.
- When a save fails, the form keeps your edits and shows the error.

**Default** means that you set nothing: the session uses the value from the model's configuration, else the model provider's own default. The form shows that value beside **Default** when it is known. Off, false and zero are values of their own, not the default.

When the agent changes model while the form is open, the form asks you to reopen it, so that you never apply one model's settings to another.

## Which agents have which settings

| Agent tool | Settings |
| --- | --- |
| [Chimera](/legato/agent-tools/chimera/) | Every setting that Chimera reports the model supports |
| [Claude Code](/legato/agent-tools/claude-code/) with Legato's native bridge | **Reasoning effort** |
| [Codex](/legato/agent-tools/codex/) with Legato's native bridge | **Reasoning effort**, when the model lists its effort levels |

For Chimera, the settings come from Chimera itself, so they need:

- a Chimera release that has the session settings API;
- a source of capability information for the model in Chimera's configuration. For models served through Manticore, add `"capabilitySource": "manticore"` to the Manticore provider, with a Manticore release that reports model capabilities. For other servers, such as an unknown local endpoint, declare a model profile in Chimera.

Without them, the form says that settings information is unavailable for this model. Chimera's own `docs/model-settings.md` has provider examples and local-server declarations.

What a model supports differs from model to model. For example, Codex models require thinking, a reasoning summary can be turned off without turning off reasoning, and some Kimi models have fixed sampling settings. When a setting in force is one the current model does not support, the form names it and says why.

## Models that read images

In the model menu, an eye (👁) marks a model that Chimera has found can read images. When you send an image to a model without the mark, Chimera routes the turn to the fallback model in its configuration. Legato itself does not route images.

Internals (the API, the code, the rollout across Manticore, Chimera and Legato) are in [docs/ai/model-settings.md](https://github.com/cpave3/legato/blob/main/docs/ai/model-settings.md) on GitHub.
