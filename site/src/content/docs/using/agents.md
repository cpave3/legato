---
title: Agents
description: Start AI coding agents on tasks, watch and talk to them, reload or stop them, and let them coordinate with each other.
sidebar:
  order: 20
---

An agent is an AI coding tool, such as Claude Code, Codex or Chimera, that Legato starts in its own tmux session. You start one on a task, and its state shows on the task's card. You can watch every agent at once, attach to one, send it a message or a macro, reload it, or stop it.

Each tool has its own page under Agent tools: [Claude Code](/legato/agent-tools/claude-code/), [Codex](/legato/agent-tools/codex/) and [Chimera](/legato/agent-tools/chimera/). Agents need tmux.

## Starting an agent on a task

1. Select the task's card on the board.
2. Press `a`. The spawn dialog opens.
3. Choose the agent tool with `h` and `l`. The default tool comes first, then the others, then `shell`.
4. With engram installed, choose an [engram](/legato/agent-tools/engrams/) on the `engram` row with `h` and `l`, or leave it on `no engram`. Press `r` there to remember the choice for the repository.
5. Tick any [resources](/legato/agent-tools/resources/) to give the agent, with `space`.
6. Choose where the agent starts, then press `enter`.

`tab` and `shift+tab` move between the fields, and `enter` spawns from any of them. A line above the keys says what `enter` will do, such as `→ chimera in ~/Development/autopilot · for f41p6uf0`.

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

An agent started as an [engram](/legato/agent-tools/engrams/) shows it beside its tool, such as `claude · hestia`, in the agents list, over its terminal, on its ensemble tile, in the board's panel about the task, and on its card in the web UI (`Running · Claude Code · hestia`).

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

## The agents view

Press `A` on the board to open the agents view. Agents are listed under what they are doing, the ones that need you first:

- **Needs you**: waiting for you, or stopped on an error.
- **Running**: working.
- **Background**: the turn ended, but a shell or a subagent it started still runs.
- **Idle**: alive and not working.
- **Ended**: the session is over.

Inside one state, a swarm's agents stay together with the conductor first, then grouped agents, then the rest. Each entry shows the agent's ID and the time in its state, its tool and task, and a bar of its recent activity.

The selected agent's terminal is beside the list. When another agent needs you, a line above the foot names it; press `w` to select it.

### The ensemble

Press `e` to see every agent at once as tiles, and `e` again to go back to the list. In the ensemble, `h` and `l` move between tiles.

A tile shows the agent's state, its task, and what it is doing. For a native agent (Claude Code, Codex or Chimera through Legato's bridge), that is the end of its conversation: what you sent, what it said, its tool calls, and any request it has open. A line the [Reeve](/legato/using/reeve/) said out loud shows as `◈ said "…"`. For an agent Legato drives through its terminal, it is the last lines of the terminal. The foot of a tile shows how full the agent's context window is, such as `ctx ▰▰▰▱▱ 60%`, in amber from 70% and red from 85%.

From 120 columns of width, a panel lists what changed among the agents, newest first.

The web UI has the same ensemble at **Ensemble** in the navigation, or `e` on the Agents page. Each web tile has a message box and **Open**.

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

Legato refuses to reload an agent that a swarm or a workflow runs, and one that has not reported its conversation yet. The agent keeps running.

Press `U` to reload an agent on the other side of the sandbox: out of it when it runs inside, and back in when it runs outside. Use it when the sandbox blocks something the agent needs.

## After a crash: interrupted agents

When the machine goes down, or tmux is killed, the agents' tmux sessions go with it. When Legato starts again, it keeps each agent that was running and whose session is gone as **Interrupted**, so you can bring it back. Stopping and starting Legato alone, as `legato restart` does, leaves agents running, so they are not marked.

Interrupted agents are listed in their own group: **Interrupted** at the top of the web UI's Agents list, and **INTERRUPTED** in the TUI's agents view. Legato does not resume them by itself. An Attention item says how many there are, with **Resume all**.

- **Resume** starts the agent again as it was: the same tool, folder, resources, sandbox choice and [engram](/legato/agent-tools/engrams/). Its conversation resumes, and its first message tells it that it was interrupted, since its last step may not have finished. In the TUI, select it and press `R`.
- **Resume all** resumes every interrupted agent that can be resumed. In the TUI, press `A`.
- **Dismiss** marks it ended. In the TUI, press `X`.

Some agents cannot be resumed, and the list says why:

- An agent that never reported its conversation has nothing to resume. **Start new** (`R` in the TUI) starts a new agent on its task, set up as it was.
- When its folder, worktree or clone is gone, Legato does not start it.
- An agent that a swarm or a workflow runs is only shown. The swarm or workflow hears that it died, as it does of any agent that ends. A reviewer is ended, and its review pass handles it.

The Reeve's session resumes the same way, with a fresh snapshot of Legato. An interrupted agent on a task [handed off](/legato/using/handoff/) to a worker resumes there, unattended in its clone, and your main shows **Interrupted on worker** in the Away lane.

## Stopping an agent

Press `X` in the agents view, or choose **Kill agent** in the web UI's **More actions** menu. Stopping a swarm's conductor from the web UI cancels the whole swarm; see [Swarms](/legato/using/swarms/).

## Macros

A macro is a named text you send to an agent with a key or two, such as "run the tests". Define them in the configuration:

```yaml
macros:
  - name: "run tests"
    keys: "task test\n"
  - name: "commit changes"
    keys: "Please commit all current changes with a descriptive message.\n"
```

Legato sends `keys` exactly as written. It does not expand environment variables. In the TUI, a trailing `\n` presses Enter.

- In the TUI, press `m` in the agents view and pick a macro.
- In the web UI, press `M` on the Agents page, then `1` to `9` for the first nine macros. Click a later macro to send it. The keys do nothing while you type in a field.

## Guidance for a repository

You can give every agent that works in a repository the same instructions, such as "run the prove-it skill when the work is done". Legato adds them to the agent's launch prompt. It finds the repository from the origin remote of the folder the agent starts in, so the guidance applies in every checkout and worktree of that repository, wherever it is on disk.

Add guidance in the web UI under **Settings › Agents › Repository guidance**, or in `config.yaml`:

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
| `g` | Put the agent's task in a group |
| `n` | Turn notifications for the agent on or off, with [notifications](/legato/integrations/notifications/) set up |
| `v` | Dictate a message, with voice input on |
| `o` | Open the cards and artifacts the agent showed in the web UI |
| `M` | Swarm actions: message a worker, close it, or finish the swarm |

See [Keyboard shortcuts](/legato/reference/keyboard/) for the rest.

## Token usage

With [Manticore](/legato/integrations/manticore/) set up, the agents view shows how many tokens were used today, with what went in and what came out. The ensemble draws the tokens of each of the last 24 hours. The numbers cover everything your Manticore instance records, not only this Legato.
