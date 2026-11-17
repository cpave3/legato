---
title: The web UI
description: A tour of Legato's web UI, from the board and the agents to Attention, plans, reviews, reports and settings.
sidebar:
  order: 30
---

The web UI shows the same board and agents as the TUI, in a browser. It adds a conversation view for agents, a side panel for what they show you, and pages for plans, reviews, workflows and reports. Use it on a second screen, on a laptop away from your desk, or on your phone.

To start the web server, see [Web server](/legato/running/web-server/). To run it with the TUI, set `web.enabled: true`. To reach it from a phone or another computer, see [Devices](/legato/running/devices/). You can also install the web UI as an app (a PWA) from the browser.

## Getting around

On a wide screen, a rail on the left holds every page. A count beside a page says how many things there wait on you, or how many agents are at work. At the foot are **Settings**, the server switcher when you use more than one Legato, and the connection state (`sync`, or a red `offline`).

On a phone, a tab bar along the bottom holds **Attention**, **Board**, **Agents**, **Review** and **More**. **More** opens the other pages, the server in use, and a few settings.

| Page | What it is for |
| --- | --- |
| **Attention** | Everything that needs you, in one list. See [Attention and approvals](/legato/using/attention/). |
| **Agents** | The agent list and one agent's conversation or terminal. |
| **Ensemble** | Every running agent at once, as tiles. |
| **Board** | The kanban board. See [Board and tasks](/legato/using/board/). |
| **Review** | Review tours of what agents changed. See [Reviews](/legato/using/reviews/). |
| **Plans** | Plans agents proposed, waiting for your decision. See [Plans](/legato/using/plans/). |
| **Flows** | Workflow runs and the workflows you can run. See [Workflows](/legato/using/workflows/#runs-and-workflows-in-the-web-ui). |
| **Learning** | The Legato Learn ledger. See [Legato Learn](/legato/using/learn/). |
| **Reports** | Where agent time went. See [Reports](/legato/using/reports/). |
| **Remote** | Clones Legato made for agents. |
| **Audit** | The credential audit log. See [The credential broker](/legato/security/credential-broker/). |
| **Settings** | Repositories, resources, security, integrations, handoff, connection, appearance and updates. |

When the connection drops, the UI first says "Reconnecting…" and keeps the page as it is. After 10 seconds without a connection, a **Connection Lost** dialog offers **Retry** and a way to switch servers.

Dialogs keep keyboard focus inside their controls and return focus to the control that opened them. Escape closes the top dialog or picker first. A dialog cannot close while an operation that requires waiting is in progress.

## Board

The board works as it does in the TUI, with the same keys: `h`/`j`/`k`/`l` to move, `n` to create, `m` to move, `/` to search, `?` for the key list. You can also drag a card to another column, or within its column to reorder it; it lands where you let it go. The phone layout has no drag: **Move** in a card's menu moves it to another column, at the end, and a column cannot be reordered there. Press `enter` or double-click a card to open its detail. From 1280 pixels of width, a panel beside the board describes the selected card, with buttons for its agent, its details and its ticket or pull request.

**Import** in the header searches your ticket providers and imports a ticket, as `i` does; see [Board](/legato/using/board/#local-tasks-and-remote-tickets). **Sync providers** in the header syncs Jira and GitHub now, and pulls every [worker](/legato/using/handoff/#staying-up-to-date) this Legato hands tasks to.

In `/` search, press `Enter` as soon as a result appears to open it, including an archived task.

Once this Legato is paired with a [worker instance](/legato/running/instances/), a card's menu and its panel offer **Hand off…**, a card dragged onto the **Away** lane opens the same form, and the lane, after the columns, lists the tasks away on each worker. On a phone they are in an **Away** section above the columns. See [Hand off tasks to a worker](/legato/using/handoff/).

A card's detail has the task's notes, reviews, plans and workflow runs in tabs. When the task's agents built a stack of branches with Staccato, a **Stacks** tab shows it, with **Download stack** once every branch is pushed; see [Staccato](/legato/integrations/staccato/#taking-a-stack-home).

On a phone, the board shows one column at a time. Swipe sideways, or tap a column's chip, to change column. A new card starts in the column you are looking at. Press and hold a card for its actions.

## Agents

The **Agents** page lists your running agents on the left, by state, as the TUI does. Open one to see it on the right.

### Groups

Once any agent is in a group, the list shows agents by group: the groups in `groups.defaults`, then the other groups in use by name, then **Ungrouped**. Inside a group, agents keep the order by state, so one that needs you comes first. The Reeve stays pinned above the groups. A group belongs to the agent's task, so the TUI and the web UI show the same groups, and a change in either shows in the other.

- **Move an agent:** drag it onto a group, or onto **Ungrouped** to take it out of its group. Or pick **Move to group…** in its right-click menu or the **More actions** menu in its header, which also has **New group…**. On a phone, use the header's menu.
- **Collapse a group:** click its header. A collapsed group shows how many agents it holds and, in amber, how many of them need you. Your browser remembers which groups are collapsed.
- **Manage groups:** **New group** under the list makes an empty group to drag agents into; your browser keeps it until you delete it. A group's `⋯` menu has **Rename group**, **Delete group** (its agents become ungrouped) and **Collapse all others**. A group in `groups.defaults` keeps its header after you delete or rename it; take it out of that setting to remove it.
- **List by state:** the button beside **Ensemble** switches between the list by group and the list by state. Your browser remembers the choice.

On a phone, the agent picker lists the same groups, and a tap on a group's header collapses it.

An agent shows in one of two ways:

- **Native panel.** Claude Code, Codex and Chimera, when Legato talks to them through its bridge, show as a conversation: your messages, the agent's replies, its tool calls as compact rows, and its thinking when the tool shares it. What the [Reeve](/legato/using/reeve/) said out loud is a bubble marked **◈ Said**, never folded with the tool calls. Permission requests and questions appear as cards you answer in place. A model menu and a settings dialog change the model and effort for this session; see [Model settings](/legato/agent-tools/model-settings/). You can queue a message while the agent works.

Each message you send is listed above the message box until the conversation shows it: `sending`, then `sent` once the agent has it. The agent reads a sent message at its next step, or as its next prompt when the turn ends first. If a message cannot be delivered, it shows as `not sent` with the reason: **Edit** puts it back in the box to send again. When the page has lost its connection to Legato, a message you send stays in the box and the panel says so.
- **Terminal.** Any other agent, or one you switched with **Downgrade to tmux**, shows its terminal. A switched agent stays on its terminal when Legato restarts. A prompt bar below it sends text and keys. **Upgrade to native panel** switches a terminal agent back when its tool supports it.

When a native agent starts subagents, a **Sub-agents** switch appears in its header beside **Related** and **Artifacts**. **Inline** shows each subagent as a card in the conversation. **Tiles** keeps the conversation in the left half and gives each subagent a tile in the right half: what it is doing, its newest steps, how long it has run and how many tools it called. Running subagents share the space; finished ones move to a list underneath, newest first. The list shows the newest three: **Show more** lists the rest, and its **Finished** heading folds it away. Click a tile, or the subagent's line in the conversation, to open it in full with the prompt it was given; **All tiles** or `Esc` goes back. On a phone or a narrow window, the tiles are a strip under the header: tap one to open it as a sheet, and swipe or use the arrows to move between subagents. The browser remembers your choice. Tiles show Claude Code and Chimera subagents; Codex subagents are not shown yet.

Background shells get tiles in the same place. When an agent starts a command in the background, such as a dev server, a test watcher or a long build, the switch appears too, and in **Tiles** each shell has a tile beside the subagents. The tile shows the command (or its description, when the agent gave one), whether it is running, exited with a code, or was killed, how long it has run, and the last lines it printed. Open a tile to read its output in full; while the shell runs, the output follows what it prints. A shell that exited with any code other than 0 is marked as an error. Finished shells go on the **Finished** list with their exit code. The tile is read-only: you can't type to the shell or stop it from here. Only the newest 32 KB of a shell's output is kept; the tile says when earlier output was dropped. In the conversation, the command's line has a link to its tile.

- **Claude Code**: tiles with output, also when the agent runs in the sandbox. A shell is marked finished when Claude Code tells the agent it ended. If that happens during a long turn, the tile updates when the turn reaches that point.
- **Chimera**: tiles with the command, state, run time and exit code. Their output isn't available yet, and the tile says so. Chimera doesn't let Legato read a shell's output yet.
- **Codex**: no shell tiles.

Cards and tiles agree on whether a child is starting, running, successful, failed or interrupted. Interrupted children go on the **Finished** shelf, separately labelled from errors. With Chimera's Legato mod, background completion status also updates while the parent is idle and survives reopening the panel.

Choose **Commands and skills** in a native agent's composer, or type `/` in its message box to list its commands and skills. As you type, the best matches come first: the exact name, then names that start with what you typed, then names with a word that starts with it (`/review` finds `plugin:review`). Use `↑` and `↓` to move, and `Tab` or `Enter` to complete the highlighted one: it goes into the box and you keep typing, for example the skill's arguments. Nothing is sent until you press `Enter` again. A command that stands for a prompt puts that prompt in the box. `Esc` closes the list; type again to reopen it. The launcher also has All, Skills and Commands filters, search, full descriptions and source/plugin details. Selecting an item keeps existing draft notes and does not send. Loading and catalog failures are shown separately; Retry keeps the draft.

When a native agent's terminal shows a numbered menu (an update notice, a trust question), the panel draws it as a card with a button for each choice.

The **More actions** menu in the agent's header holds **Rename**, **Refresh from snapshot**, notifications, **Reload agent**, **Reload without sandbox** (or **Reload in sandbox**, for an agent you started outside it), **Reload with host network** (or **Reload without host network**), **Disconnect** and **Kill agent**. The sandbox entry is there only while Legato's sandbox is on. The host network entry is there only for a sandboxed agent, with Docker in the sandbox on, and never for the Reeve; see [Sandbox](/legato/security/sandbox/#the-host-network-for-one-agent). Right-click an agent in the sidebar for the same actions. For an agent you have not opened, the actions that act on the open terminal (refreshing it, prompt detection) are left out. **Rename** renames the agent's task, so it works for local tasks; a task from a provider keeps its title. Asking before **Kill agent**, **Reload agent** and similar actions uses a small dialog in the app, not the browser's. See [Agents](/legato/using/agents/) for reloading and stopping.

To switch agents from the keyboard, hold the switch key and press `1` to `9`. The numbers follow the visible list order, ready as soon as the agents appear. The key is Control on Linux and Alt on a Mac unless you change it in **Settings › Agents and workspaces**. Press `M` for the agent's macros. A draft you type for one agent is kept when you switch to another.

### Terminal mode

An agent that runs in tmux opens on the Agents page as a real terminal on its tmux session, set in a frame. Type straight into it (menus, arrow keys, Ctrl keys), scroll with the wheel or a drag to read tmux's own history; it resizes with the window. The strip along the top of the frame names the session. If the connection drops, it says **Reconnecting** and when it will try again, and **Retry now** tries at once. **Refresh terminal** in the agent's menu reconnects it too. Agents with a native panel keep their panel.

When voice is enabled, press `v` outside a text field to start dictation in a terminal session with a message box, as soon as the selected agent appears; `Esc` cancels it.

On a computer there is no message box: the terminal is where you type, and the agent's menu is in the header. A phone keeps the message box, with a row of the keys its keyboard lacks: **Esc**, **⇧Tab**, **↑** and **↓**, and **Ctrl**, which sends the next character you type, in the box or the terminal, with Ctrl. When the agent asks something, such as a yes-or-no menu, its answers appear as buttons above the box.

Use Tab or the arrow keys to focus a terminal prompt choice, then press Enter to select it. **Dismiss** closes the choices without sending terminal keys.

Choose terminal colours in **Settings › Preferences › Appearance › Terminal theme**: **Follow app**, **Legato**, **Night**, **Solarized Dark**, **Classic**, **Dracula**, **Ocular Light**, **Ocular Dark**, **Ocular Light Vivid** or **Ocular Dark Vivid**. The default follows your app theme; an existing saved terminal choice stays as it was. A change updates open terminals without reconnecting or clearing their screens. Programs that emit their own truecolour backgrounds can override the terminal palette.

While a terminal is open in the web UI, the session has mouse mode on and no tmux status line, which a desktop `tmux attach` sees too. Hold Shift to select text with the mouse there.

### Artifacts and cards

An agent can show you things through its [MCP tools](/legato/agent-tools/mcp-tools/):

- A **card** appears in the conversation where the agent made it, in place of text: a chart, a table, a status.
- An **artifact** opens in the **Artifacts** panel beside the conversation (a full-screen sheet on a narrow screen). The panel keeps each artifact as a tab, with its revisions, **Copy source** and **Download**.

HTML an agent wrote runs in a closed frame that cannot reach the network beyond the hosts in `agents.mcp.resource_domains`. A card can offer text for your message box, but it never sends anything for you.

The **Related** switch beside **Artifacts** shows the task's card, its pull request, and its plans, reviews and workflow runs.

### Review briefings on the branch

If the agent's branch carries a review briefing (HTML pages under `.review/` that the branch adds or changes), the **Artifacts** panel shows each page as a tab before the pull request exists. Legato checks for changes every 15 seconds.

### Images

When an agent reads an image file, its row in the conversation opens to show the image. Click the image to see it full size.

To give an agent an image, paste it into the message box, drop an image file on the box, or press **Attach image** (the picture icon under the box) and choose one. On a phone, **Attach image** opens your photos. Legato stores the image in a temporary folder and puts the file's path in your message, where the agent can read it. An image at the very start of the message comes in as `Image: <path>`, because Claude Code, in its conversation view, turns away a message from Legato that starts with `/`. A thumbnail above the box stands for each image. Legato keeps a task's newest 20 images, up to 10 MiB each, in PNG, JPEG, GIF, WebP or BMP.

If the agent's session would not take a message, the message is marked **not sent** with the reason, and the agent no longer shows as working. A message to an agent on its terminal that could not be typed into the terminal shows as an error; one that could not leave the page because it had lost its connection stays in the box.

If you copied text with the image, the text is pasted too. A task that is away on a worker takes images only from the worker's own page, where its agent runs.

### Context fill

For a native agent, a meter in the agent list and on each ensemble tile shows how full its context window is. Legato pushes the reading as it changes. A terminal agent has no meter.

### Swarms

Select any agent of a [swarm](/legato/using/swarms/) and the page shows the swarm as one unit: the conductor, and every worker in a stack beside it. Click a worker to open it next to the conductor. A **Swarm events** panel lists what the workers reported.

## Ensemble

**Ensemble** shows every running agent as a tile, the ones that need you first. A tile shows the agent's state and time, its task and tool, the end of its conversation or terminal, its recent activity and its context fill. Type in a tile's box and press Enter to message that agent; **Open** takes you to it. Press `e` on the Agents page to switch between the list and the ensemble.

## Attention, plans, reviews and workflows

- **Attention** groups what needs you under **Needs intervention**, **Ready for review** and **Merge blockers**. See [Attention and approvals](/legato/using/attention/).
- **Plans** is the queue of plans waiting for your decision; open one to read it, comment, answer its questions and approve it. See [Plans](/legato/using/plans/).
- **Review** is the queue of review tours; open one to step through the changes and ask the agent about them. See [Reviews](/legato/using/reviews/).
- **Flows** shows workflow runs, those that need you first, and the workflows you can run. See [Workflows](/legato/using/workflows/#runs-and-workflows-in-the-web-ui).

Requests from agents that wait for you (permissions, questions, credentials) appear in a tray at the bottom right of every page. See [Approvals](/legato/using/attention/#approvals).

## Reports, learning and audit

- **Reports** shows agent time by day, week or month. See [Reports](/legato/using/reports/).
- **Learning** lists and searches the learning ledger, and holds its decisions. See [Legato Learn](/legato/using/learn/).
- **Audit** lists which agent asked for, was given, was refused or used which credential, and the grants you made that still stand, each with **Revoke**.

## Remote

**Remote** lists the clones Legato made for agents (with `repository_cache.enabled`): each clone's repository, branch, path, tasks, whether an agent works in it, and when Legato will remove it. **Remove** deletes a clone's whole folder, changed and untracked files included, after you confirm. You cannot remove a clone while an agent works in it.

## Settings

Choose a category, or search by task, title or configuration key. On a wide screen, categories stay beside the selected section. On a phone, open a category from the list; **Back to Settings** returns to it. Search results open and focus the matching control. Existing setting links still work.

- **Preferences**: app and terminal appearance, motion, prompt detection, keyboard switching and browser notifications.
- **Agents and workspaces**: repositories, directory presets, resources, [skills](/legato/agent-tools/skills/), guidance, agent tools, workspaces, macros, sandbox, swarms and engrams. Managed clones link to **Remote**.
- **Reeve**: sessions, automation, rules, engram, pairings and voice.
- **Security**: credentials and sources, grants, audit, signing devices and integration tokens.
- **Integrations**: Jira, GitHub, Burnlite, Argus, MCP servers, MQTT and server notification delivery.
- **Connections and handoff**: browser servers, pairing, TLS, workers and main instances.
- **Server and maintenance**: tint, updates/restart, configuration sources, migration, import/export and conditional development controls.

Choose **Follow server** to use the active server’s **Preferred theme**. Set that preference in Settings › Server and maintenance › Server appearance, or use the [Home Assistant select](/legato/integrations/home-assistant/#control-the-server-theme). **No preference**, an older server or an unsupported theme uses your saved local choice. A lost connection keeps that server’s last known preference; switching to another offline server uses only its own cached preference, otherwise your local choice. Selecting a named theme leaves Follow server. Older native apps may need an update before they offer it.

**Ocular Light Vivid** and **Ocular Dark Vivid** keep the matching original backgrounds and surface depth, with stronger action, focus and status highlights. Dark Vivid uses brighter, clearer accents; Light Vivid uses richer, deeper accents. The originals remain available. Vivid is a visual alternative, not a proven eye-strain treatment.

Settings show their scope: this browser/device or the selected server. A server value shows **default**, **config.yaml** with a lock, or **set here** with **Reset**. File-only settings explain where to edit them. **Applies at restart** means the saved change waits for a restart.
All Ocular themes use purple for actions and identity, green for activity and success, and blue for links. Agent-to-agent messages have a purple-tinted interior; Legato notices have a teal-tinted interior. Voice and signing keep the purple-and-green character. Text selection and the reading background remain separate, and the message box keeps its rounded inset focus indicator.

To tell your Legatos apart at a glance, give each a tint in **Settings › Server and maintenance › Tint**: red, orange, yellow, green, teal, blue, purple or pink. The tint is saved on the server, so every browser and phone that uses it shows it: a band across the top of the window, a wash on the rail, and a dot beside the server in the switcher. A tint takes priority over the app canvas for the browser's address-bar colour.

Toggles and choices save immediately. Text and multi-field edits use **Apply** or **Save** and **Cancel**; leaving a field does not save it. If saving fails, the draft stays for retry. When another client changes the value, choose **Reload value** or **Keep my edit** before saving. An approval request is not a completed change.

Lists live in their owning category, not a separate Lists page. Common entries have named fields; **Advanced JSON** keeps other fields available. File entries are locked. Credentials use secret references, never pasted secrets. **Export and import settings**, in Server and maintenance, downloads the server settings or proposes an import for approval.

## Message delivery safety

Terminal messages and keys are refused unless Legato can verify the agent tool
is in the foreground. A pane back at a shell receives nothing. Native agents
never fall back to terminal typing when their connection fails; use their
native message and approval controls. A terminal-only dialog in a native
session needs deliberate terminal attachment, not a raw-key action button.
Voice with auto-send uses the native connection; without auto-send, use the
native composer's dictation rather than asking the server to type a draft.
