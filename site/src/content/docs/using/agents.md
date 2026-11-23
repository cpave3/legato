---
title: Agents
description: Start AI coding agents on tasks, watch and talk to them, reload or stop them, and let them coordinate with each other.
sidebar:
  order: 20
---

An agent is an AI coding tool, such as Claude Code, Codex or Chimera, that Legato starts in its own tmux session. You start one on a task, and its state shows on the task's card. On the web board, a card whose agent waits for you says why, such as **Needs you · permission to run a command**, with a way to answer. You can watch every agent at once, attach to one, send it a message or a macro, reload it, or stop it.

Each tool has its own page under Agent tools: [Claude Code](/legato/agent-tools/claude-code/), [Codex](/legato/agent-tools/codex/) and [Chimera](/legato/agent-tools/chimera/). Agents need tmux.

Legato keeps an agent listed while its launch shell still has child processes,
including a sandboxed agent. A shell name alone does not mean the agent exited.
Messages are never typed into that shell. Once the original shell has no children,
Legato can mark the session ended; a failed process check does not mark it ended.
In a native web conversation, focusing the message field highlights the rounded composer boundary in the current theme. Use `tab` to move to its toolbar buttons; each button has its own focus indicator.

At launch, Legato waits up to three seconds for the new terminal's shell before
it types the agent's launch command. If the shell is not ready, it refuses the
launch and logs the failure instead of sending the command to another program.

## Starting an agent on a task

1. Select the task's card on the board.
2. Press `a`. The spawn dialog opens.
3. Choose the agent tool with `h` and `l`. The default tool comes first, then the others, then `shell`.
4. With engram installed, choose an [engram](/legato/agent-tools/engrams/) on the `engram` row with `h` and `l`, or leave it on `no engram`. Press `r` there to remember the choice for the repository.
5. Tick any [resources](/legato/agent-tools/resources/) to give the agent, with `space`.
6. Choose where the agent starts, then press `enter`.

`tab` and `shift+tab` move between the fields, and `enter` spawns from any of them. A line above the keys says what `enter` will do, such as `→ chimera in ~/Development/autopilot · for f41p6uf0`.

Without tmux, Legato still starts, with agents off: the TUI says `tmux not found: agents are off` in its status bar, `a` and every other way of starting an agent show that reason with the fix, and `legato serve` logs it and keeps serving the board. Install tmux and restart Legato.

Before it starts the agent, Legato checks that the agent tool is installed and runs, as [`legato doctor`](/legato/reference/cli/#running-legato) does. When it is missing or broken, the spawn is refused with the reason and the fix, in the status bar or the web spawn dialog, rather than leaving `command not found` in a terminal. The check reuses a result from the last few minutes and never holds a spawn for more than a fraction of a second.

When the task's agent tool has a saved conversation, Legato then asks whether to resume it (`enter` or `r`) or start a new one (`n`).

In the web UI, press **Open agent** in the task's detail, or press `s` with the card selected on a wide screen. If the task's agent already runs, this takes you to it.

To open the terminal of a task's running agent from the board, press `t`.

### Where the agent starts

The list under **where** starts with the directory the agent would use without a choice: the task's worktree or the directory you used last for it, else the directory Legato runs in. Then come your directory presets, then `custom path…`.

- Move with `j`/`k` or the arrow keys, or press a preset's number.
- Press `/`, or `enter` on `custom path…`, to type a path.
- While you type, Legato lists the directories that complete the path: presets and Git repositories first. `tab` completes to the highlighted one, `ctrl+w` goes up a directory, and `esc` goes back to the list.
- Press `ctrl+s` to save the typed directory as a preset.

Presets are the `repositories` in your configuration that have a `primary_dir`, then the directories you saved in the TUI or the web UI. You cannot remove a saved preset yet.

### Without the sandbox

When `agents.sandbox.enabled` is on, every agent runs inside Legato's [sandbox](/legato/security/sandbox/). To start one agent outside it, press `ctrl+u` in the spawn dialog. The web dialog has a **Run without sandbox** box, which is unticked each time it opens.

An agent started as an [engram](/legato/agent-tools/engrams/) shows it beside its tool, such as `claude · hestia`, in the agents list, over its terminal, on its ensemble tile, in the board's panel about the task, and in the tooltip of its card's strip in the web UI (`Claude Code · hestia`).

With [Docker in the sandbox](/legato/security/sandbox/#docker-in-the-sandbox) on, `ctrl+n` in the spawn dialog (or **Allow the host network for its containers** on the web) lets that agent's containers use the host's network. This weakens the agent's isolation; see [The host network for one agent](/legato/security/sandbox/#the-host-network-for-one-agent) for how. Such an agent is marked with `⇄` beside its ID (a sitemap glyph with Nerd Fonts) and **host network** over its terminal. In the web UI, **Reload with host network** and **Reload without host network** change it for a running agent.

An agent that runs outside the sandbox is marked in amber: with `⊘` beside its ID in the agents list (an open padlock with Nerd Fonts), and with **unsandboxed** over its terminal and on its tile in the ensemble. When the sandbox is off, no agent is marked.

### The web spawn dialog

The web UI's dialog asks the same questions and adds these:

- **First message**: text the agent gets as its first message once it is ready. `Ctrl+Enter` spawns from any field.
- **Engram**: with the engram command installed, start the agent as one of your [engrams](/legato/agent-tools/engrams/), or as **No engram** for a normal session. The engram's summary is shown under it.
- **Clone**: with `repository_cache.enabled`, a **Folder | Clone** switch lets the agent start in a fresh clone of a repository on a branch of its own, instead of a folder. The agent appears under **Starting** while Legato copies and sets up the clone.

## Agents with no task

An ephemeral agent belongs to no card. Press `s` in the agents view, or **+ Spawn agent** in the web UI, and give it a title. Without one it is called "Ephemeral session".

## The Reeve in the list

The [Reeve](/legato/using/reeve/), the agent of your Legato itself, is pinned first in the web UI's Agents list, above the groups, with a violet edge. It shows this instance's name and how many of its changes wait for your approval. When no session runs it stays in the list, quieter; picking it starts one, or resumes the one that was suspended. On a phone it leads the agent picker. Its conversation's status bar says it runs in the sandbox, and on which instance.

In the TUI the Reeve is on the first row of the agents view, above the groups, with a violet wash. Press `R` in any view to open it: Legato goes to its session, resuming or starting one. In the agents view `R` still reloads the selected agent; there, `a` asks the Reeve about the selected agent instead, with a brief of what it is and what it is doing.

## Stages

Each agent has a **stage**: where its work stands, apart from what it is doing right now.

| Stage | Means |
|---|---|
| Exploring | Reading, searching, answering. Nothing changed yet. A new conversation starts here. |
| Planning | A plan waits on you. |
| Implementing | Changing the code. |
| Reviewing | The work is up for review. A reviewer agent is always here. |
| Finished | The work is done. |

Legato works the stage out for itself. An edit in the agent's working folder or a push makes it Implementing. A plan submitted makes it Planning, and approved makes it Implementing. A pull request opened, a review tour marked ready or a review pass started makes it Reviewing. A pull request that was already open when the agent started does not count, so an agent started on a branch with an open pull request stays Exploring until it pushes to that branch. An agent with no card links no pull requests, so it reports Reviewing itself. Review feedback delivered to it makes it Implementing again, and every pull request merged makes it Finished. Agents also report their own stage, and a done note means Finished. You can set it yourself from the agent's **⋯** menu › **Set stage**, which shows "set by you" until something later moves it. The [Reeve](/legato/using/reeve/) has no stage.

In the web UI's Agents list, agents are grouped so you see at a glance which need you:

- **Needs you**: waiting for your answer, stopped on an error, with something it posted for you, or with a plan waiting for your approval. The row says which.
- **Your turn**, under it and quieter: idle, with its work not finished. It stopped mid-work and waits for your reply.
- **Working**: working, or waiting on work it started.
- **Finished**: its stage is Finished. A done note it left marks it unread, with a blue edge.

Each row has a stage line: the stage's **track**, five small bars lit up to the stage (one for Exploring, up to all five, in blue, for Finished), with the stage's name and why. Blue is kept for finished work: an agent working in the background is green, as a working one is. Under **Finished**, a row gives only why it finished, such as `PR #212 merged · a note for you`, since its group already says Finished. **Stage ▾** in the header filters the list by stage: it lists every stage with how many agents are at it, and while a filter is on it shows that stage, with ✕ to clear it, and the header says `Agents 1 of 3`. The Reeve is left out of every count. Your choice is kept in this browser and in the address (`?stage=reviewing`), so a link opens the same filter. While a filter is on, a notice says how many agents need you outside it, so a filter never hides urgent work. The chat header shows the stage too; click it for the agent's stage history. On a phone, the agent picker at the top reads the agent's group, stage and time, such as `Needs you · Implementing · 42m`, with the stage's track before its name, and its list has the same groups, with the same **Stage** menu beside the Reeve at its top.

## The agents view

Press `A` on the board to open the agents view. Agents are listed in the same groups as the web UI (see [Stages](#stages)), the ones that need you first:

- **Needs you**: waiting for you, stopped on an error, with something it [posted for you](/legato/using/attention/#operator-items) still open (a done note aside), or with a plan waiting for you, even when it is idle.
- **Your turn**: idle with its work not finished: it waits for your reply.
- **Interrupted**: a crash stopped it (see below).
- **Working**: working, or in the background: the turn ended, but something it started still runs and will wake it (a shell, a Monitor, a subagent or a scheduled prompt). The line over the terminal says what: `background · 2 jobs`, or the one job by name. When the agent's subagents run subagents of their own, every level is counted: `background · 3 sub-agents working`. In the web UI, click the `◌ 3 sub-agents working` line on the agent's row to see each one under the one that started it.
- **Finished**: its stage is Finished.
- **Ended**: the session is over.

Each entry has a line with its stage, such as `◑ implementing · 6 files changed` (○ exploring, ◔ planning, ◑ implementing, ◕ reviewing, ● finished). Press `f` to filter the list by stage, and again for the next stage; the list then says how many agents need you outside the filter. In the ensemble, each tile names its stage, and the panel of what changed lists stage changes too, such as `LEG-405 finished · PR #212 merged`.

Chimera sessions using Legato's mod report their state directly: permission prompts and questions show **Needs you** until the last open request is answered. A normal turn ending with background shells or background subagents still running shows **Background**, with those jobs listed. Errors show **Needs you**; an interrupted, timed-out or otherwise cancelled turn shows **Idle**, even if jobs remain. Old hook scripts and terminal prompt detection cannot override a session after its first mod report. These mappings follow Chimera's event spec; recorded fixtures are not available yet.

Inside one state, a swarm's agents stay together with the conductor first, then grouped agents, then the rest. Each entry shows the agent's ID and the time in its state, its tool and task, and a bar of its recent activity.

The selected agent's terminal is beside the list. When another agent needs you, a line above the foot names it; press `w` to select it.

### The ensemble

Press `e` to see every agent at once as tiles, and `e` again to go back to the list. In the ensemble, `h` and `l` move between tiles.

A tile shows the agent's state, its task, and what it is doing. For a native agent (Claude Code, Codex or Chimera through Legato's bridge), that is the end of its conversation: what you sent, what it said, its tool calls, and any request it has open. A line the [Reeve](/legato/using/reeve/) said out loud shows as `◈ said "…"`. For an agent Legato drives through its terminal, it is the last lines of the terminal. The foot of a tile shows how full the agent's context window is, such as `ctx ▰▰▰▱▱ 60%`, in amber from 70% and red from 85%.

From 120 columns of width, a panel lists what changed among the agents, newest first.

The web UI has the same ensemble at **Ensemble** in the navigation, or `e` on the Agents page. Each web tile has a message box and **Open**, and names the agent's stage beside its state. Its header counts the agents that need you, whose turn it is, that are working and that are finished. **By stage** lays the agents out in a band for each stage, Exploring to Finished, so you see the whole pipeline at once: each band is as tall as its agents, and stages with no agents shrink to one line that names them, such as `Exploring · Planning · none`. A card in a band does not repeat its stage. It leads with what the agent needs from you and for how long, then its ID, title, why it is at its stage and its tool, then one thing to read: what it asks you, with **Allow** and **Deny** or **Review plan**, what it is doing now, or the first four lines of its last reply. **Reply** opens a message box on the card. Finished agents are one line each, with a blue dot when they left you a note. On a phone the bands stack and empty stages are left out. Only agents that need you, or whose turn it is, get a card; a working agent is one line with what it is doing. **Finished** starts collapsed, showing how many new notes it holds, and opens on a tap; the phone remembers whether you left it open. Tap a band's header to close or open it; a band opens by itself when one of its agents comes to need you. The web UI remembers the layout you chose on each device.

## When an agent exits

Legato checks that the agent tool is in the foreground before sending terminal
messages, macros or keys. If the pane is back at a shell, it sends nothing and
reports the failure; messages are never run as shell commands. The agent is
marked ended once exit is confirmed. Starting tools have a short grace period,
but receive no keys until verified. A shell-only session is not a message target.

Native agents use their native connection only: a failed connection does not
fall back to typing. Launch messages remain pending until accepted. Native TUI
macros must submit text (end in a newline); raw key macros are refused. To work
in the shell yourself, attach to the terminal deliberately.

## Attaching to an agent

Press `enter` (or `tab`) in the agents view to attach your terminal to the agent's tmux session. Press the escape key to detach and come back to Legato. The key is `ctrl+]` unless you set another:

```yaml
agents:
  escape_key: "ctrl+]"
```

In the web UI, open an agent from the Agents page. A native agent shows as a conversation; another agent shows its terminal. See [The web UI](/legato/using/web-ui/).

## A task with more than one agent

A task has one agent of its own, and Legato may run another beside it: a reviewer that checks a handoff's pull requests. The agents view lists each as its own row, the reviewer as "Task title · reviewer". Attaching, messages and stopping act on that row's agent only. You cannot start an agent in the reviewer's place yourself, and Legato refuses to reload it, since it starts the reviewer with the settings its review needs.

## Reloading an agent

Reloading stops an agent and starts it again with the same tool, directory, resources and sandbox choice, and resumes its conversation. The agent then reads its MCP servers, plugins and hooks again.

- In the TUI, press `R` in the agents view.
- In the web UI, choose **Reload agent** in the agent's **More actions** menu.

Legato tells the reloaded agent that its session was reloaded. The web UI shows that as a message from Legato.

Legato refuses to reload an agent that a swarm or a workflow runs, and one that has not reported its conversation yet. The agent keeps running.

Press `U` to reload an agent on the other side of the sandbox: out of it when it runs inside, and back in when it runs outside. Use it when the sandbox blocks something the agent needs.

## After a crash: interrupted agents

When the machine goes down, or tmux is killed, the agents' tmux sessions go with it. When Legato starts again, it keeps each agent that was running and whose session is gone as **Interrupted**, so you can bring it back. Stopping and starting Legato alone, as `legato restart` does, leaves agents running, so they are not marked.

Interrupted agents are listed in their own group: **Interrupted** at the top of the web UI's Agents list, and **INTERRUPTED** in the TUI's agents view. Legato does not resume them by itself. An Attention item says how many there are, with **Resume all**.

- **Resume** starts the agent again as it was: the same tool, folder, resources, sandbox choice and [engram](/legato/agent-tools/engrams/). Its conversation resumes, and its first message tells it that it was interrupted, since its last step may not have finished. The web UI shows that message as one from Legato. In the TUI, select it and press `R`.
- **Resume all** resumes every interrupted agent that can be resumed. In the TUI, press `A`.
- **Dismiss** marks it ended. Its conversation stays in the [session history](#session-history), where you can restore it later. In the TUI, press `X`.

Some agents cannot be resumed, and the list says why:

- An agent that never reported its conversation has nothing to resume. **Start new** (`R` in the TUI) starts a new agent on its task, set up as it was.
- When its folder, worktree or clone is gone, Legato does not start it.
- An agent that a swarm or a workflow runs is only shown. The swarm or workflow hears that it died, as it does of any agent that ends. A reviewer is ended, and its review pass handles it.

The Reeve's session resumes the same way, with a fresh snapshot of Legato. An interrupted agent on a task [handed off](/legato/using/handoff/) to a worker resumes there, unattended in its clone, and your main shows **Interrupted on worker** in the Away lane.

## Session history

Legato keeps every conversation its agents have had on this Legato in its **Session history**, with each time it ran. It keeps one after its agent is stopped or dismissed, after `/clear` starts another, and after its task is deleted. You can bring back any of them, not only an interrupted one. A dismissed agent stays in the history.

An entry keeps the conversation's first prompt and last message, so you can search for what an agent did. Legato keeps an entry for 180 days after it was last used; set [`agents.history.keep`](/legato/reference/configuration/#agents) to change that.

**Restore** starts the conversation again in its task, as it last ran: the same tool, folder, sandbox choice, [engram](/legato/agent-tools/engrams/) and resources (any removed since are left out). Its first message tells it that it was restored and that files, branches and other agents may have changed since. The web UI shows that message as one from Legato. Restoring the task's interrupted agent resumes it; restoring another conversation of that task dismisses the interrupted agent first. A Reeve conversation is resumed as the Reeve's.

**Restore into a new task** starts it in a new task instead: a card titled as the old one, in its workspace and the board's first column, or an agent with no task when the original had none. Use it when the task's own agent is busy, or when the task is deleted or archived. Its earlier runs stay listed with it.

When a conversation cannot be restored, because its folder or its transcript is gone, **Start fresh** starts a new conversation in its place, in a folder you pick (its old one when that is still there). The new agent's first message hands over what Legato knows of the old one: the task, folder, repository and branch, when it last ran and how it ended, its first prompt and its last message, and where its transcript is. Legato never makes a folder again or moves a transcript. For a Chimera conversation, **Start fresh** is offered beside **Restore**, because Legato cannot tell beforehand whether Chimera still has it.

Legato does not restore a conversation that is running, or one whose folder or tool is gone. It also does not restore into a task whose agent is running, or into a task that is deleted, archived or [away on a worker](/legato/using/handoff/): restore into a new task instead. In each case it says why.

In the web UI, open **Session history** at the foot of the agents list (on a phone, under the agent picker). Search by title, folder or what was said, and filter by tool, state or repository. Select a session to see its first prompt, its last message, the newest messages of its transcript and each time it ran, with **Restore** and **Forget**. Legato reads Claude Code's and Codex's transcripts where those tools keep them; when one is gone, the session cannot be restored. **Forget** removes a session from the history only: the tool's own transcript stays, and so does the task's **Resume**. A task's detail panel lists its **Earlier sessions**, each one a link into the history. A session marked "found by folder and time" is one Legato matched to an agent's run by its folder and when it started, because that agent never reported it. Legato looks for these when it starts and after each agent ends, among Claude Code's and Codex's transcripts, and only for runs Legato itself started: a conversation you started outside Legato is never listed. When two agents worked in one folder at once, each run lists both conversations it may have had.

In the TUI, press `H` in the agents view for the history: `/` searches, `enter` restores the selected conversation into its task and `n` into a new task; the selected one's first prompt and last message show under the list.

The Reeve can search the history and restore from it too. A restore or a Forget is a change, so the Reeve asks you first.

## Stopping an agent

Press `X` in the agents view, or choose **Kill agent** in the web UI's **More actions** menu. Stopping a swarm's conductor from the web UI cancels the whole swarm; see [Swarms](/legato/using/swarms/).

## Macros

A macro is a saved message for an agent, such as "run the tests and fix what fails". Run on a task, it starts the task's agent with the macro as its first message, or becomes the running agent's next message. An agent a macro starts resumes the task's saved conversation in its own folder when there is one. Otherwise it starts in the task's worktree, else in the task's job clone of the pull request's repository (the task's own repository for a task macro), else in the folder the task last worked in, and only with none of these in Legato's default folder.

Manage them on the **Macros** page of the web UI (in the side rail after **Flows**; on a phone, under **More**). Four are built in: **Plan** and **Implement** for a task, **Review** and **Address feedback** for a pull request. Edit one to make it yours, for example to point Review at your favourite review skill; **Reset** puts it back as shipped. A built-in can't be deleted or renamed. Your own macros can be renamed and deleted; a workflow that names a macro by its old name fails until you change it. A macro set in `config.yaml` is locked on the page.

Each macro has:

| Field | Required | What it does | Default |
| --- | --- | --- | --- |
| **Name** | Required | What menus, `legato macro run` and a workflow's `with.macro` call it. | none |
| **Message** | Required | The text the agent gets, as written. Run on a task or a pull request, it comes below the task's key and title (and the pull request's link). | none |
| **Offer it on** | Optional | Which menus list it. Tasks: a card's menu and the spawn dialog. Pull requests: a card's menu, under each pull request, and the button of a pull request that needs you. The Agents page and the TUI picker list every macro. | Tasks and pull requests |
| **Agent to start** | Optional | The agent (Claude Code, Codex or Chimera) started when the task has no agent running. Ignored when one is running. | Your default agent |
| **Model tier** | Optional | One of that agent's [tiers](/legato/reference/configuration/#adapters), usually choosing its model. Used only when the macro starts the agent. Offered only for an agent that has tiers. | None |

In `config.yaml`, the same fields are `name`, `message` (or `keys`, its older name), `applies_to` (`task` or `pr`), `agent` and `tier`:

```yaml
macros:
  - name: run tests
    message: Run the tests and fix what fails.
  - name: Deep review
    message: Review this pull request in depth.
    applies_to: pr
    agent: claude-code
    tier: large
```

Legato sends the message exactly as written: it does not expand environment variables. A macro is sent as a message, as if you typed it, so it needs no final new line. A change applies the next time you use the macro, with no restart. Legato never adds its own tag to a macro: it reaches the agent as your words.

Where to run one:

- On the board: a card's menu (the task's macros, then each open pull request's), and the button on a pull request that needs you. See [Board](/legato/using/board/).
- In the spawn dialog, under **Start with macro**: it fills the first message, and the agent and model tier when the macro sets them.
- On the Agents page, press `M`, then `1` to `9` for the first nine macros. Click a later macro to send it. The keys do nothing while you type in a field.
- In the TUI, press `m` in the agents view, or `c` on a card and choose **Macros ›**.
- From a script: `legato macro run Plan <task-id>`.
- In a workflow step, with `with.macro`. See [Workflows](/legato/using/workflows/).

## Guidance for a repository

To have agents open a repository's pull requests as drafts or ready for review, use a [`pull_requests` rule](/legato/reference/configuration/#pull-requests) (Settings › Repositories › Pull requests) rather than guidance: the agent is told in its launch prompt, and with **As drafts** Legato also refuses any pull request that is not a draft.

You can give every agent that works in a repository the same instructions, such as "run the prove-it skill when the work is done". Legato adds them to the agent's launch prompt. It finds the repository from the origin remote of the folder the agent starts in, so the guidance applies in every checkout and worktree of that repository, wherever it is on disk.

Add guidance in the web UI under **Settings › Repositories › Repository guidance**, or in `config.yaml`:

```yaml
agents:
  guidance:
    - repo: acme/web-app
      text: |
        When a piece of work is done, run the prove-it skill
        and show its result before you say the work is finished.
```

`repo` takes `owner/name` (a GitHub repository), `host/owner/name` for another host, or a Git remote address. Guidance from `config.yaml` is shown in Settings but can only be changed in the file. Agents started after you save guidance get it. An agent that is already running does not, and neither does a conversation that is resumed.

## Agents that work together

Agents started anywhere in the same Git worktree are peers. A peer can list the others and message them, so two agents can agree who edits which file. Separate worktrees stay apart.

When an agent starts, Legato tells it about its peers, and tells the peers it joined. To turn these notices off:

```yaml
agents:
  peer_awareness: false
```

Agents always have the tools to talk to peers: `list_peers`, `message_peer` and `broadcast_peers` among Legato's [MCP tools](/legato/agent-tools/mcp-tools/), and the same as commands:

```bash
legato agent peers
legato agent message <task-id> "Done with the parser; you can edit it now."
legato agent broadcast "Working on internal/service/agent.go."
```

You can run `legato agent message` yourself to message any running agent. Peer messages are hints, not locks: nothing stops an agent from editing a file another one works on. A [swarm](/legato/using/swarms/) is the way to split work with owned files.

## Other keys in the agents view

| Key | Action |
| --- | --- |
| `g` | Put the agent's task in a group. The web UI lists agents by these groups; see [Groups](/legato/using/web-ui/#groups) |
| `n` | Turn notifications for the agent on or off, with [notifications](/legato/integrations/notifications/) set up |
| `v` | Dictate a message, with voice input on |
| `o` | Open the cards and artifacts the agent showed in the web UI |
| `M` | Swarm actions: message a worker, close it, or finish the swarm |

See [Keyboard shortcuts](/legato/reference/keyboard/) for the rest.

## Token usage

With [Manticore](/legato/integrations/manticore/) set up, the agents view shows how many tokens were used today, with what went in and what came out. The ensemble draws the tokens of each of the last 24 hours. The numbers cover everything your Manticore instance records, not only this Legato.
