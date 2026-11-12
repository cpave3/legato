---
title: The Reeve
description: The agent of your Legato itself, which you talk to to run and configure that instance.
sidebar:
  order: 45
---

The Reeve is the agent of a Legato instance itself. You talk to it to run and configure that instance: change a setting, find out why a sync failed, read the logs, pair a worker. It is not for working on a task in a repository; that is what your other agents do. Each instance has its own Reeve.

- **It reads freely.** Anything Legato can show, the Reeve can read.
- **It never changes anything by itself.** Every change it wants comes to you as an approval; Legato carries out what you approved.
- **Some things it may never do,** whatever you approve: decide an approval, enroll or remove one of your approval devices, read or open a pairing request, or type into another agent's terminal.

## Starting it

The quickest way is the Reeve entry pinned at the top of the Agents list: pick it to start a session, or to go to the one that runs. In the TUI, press `R` from any view.

**Ask the Reeve** starts it about something specific: it is on each item in Attention, on the note the board keeps when a provider sync fails, and beside an agent that needs you. The session starts with a brief of what it is, its IDs and its error. If a session is already there, Legato asks whether to **Add to the current conversation** or **End it and start fresh**.


Settings › **Reeve** has **Start the Reeve**. A session runs with the agent tool, model and launch arguments set there (`reeve.agent`, `reeve.model`, `reeve.launch_args`); you can pick another tool or model for one session before starting it. One session runs at a time.

**End session** stops it, and so does killing its agent from the Agents list or the TUI's agents view. Ended sessions are kept in the history, where **Resume** starts one again with its conversation, and **Delete** removes it.

A session that has been idle for two hours is suspended: its agent stops, and it picks up where it left off the next time you send it a message. Change the time in Settings › Reeve (`reeve.idle_timeout`).

## Give it a name

Each instance's Reeve can have a name. Set it in Settings › Reeve (`reeve.name`), such as Tessitura, and Legato says it wherever it would say "the Reeve": its entry in the Agents list, its conversation, Ask buttons, approvals and the approval tray, notifications, the audit log, the desktop app's list of approvals and the TUI. The Reeve is told its name too, from its next session or the next time it resumes. Everywhere else shows a change at once. The desktop app's voice capsule and the TUI's key help still say "the Reeve".

A name is one line of plain text, at most 40 characters: letters, digits, spaces, apostrophes, hyphens and dots. Leave it empty and Legato keeps saying "the Reeve". The name changes only what you see: the Reeve may do no more or less, and the manual, settings and the API still call the role the Reeve.

An approval keeps the name the Reeve had when it asked.

## What it knows

A session starts with what it needs to know about this instance: a short snapshot (version and updates, how Legato runs, its workers, open Attention items, running agents, recent errors in the log) and how it is meant to work. "Ask the Reeve" adds a brief about what you asked about. A session you resume is told what changed since it was last here.

While it runs, it has tools of its own:

- **docs**: Legato's user manual, these pages, built into Legato. Its search puts first the pages with your words together on a line, so "move a card" or "stop an agent" finds the [API reference](/legato/reference/api/).
- **instance_status**: a fresh snapshot of the instance.
- **read_logs**: the recent lines of Legato's [log file](/legato/running/agent-server/#the-log-file).
- **legato_api**: any of Legato's API calls, listed on the [API reference](/legato/reference/api/); a change becomes a change set for you to approve. Every `GET` reads without asking, except the few that page marks otherwise, and a route Legato does not serve answers 404 at once. To check on a task or an agent it reads them in brief (`?view=summary`, `?view=compact`), which keeps its conversation short.
- **legato**: a `legato` command on the machine, for what only the command line does (integration tokens, connecting Argus, `columns sync`, `hooks install`). A command that only reads runs at once; one that changes something is a change set. Some commands it never runs, such as those that hand out secrets or reset your devices; when it asks for one, it is told what to use instead.

## Its memory

The Reeve remembers earlier sessions through an [engram](/legato/agent-tools/engrams/) of its own, named after the instance (`reeve-` and eight characters of the instance's ID). It keeps the why and the how there as it works: your preferences, why settings are as they are, problems and how they were fixed, and work left open. Legato's own records already hold what changed and who approved it. Secrets are never kept.

- **Legato makes it** the first time a session starts, from a built-in seed, in a store of its own inside the Reeve's folder (`~/.local/state/legato/reeve/engram`). It never shows among your own engrams, in `engram identity list` or in the spawn dialog.
- **A resumed session** carries on in the same engram session; a new session starts with what earlier ones kept.
- **Ending a session** ends its engram session too. A session suspended for being idle keeps it.
- **If its engram cannot start** (engram is missing, or its store cannot be written), the Reeve starts anyway, says so in its first reply, and keeps nothing that session. An item in Attention, **The Reeve's engram did not boot**, says why. With engrams turned off (`engram.enabled: off`) it starts without one and raises no item.

The conversation's status bar names the engram the Reeve runs as, or says **no engram**.

### In Settings

**Settings › Reeve › The Reeve's engram** shows its name and where it is kept, and:

- **The seed**: who the Reeve is and how it should work, the one part of its engram it treats as your instruction. Change it and choose **Save the seed**; like the Reeve's other rules, the change needs your signature on your phone or with a passkey, and the next session starts with it. Legato never overwrites a seed you changed. When a newer version of Legato has a new built-in seed, Settings offers it: **Use the new built-in seed** (with a signature too).
- **Memories and open threads**: what it keeps, each with **Remove**. Removing a memory deletes it for good; removing a thread closes it. To edit a memory, use the engram command with `ENGRAM_HOME` set to the store shown.

## Its changes

When the Reeve wants to change something, it proposes a **change set**: the calls of one step, with a one-line reason, put to you as one approval in the approval tray and on your phone. You see each call, and for a setting its value before and after. Approve it, and Legato runs exactly those calls, in order, a restart last, stopping at the first failure; the Reeve is told how each went. Deny it with a note, and the Reeve gets the note and does not propose the same change again unless you ask.

The approval card says where the set comes from (the Reeve and the instance), its ID, how many changes and what it needs; each change with its kind (change, risky), the call it makes, and a setting's value before and after. In the Reeve's conversation, each change set is a line with its state: waiting, or done with how many of its calls ran. A set that ran without asking (see [Running without asking](#running-without-asking)) is marked **auto**.

Ending a session withdraws the change sets still waiting for you; a suspended session keeps them.

### Restarts, config.yaml and secrets

- The Reeve can propose a **restart** of Legato. When Legato is back, the Reeve is told: when it restarted, on which version, how each step went, and which settings now apply.
- It can propose an **edit to `config.yaml`**, for a setting that lives only in the file or one the file pins. It can change an entry of a list too, such as one rule of a credential, or add one. Legato checks the edited file loads before it asks you, and shows you the change as a diff. Only the lines it changes are rewritten. Approved, it writes the file with its comments kept and the old one beside it as a backup, then restarts. **Approve without the restart** writes the file and leaves the restart for later.
- When it needs a **secret** (an API key, a token), it asks for it by name, and a form in the approval tray takes the value. Legato keeps it in its own secret store, and the Reeve only ever gets the name. Never paste a secret into the conversation.

### Commands on the machine

With host operations on (the default; Settings › Reeve, changed only with a signature), the Reeve can also propose, as a last resort, a shell command, writing a file outside Legato (you see a diff; the old file is kept as a backup), or starting, stopping or reading a systemd service. Each needs your approval, unless you let them run without asking (below). They run as Legato's user, in its home folder, with a small fixed environment, for at most ten minutes, and cannot gain privileges: `sudo` fails. Only a call the Reeve marks **as root** runs as root, and it always needs your signature with a fingerprint or PIN. Every host action is written to the audit log: what ran, whether as root, who approved it with which device (or, for one that ran without asking, the setting or rule that let it), the exit code and a fingerprint of the output.

**Run host operations without asking** (`reeve.host_operations_auto`, off by default) lets the Reeve's commands, file writes and service actions that are not as root run at once. Turned on, **the Reeve can run any command as your user on this machine without asking you**, as an agent outside the sandbox can. A command as root still needs your signature with a fingerprint or PIN. Turning it on needs a signed approval; turning it off never does.

To let only some of them run, add a rule at `auto` instead (see [What a change needs](#what-a-change-needs)), written as the kind of call, a colon and what it acts on:

- `host_shell:journalctl *`, `host_shell:coredumpctl list*`: a command that starts so. A command with `;`, `|`, `&`, `$(`, a backquote or a redirect is more than the command a rule names, and still asks.
- `edit_file:~/Projects/**`: a file in that folder. `~` is your home folder; `*` matches any run of characters, slashes too. A path with `..` in it still asks.
- `manage_service:status *`, `manage_service:restart legato.service`: an action and a unit.

### Watching agents

The Reeve can look after agents while you are away: start one on a task, hear back when it needs something, and decide what comes next. It watches every agent it starts or messages, and you can ask it to watch any other.

A watched agent wakes the Reeve with a `[legato agent]` message in its conversation when it:

- waits for you: a question or a permission prompt;
- has been idle for a while (30 seconds), or busy only with work it left running in the background;
- stops with an error, or exits;
- adds a note to its task, or has a pull request opened, or its checks or review change.

The message has the agent's last words, which often say what it waits on ("waiting for the build"), and whether anything it started still runs. For a Claude Code agent in the background it names each job, such as `bash "npm run build"` or `subagent "Explore: find the callers"`, so the Reeve can tell an agent waiting on a build from one that has stalled. An idle agent has nothing that will wake it but a message. The Reeve then reads the task's notes and decides: leave the agent alone, message it, tell you, or stop it and start the next task, still one change at a time for you to approve. It never answers a permission prompt: it tells you what the agent asks.

A short pause doesn't wake it, and news that comes close together arrives as one message, only between the Reeve's turns. When no Reeve session is running, the news waits for the next one. Turn on `reeve.watch.wake` to have Legato resume or start the Reeve instead, which costs tokens while you are away. Watching asks no approval, and outlasts a restart of Legato or of the Reeve. A watch ends when its agent exits.

| Setting | What it does |
| --- | --- |
| `reeve.watch.settle` | How long an agent must stay idle before the Reeve is told. `30s` by default. |
| `reeve.watch.started` | Watch every agent the Reeve starts or messages. On by default. |
| `reeve.watch.wake` | Resume or start the Reeve to tell it. Off by default. |

A [swarm](/legato/using/swarms/) is another way to run many agents: a conductor agent splits one task among workers, with a plan you approve. Watching is the Reeve running ordinary agents on ordinary tasks, each step a change you approve.

## What a change needs

Each change the Reeve proposes needs your approval, at a level set in Settings › Reeve:

- **A change** (a task, a setting that is not about security): a tap, unless you ask for more.
- **A risky change** (security, restarts and anything that stops work, anything that cannot be undone, any command on the machine): a decision signed on your phone, passkey or security key once one is enrolled; until then a tap.
- **Rules** raise or lower the level of the calls they match, such as `POST /api/tasks*`.

Each level is one of **auto** (it runs at once, with no approval asked), **approve** (a tap), **signed** (a decision signed on an enrolled device) or **verified** (signed, with a fingerprint or a PIN). A risky change's **default** is signed once a device is enrolled, else a tap.

These levels and rules, host operations and whether they run without asking, the Reeve's peer setting and the seed of its engram, are the Reeve's own rules: they change only with a signed approval, even from Settings, and not at all while no device is enrolled. Making one stricter (a higher level, an `auto` rule raised or removed, host operations or running them without asking turned off) needs no signature.

### Running without asking

At **auto**, a change set runs as soon as the Reeve proposes it. It is still recorded as an approval, decided by the level or rule that allowed it, every call is written to the audit log, it shows for a few seconds at the corner of the screen as already done, and its line in the conversation is marked **auto**. If any call of a set needs more, the whole set waits for you.

Use it for the changes you approve without reading: set **What a change needs** to `auto` for tasks, agents and their messages and ordinary settings, or add rules for narrow patterns while the class stays at approve, such as `POST /api/tasks*`, `PATCH /api/tasks/*` or `POST /api/tasks/*/agent`. A risky change at `auto` includes restarts, stopping agents and settings that choose what runs on this machine: choose it only on purpose. In `config.yaml`, `risky: auto` is read as `default`, which is what it meant before `auto` was a level.

Some calls always wait for you, whatever is set to auto:

- a command as root (signed, with a fingerprint or PIN);
- a change to the Reeve's own rules or its seed (and, when it runs, its own signed approval);
- an edit of `config.yaml`, which can change anything;
- any deletion (`DELETE`, and `legato` commands that remove or discard);
- security settings: credentials and credential sources, the sandbox, the SSH agent socket, GitHub credentials, approval notifications, agent tools and their launch arguments, integrations, and the Reeve's voice command, credential and player;
- pairing, workers and mains, Reeve pairings and connecting integrations;
- secrets, integration tokens and credentials through the `legato` command.

Calls the Reeve may never make (deciding approvals, approval devices, the web token) stay refused.

**Settings › Reeve › Runs without asking** lists what is at auto, with what each lets the Reeve do, and every set that ran without asking. **Ask for everything** puts every level, rule and host operation at auto back to approve at once, with no signature. A value `config.yaml` sets is named there; change it in the file.

## Talking to other instances' Reeves

The Reeve can message the Reeve of another Legato this one is paired with: its workers, its main instances, and instances you pair in **Settings › Reeve › Reeve pairings**. It asks; it never changes the other instance. Whatever the other Reeve does there, that instance's person approves.

- **To pair two instances**, choose **Let another instance pair with this one** on one of them. It shows an address and four words; on the other, enter them under Reeve pairings and choose **Pair**.
- A main instance's Reeve can message its workers' without another pairing. On a worker, **Settings › Main instances** has a switch for each main: **Its Reeve may message this one's**.
- Change sets that wait on a paired instance show in this instance's approvals tray too, marked with it, and are decided there.
- A message from another Reeve shows in the conversation as its own block, marked "From the Reeve on <instance> · <role>", with the Reeve's violet edge. When no session runs here, **Reeve › When another Reeve messages** (`reeve.peer`) decides: start a session (the default), ask you first in Attention (**Let it talk**), or refuse. It is one of the Reeve's own rules, changed only with a signature.

## The Reeve's voice

The Reeve can talk out loud on the speakers of the machine Legato runs on. It speaks a short line, one to three sentences with the outcome first, and leaves the detail to its written reply. To a typed message it answers in writing, unless you ask it to say something out loud. When you talk to it (see [Talking to the Reeve](#talking-to-the-reeve)), it answers out loud by itself.

To turn it on, in **Settings › Reeve**:

1. Set **Voice command** (`reeve.voice.command`) to a command that makes a line's audio. Legato runs it as `<command> "<text>" <out-file>`, and it must write an audio file to `<out-file>`. For the ElevenLabs voice, that is the path to `reeve-say.sh`.
2. For a paid voice, set **Voice credential** (`reeve.voice.credential`) to the credential that holds its key, as `elevenlabs`. Legato gives the command the key as `ELEVENLABS_API_KEY` (or the variable in `reeve.voice.credential_env`). The Reeve never sees the key, no approval is asked, and each use is written to the credential audit log.
3. Turn on **The Reeve's voice** (`reeve.voice.enabled`), and start a new Reeve session: a session gets the `speak` tool when it starts.

Lines play one at a time, in order, with `pw-play` (or `ffplay` if `pw-play` fails), or with the command in **Voice player** (`reeve.voice.player`). A line over about 400 characters is refused. A line that fails to be made or played is written to Legato's log, and the next one still plays. Turn **The Reeve's voice** off to mute it at once. Legato keeps no audio: each file is removed once it has played.

The conversation keeps a record of each line. In the web UI it is a bubble marked **◈ Said** with the words. In the TUI's ensemble it is a line `◈ said "…"`. The Reeve does not write the same words again in its reply.

To switch to a local voice such as Piper, set the voice command to a Piper script and clear the voice credential.

### Talking to the Reeve

You can also talk to the Reeve and hear its answers, from the desktop and Android apps. This needs, on the main instance:

- [Voice dictation](/legato/reference/configuration/) on (`voice.enabled`) with a Whisper server (`voice.whisper_url`). It can be the Whisper that Home Assistant Assist already uses. Both settings take a restart.
- The Reeve's voice, set up as above.

What you say reaches the Reeve marked as spoken, and it answers out loud, in a few short sentences, and writes only what cannot be said, such as a path or a link. A message you type gets a written answer, as before: voice lasts for one message. If the Reeve answers a spoken message only in writing, the app says so.

Press the key or the button and speak. When you stop for about a second (**Pause that ends a spoken turn**, `reeve.voice.pause`), what you said is sent to the Reeve, and the words appear as you speak (**Words as you speak**, `reeve.voice.partials`). If you go on speaking within two seconds, before the Reeve answers, that is sent too. While the Reeve works, the app shows what it is doing. After it has answered out loud, the app listens for your reply for six seconds (**Listen for a reply for**, `reeve.voice.follow_up`); press the key again, or Cancel, to stop. Only one device talks to the Reeve at a time: starting on another takes the conversation over.

The Reeve's answer plays on the device you are talking to, not on the main's speakers, with its words shown as it plays. The microphone is off while it plays. **Stop** drops the rest of the answer; while the Reeve is still working, it interrupts it. The main keeps each line's audio only until your device has played it, ten minutes at most, and the voice key never leaves the main.

**Quiet hours** (`reeve.voice.quiet_hours`, as `22:00-07:00` in the main's local time): no line plays on the main's speakers then, and the Reeve replies in writing. A reply to something you said to it still plays on your device.

In the web UI, the ring beside the Reeve in the Agents list opens a voice sheet. It needs the page over HTTPS (a browser gives a page served over plain HTTP no microphone), or the Legato app. On a worker's page the sheet says the voice is on the main; in the Android app, **Open the main** goes there. When the sheet opens on a page you haven't touched yet (a reload with the sheet open), it waits for you to tap **Talk**. Press Escape to close it. The transcription settings (`voice.*`) are in Settings, under Reeve.

Before it listens, an app asks the main whether a conversation will work. If not, it shows one line saying why, such as "Whisper isn't answering on tessitura", and which setting to change. When the Reeve is suspended, that question also starts resuming it, so it is ready by the time you finish speaking.

## Where it runs

The Reeve always runs in Legato's [sandbox](/legato/security/sandbox/): it cannot be started without it, and **Run without sandbox** and the switch out of the sandbox are refused for it. It works in a folder of its own, `~/.local/state/legato/reeve` (or under `$XDG_STATE_HOME`), outside your repositories and Legato's data folder. The folder also holds its engram's store.

It reads your agents' work from Legato's copy of each repository, which it can see but not change. Legato fetches that copy each time an agent pushes, so the Reeve has an agent's latest commits within seconds of the push. It cannot see the agents' own clones, which hold files copied from your checkout (such as `.env`) and the key each agent pushes with.

Each session gets a token of its own for Legato's API that can only read. It stops working when the session ends.
