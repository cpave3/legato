---
title: Engrams
description: Start an agent session as one of your engrams, with its memories and open threads already in the session.
sidebar:
  order: 70
---

An engram is a named individual that the [engram](https://github.com/cpave3/engram) command keeps: argus, hestia, mneme, each with its own memories, open threads and handoffs. Legato can start an agent session **as** an engram, so the session begins with what that engram knows instead of you telling the agent who it is and asking it to run `engram boot`.

## What you need

Install the engram command, in a version that has `engram where`. Legato looks for it on the `PATH` of the Legato server, or where `engram.command` says.

Legato asks engram where it is and which engrams it keeps each time you open a spawn dialog. Installing engram later needs no restart.

## Settings

| Setting | Default | What it does |
| --- | --- | --- |
| `engram.enabled` | `auto` | `auto` offers engrams when the engram command answers. `on` does the same, and logs an error when it does not answer. `off` hides everything about engrams. |
| `engram.command` | none | The engram binary to run. Without it, Legato runs `engram` from its `PATH`. |

Both can be changed in Settings or in `config.yaml`; see [Configuration](/legato/reference/configuration/#engram).

## Start an agent as an engram

In the web UI's spawn dialog, pick an engram in **Engram**, under **Agent**. **No engram** starts a normal session. Only Claude Code, Codex and Chimera agents can be started as an engram; a shell cannot.

The agent starts with the engram's boot packet at the top of its launch prompt, and with the engram's name, store and session in its environment, so its own `engram` commands write to that engram. Legato adds no guidance of its own about keeping the engram current: the boot packet says when to note, remember and hand off.

If the engram does not boot (engram fails, takes longer than 5 seconds, no longer has that engram, or its store is missing), the agent starts anyway, as a normal session, and Legato says why.

In the TUI's spawn dialog, the `engram` row comes right after `agent`: `‹ mneme · default ›  2/11`. Press `h` and `l` to go through the engrams in the same order as the web dialog. When remembering would change something, a line `[ ] remember for this repo (r)` appears under it; press `r` on the row to tick it. If the engram does not boot, the status bar says why.

## Engrams for a repository

The **Engram** list starts with **No engram**, then the engrams of **This repo** (or **This folder**, for a folder outside git), then the other engrams:

1. the engram this task last ran as, marked **last on this task**;
2. the repository's default engram, marked **default**;
3. the other engrams bound to the repository, the most recently used first;
4. every other engram, A to Z.

The dialog picks the task's last engram first, else the repository's default, else No engram. When you change **Where**, the list and the pick follow the new place.

Tick **Remember for this repo** to bind the engram you picked to the repository and make it the default there; the old default stays bound. With **No engram** picked, the box reads **Clear this repo's default**, and the bound engrams stay. The box shows only when ticking it would change something. A repository is named the way engram names it, such as `github.com/cpave3/legato`, so every clone of it shares its engrams.

An engram that engram no longer lists is left out of the list, and its bindings are kept: if it comes back, they work again.

## Restarting and ending

Restarting an engram agent keeps its engram. When its conversation is resumed it keeps its engram session too, and the boot packet is not sent again; otherwise the engram boots again in a new session.

When an engram agent ends for good, Legato tells engram the session is over (`engram session end`). Legato does not ask the agent for a handoff: the boot packet tells it to hand off at stopping points.

## Engram bindings in Settings

**Settings › Agents › Engram bindings** lists each repository or folder that has engrams bound to it, with its default marked and any engram that engram no longer lists marked **missing**. You can make a bound engram the default, unbind one, or clear the default. Engrams are bound with **Remember** in the spawn dialog, not here. The section also says where engram and its store are, or that engram was not found.

## In workflows

A workflow's `agent.run` step can start its agent as an engram with `with.engram`. See [Workflows](/legato/using/workflows/).

## Not offered

Swarm conductors and workers, and tasks handed off to another Legato, cannot be started as an engram. Legato does not create or edit engrams: use the engram command for that. The one exception is [the Reeve](/legato/using/reeve/#its-memory)'s own engram, which Legato makes, in a store of its own that never shows among yours.
