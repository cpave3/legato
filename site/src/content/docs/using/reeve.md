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

Settings › **Reeve** has **Start the Reeve**. A session runs with the agent tool, model and launch arguments set there (`reeve.agent`, `reeve.model`, `reeve.launch_args`); you can pick another tool or model for one session before starting it. One session runs at a time.

**End session** stops it. Ended sessions are kept in the history, where **Resume** starts one again with its conversation, and **Delete** removes it.

## Where it runs

The Reeve always runs in Legato's [sandbox](/legato/security/sandbox/): it cannot be started without it, and **Run without sandbox** and the switch out of the sandbox are refused for it. It works in a folder of its own, `~/.local/state/legato/reeve` (or under `$XDG_STATE_HOME`), outside your repositories and Legato's data folder.

Each session gets a token of its own for Legato's API that can only read. It stops working when the session ends.
