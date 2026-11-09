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
- **legato_api**: any of Legato's API calls; a change becomes a change set for you to approve.
- **legato**: a `legato` command on the machine, for what only the command line does (integration tokens, connecting Argus, `columns sync`, `hooks install`). A command that only reads runs at once; one that changes something is a change set. Some commands it never runs, such as those that hand out secrets or reset your devices.

The Reeve does not yet remember earlier sessions: it starts each one without its memory, and says so.

## Its changes

When the Reeve wants to change something, it proposes a **change set**: the calls of one step, with a one-line reason, put to you as one approval in the approval tray and on your phone. You see each call, and for a setting its value before and after. Approve it, and Legato runs exactly those calls, in order, a restart last, stopping at the first failure; the Reeve is told how each went. Deny it with a note, and the Reeve gets the note and does not propose the same change again unless you ask.

The approval card says where the set comes from (the Reeve and the instance), its ID, how many changes and what it needs; each change with its kind (change, risky), the call it makes, and a setting's value before and after. In the Reeve's conversation, each change set is a line with its state: waiting, or done with how many of its calls ran.

Ending a session withdraws the change sets still waiting for you; a suspended session keeps them.

### Restarts, config.yaml and secrets

- The Reeve can propose a **restart** of Legato. When Legato is back, the Reeve is told: when it restarted, on which version, how each step went, and which settings now apply.
- It can propose an **edit to `config.yaml`**, for a setting that lives only in the file or one the file pins. Legato checks the edited file loads before it asks you, and shows you the change as a diff. Approved, it writes the file with its comments kept and the old one beside it as a backup, then restarts. **Approve without the restart** writes the file and leaves the restart for later.
- When it needs a **secret** (an API key, a token), it asks for it by name, and a form in the approval tray takes the value. Legato keeps it in its own secret store, and the Reeve only ever gets the name. Never paste a secret into the conversation.

### Commands on the machine

With host operations on (the default; Settings › Reeve, changed only with a signature), the Reeve can also propose, as a last resort, a shell command, writing a file outside Legato (you see a diff; the old file is kept as a backup), or starting, stopping or reading a systemd service. Each needs your approval. They run as Legato's user, in its home folder, with a small fixed environment, for at most ten minutes, and cannot gain privileges: `sudo` fails. Only a call the Reeve marks **as root** runs as root, and it always needs your signature with a fingerprint or PIN. Every host action is written to the audit log: what ran, whether as root, who approved it with which device, the exit code and a fingerprint of the output.

## What a change needs

Each change the Reeve proposes needs your approval, at a level set in Settings › Reeve:

- **A change** (a task, a setting that is not about security): a tap, unless you ask for more.
- **A risky change** (security, restarts and anything that stops work, anything that cannot be undone, any command on the machine): a decision signed on your phone, passkey or security key once one is enrolled; until then a tap.
- **Rules** raise or lower the level of the calls they match, such as `POST /api/tasks*`.

These levels and rules are the Reeve's own rules: they change only with a signed approval, even from Settings, and not at all while no device is enrolled.

## Where it runs

The Reeve always runs in Legato's [sandbox](/legato/security/sandbox/): it cannot be started without it, and **Run without sandbox** and the switch out of the sandbox are refused for it. It works in a folder of its own, `~/.local/state/legato/reeve` (or under `$XDG_STATE_HOME`), outside your repositories and Legato's data folder.

Each session gets a token of its own for Legato's API that can only read. It stops working when the session ends.
