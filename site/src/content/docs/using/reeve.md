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

## What it knows

A session starts with what it needs to know about this instance: a short snapshot (version and updates, how Legato runs, its workers, open Attention items, running agents, recent errors in the log) and how it is meant to work. "Ask the Reeve" adds a brief about what you asked about. A session you resume is told what changed since it was last here.

While it runs, it has tools of its own:

- **docs**: Legato's user manual, these pages, built into Legato.
- **instance_status**: a fresh snapshot of the instance.
- **read_logs**: the recent lines of Legato's [log file](/legato/running/agent-server/#the-log-file).
- **legato_api**: any of Legato's API calls; a change becomes a change set for you to approve.
- **legato**: a `legato` command on the machine, for what only the command line does (integration tokens, connecting Argus, `columns sync`, `hooks install`). A command that only reads runs at once; one that changes something is a change set. Some commands it never runs, such as those that hand out secrets or reset your devices.

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

These levels and rules, and the seed of its engram, are the Reeve's own rules: they change only with a signed approval, even from Settings, and not at all while no device is enrolled.

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

To switch to a local voice such as Piper, set the voice command to a Piper script and clear the voice credential.

### Talking to the Reeve

You can also talk to the Reeve and hear its answers, from the desktop and Android apps. This needs, on the main instance:

- [Voice dictation](/legato/reference/configuration/) on (`voice.enabled`) with a Whisper server (`voice.whisper_url`). It can be the Whisper that Home Assistant Assist already uses. Both settings take a restart.
- The Reeve's voice, set up as above.

What you say reaches the Reeve marked as spoken, and it answers out loud, in a few short sentences, and writes only what cannot be said, such as a path or a link. A message you type gets a written answer, as before: voice lasts for one message. If the Reeve answers a spoken message only in writing, the app says so.

Press the key or the button and speak. When you stop for about a second (**Pause that ends a spoken turn**, `reeve.voice.pause`), what you said is sent to the Reeve, and the words appear as you speak (**Words as you speak**, `reeve.voice.partials`). If you go on speaking within two seconds, before the Reeve answers, that is sent too. While the Reeve works, the app shows what it is doing. After it has answered out loud, the app listens for your reply for six seconds (**Listen for a reply for**, `reeve.voice.follow_up`); press the key again, or Cancel, to stop. Only one device talks to the Reeve at a time: starting on another takes the conversation over.

The Reeve's answer plays on the device you are talking to, not on the main's speakers, with its words shown as it plays. The microphone is off while it plays. **Stop** drops the rest of the answer; while the Reeve is still working, it interrupts it. The main keeps each line's audio only until your device has played it, ten minutes at most, and the voice key never leaves the main.

**Quiet hours** (`reeve.voice.quiet_hours`, as `22:00-07:00` in the main's local time): no line plays on the main's speakers then, and the Reeve replies in writing. A reply to something you said to it still plays on your device.

In the web UI, the ring beside the Reeve in the Agents list opens a voice sheet. It needs the page over HTTPS (a browser gives a page served over plain HTTP no microphone), or the Legato app. On a worker's page the sheet says the voice is on the main; in the Android app, **Open the main** goes there.

Before it listens, an app asks the main whether a conversation will work. If not, it shows one line saying why, such as "Whisper isn't answering on tessitura", and which setting to change. When the Reeve is suspended, that question also starts resuming it, so it is ready by the time you finish speaking.

## Where it runs

The Reeve always runs in Legato's [sandbox](/legato/security/sandbox/): it cannot be started without it, and **Run without sandbox** and the switch out of the sandbox are refused for it. It works in a folder of its own, `~/.local/state/legato/reeve` (or under `$XDG_STATE_HOME`), outside your repositories and Legato's data folder. The folder also holds its engram's store.

Each session gets a token of its own for Legato's API that can only read. It stops working when the session ends.
