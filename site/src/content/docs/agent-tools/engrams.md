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
