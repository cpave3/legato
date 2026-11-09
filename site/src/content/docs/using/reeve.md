---
title: The Reeve
description: The agent of your Legato itself, which you talk to to run and configure that instance.
sidebar:
  order: 45
---

The Reeve is the agent of a Legato instance itself. You talk to it to run and configure that instance: change a setting, find out why a sync failed, read the logs, pair a worker. It is not for working on a task in a repository; that is what your other agents do. Each instance has its own Reeve.

- **It reads freely.** Anything Legato can show, the Reeve can read.
- **It never changes anything by itself.** Every change it wants comes to you as an approval; Legato carries out what you approved.
- **Some things it may never do,** whatever you approve: decide an approval, enroll or remove one of your approval devices, or type into another agent's terminal.

## Starting it

The quickest way is the Reeve entry pinned at the top of the Agents list: pick it to start a session, or to go to the one that runs.


Settings › **Reeve** has **Start the Reeve**. A session runs with the agent tool, model and launch arguments set there (`reeve.agent`, `reeve.model`, `reeve.launch_args`); you can pick another tool or model for one session before starting it. One session runs at a time.

**End session** stops it. Ended sessions are kept in the history, where **Resume** starts one again with its conversation, and **Delete** removes it.

A session that has been idle for two hours is suspended: its agent stops, and it picks up where it left off the next time you send it a message. Change the time in Settings › Reeve (`reeve.idle_timeout`).

## What it knows

A session starts with what it needs to know about this instance: a short snapshot (version and updates, how Legato runs, its workers, open Attention items, running agents, recent errors in the log) and how it is meant to work. "Ask the Reeve" adds a brief about what you asked about. A session you resume is told what changed since it was last here.

While it runs, it has tools of its own:

- **docs**: Legato's user manual, these pages, built into Legato.
- **instance_status**: a fresh snapshot of the instance.
- **read_logs**: the recent lines of Legato's [log file](/legato/running/agent-server/#the-log-file).
- **legato_api**: any of Legato's API calls that only read.

The Reeve does not yet remember earlier sessions: it starts each one without its memory, and says so.

## What a change needs

Each change the Reeve proposes needs your approval, at a level set in Settings › Reeve:

- **A change** (a task, a setting that is not about security): a tap, unless you ask for more.
- **A risky change** (security, restarts and anything that stops work, anything that cannot be undone, any command on the machine): a decision signed on your phone, passkey or security key once one is enrolled; until then a tap.
- **Rules** raise or lower the level of the calls they match, such as `POST /api/tasks*`.

These levels and rules are the Reeve's own rules: they change only with a signed approval, even from Settings, and not at all while no device is enrolled.

## Where it runs

The Reeve always runs in Legato's [sandbox](/legato/security/sandbox/): it cannot be started without it, and **Run without sandbox** and the switch out of the sandbox are refused for it. It works in a folder of its own, `~/.local/state/legato/reeve` (or under `$XDG_STATE_HOME`), outside your repositories and Legato's data folder.

Each session gets a token of its own for Legato's API that can only read. It stops working when the session ends.
