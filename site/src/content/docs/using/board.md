---
title: Board and tasks
description: The kanban board of tasks, what a card shows, and how you create, move, find and archive tasks.
sidebar:
  order: 10
---

The board is where your tasks live. Each column is a stage of work, and each card is a task. A card also shows the agent that works on the task, the state of its pull request, and anything that went wrong. You use the board in the TUI (run `legato`) and on the **Board** page of the [web UI](/legato/using/web-ui/).

When tmux is missing, the board still works, but agents are off: the status bar says `tmux not found: agents are off`, and `a` on a card shows the fix instead of starting an agent. See [Agents](/legato/using/agents/#starting-an-agent-on-a-task).

This page covers the board in both. For every key, see [Keyboard shortcuts](/legato/reference/keyboard/), or press `?` in the TUI.

## Columns

On first run, Legato gives the board five columns: Backlog, Ready, Doing, Review and Done. You choose your own columns with `board.columns` in the configuration:

```yaml
board:
  columns:
    - name: Backlog
      remote_statuses: ["To Do", "Open", "Backlog"]
    - name: Doing
      remote_statuses: ["In Progress"]
      remote_transition_id: "21"
    - name: Review
      remote_statuses: ["In Review"]
    - name: Done
      remote_statuses: ["Done", "Closed"]
```

`remote_statuses` and `remote_transition_id` matter only with a ticket provider such as [Jira](/legato/integrations/jira/). A column with no `remote_statuses` is local: a sync refreshes the details of its tickets but never moves them out of it.

The board keeps its columns in its database. After you change `board.columns`, apply the change:

```bash
legato columns sync
```

Tasks in a column you removed move to a column named `Unmapped` when you have one, and otherwise to your first column. The setup wizard makes an `Unmapped` column for Jira statuses it cannot match to a column.

A column with no cards collapses to a narrow rail with its count and its name written downwards. Press `z` to open the empty columns, and `z` again to collapse them. `h` and `l` step over a collapsed column; a number key still jumps to it. A board with no cards at all keeps every column open.

## Cards

A card shows, from the top:

- The priority mark (`▲` high, `▼` low, `·` none), the task's key, and where the task comes from: `local`, or a ticket provider such as `◈ jira`.
- The title.
- The ticket type, the workspace (in the All view), the swarm's progress and the review mark.
- On the web board, a line for each pull request, from the bottom of the stack: its number, which opens it on GitHub, one word for its state and a detail, such as `#191 Awaiting review · CI passed`. The words are **Merged**, **Closed, not merged** (the number struck through), **Draft**, **CI failing**, **Changes requested**, **CI running**, **Your review** (someone else's pull request waiting for your review), **Awaiting review**, **Ready to merge**, and **Approved** (approved, but a layer below it is not ready yet). A stack of two or more pull requests folds to a few lines:
  - a head, such as **Stack of 13 PRs** with **2 merged** (or **5 PRs in 2 repos**);
  - a bar with a segment for each pull request, from the bottom: blue for merged, green for ready to merge, a green outline for approved but waiting on a layer below, red for failing, amber stripes for CI running, a dashed outline for a draft;
  - the lowest pull request that needs you, such as **#200 CI failing · 6 comments**, with how many above it it holds back and its button;
  - the pull requests you can merge now, from the bottom: **#196–#199 ready to merge, bottom first**;
  - **Show all 13**, which lists the stack from the bottom under its repository and base branch, on a rail that is dashed above a failing layer. A pull request that shares its base with another (a fork) is indented under it, and each repository is a group of its own. The card remembers that it is open until you reload the page.

  A stack that is all merged is one line: **Stack of 10 PRs, all merged**. In the TUI the card names each with its state: `#11 ✗ CI · #12 draft PR`.
- A warning when Legato could not send a change to the ticket provider (`⚠ sync failed` in the web UI).
- An agent strip at the foot, when an agent works on the task.

The agent strip shows the agent's state and how long it has been in it:

| State | Meaning |
| --- | --- |
| running | The agent is working. |
| needs you | The agent waits for you: a permission, a question, a plan to approve, or something it [posted for you](/legato/using/attention/#operator-items), such as a command to run, while that stays open. |
| background | The agent's turn ended, but something it started still runs and will wake it: a shell, a Monitor, a subagent or a scheduled prompt. |
| idle | The agent is alive, and nothing will wake it but a message. |
| error | The agent stopped on an error. |
| mixed (TUI), Agents disagree (web) | The task's agents are in different states. |

`ready for review` replaces `idle` when the agent marked its review tour ready. When Legato runs a second agent on the task, such as a reviewer, the strip adds a chip for it, `reviewer · running`. In the TUI the strip says `needs you` when either agent waits for you; on the web board a waiting reviewer has a strip of its own above the main agent's. A card with no live agent shows the time agents spent on it. The web UI also names the agent tool in the strip, and draws a moving light round the edge of a card while its agent works. You can turn that light off in **Settings › Appearance**. A card that needs you has a steady amber edge instead, and one whose agent stopped on an error a red one. A card a link points you to has a ring in the theme's focus colour, in every theme and on a phone. Cards in Done are not faded: their title is in a quieter ink.

On the web board, a card that needs you says why, in a strip of its own above the agent's, with the way there:

| Strip | Its action |
| --- | --- |
| **Needs you · 1 question, 1 action**: things the agent [posted for you](/legato/using/attention/#operator-items) | **Open** goes to the agent, with the Needs you rail open; on a phone it opens the Needs you sheet |
| **Needs you · permission to run a command**, or the agent's question | **Answer** opens the approval in the Needs you rail (on a phone, the tray) |
| **Plan to approve** | **Review plan** opens the plan |
| **4 changes to review** | **Review** opens the review tour |
| **Reviewer needs you · question**: a second agent, such as a reviewer, waits | **Answer** opens what it asks, or **Open** goes to it; the main agent's own strip stays below |
| **1 of 2 agents needs you** | **Open** goes to the agents; the strip below says how many run |
| **Interrupted · Claude Code**: the agent stopped when Legato or the machine did | **Resume** starts it again where its conversation was |

What waits on you is drawn above what runs. While the agent itself only waits or idles, the needs-you strip takes the place of its own; a running agent keeps its strip below. The agent strip has its time on the right, and an idle agent's strip names its tool and the time agents spent on the task. The card changes as soon as an item is posted or cleared, even while the agent idles.

To link a pull request to a task, press `p`. Legato also finds pull requests by itself; see [GitHub](/legato/integrations/github/).

In a TUI at least 100 columns wide, a panel beside the board describes the selected task. In the web UI the same panel appears from 1280 pixels of width. It holds the task's details, the start of its description and its actions. Press `s` in the web UI to start or open the task's agent from it.

## Local tasks and remote tickets

A local task belongs to Legato alone. A remote ticket comes from a ticket provider, such as a Jira issue. Legato keeps a copy of the ticket and updates it at each sync, but it never adds tickets to the board by itself: you import each one.

- Press `n` to create a local task. You give it a title, a description, a column, a priority (`ctrl+p` cycles it) and a workspace.
- Press `i` to search the ticket providers and import a ticket. Type at least two characters. In the web UI, **Import** in the board's header (the download button on a phone) opens the same dialog. Choose a result with `↑` and `↓` and press `enter`, or click it; `esc` closes the dialog. With no provider connected, the dialog sends you to Settings to connect Burnlite or Jira.
- Press `r` to sync with the ticket provider now. It also pulls every [worker](/legato/using/handoff/#staying-up-to-date) holding tasks of yours.

In the web UI's import dialog you also choose where the task starts:

- **Workspace** starts as the workspace the board shows. With **Unassigned** shown it is **No workspace**, and with **All** it is `default_import_workspace`.
- **Column** starts as **Provider default**: the column the ticket's status maps to, else Backlog. Pick a column to start the task there instead. Importing never changes the ticket. The column is only a starting point: when the ticket changes in its provider, the next sync moves the card to the column its status maps to, as for any ticket.

When the import is done, a message says where the task is, such as "Imported LEG-12 to Ready · backend". Importing a ticket that is already on the board leaves its task where it is.

You can rename only a local task. For any task you can edit the details that belong to Legato with `e` in the detail view. On a remote ticket, the provider's own description stays read-only, and the detail view shows both together.

## Moving and deleting tasks

Press `m` to move the selected task. Each column has a letter, usually the first letter of its name, so `m` then `d` moves a card to Doing. In the web UI you can also drag a card to another column, or up and down its own to change the order: a line shows where it will land. The order is saved, so the TUI and every browser show it too.

With a ticket provider, moving a card also moves its ticket. If Legato cannot send the move, the card stays where you put it and shows a warning. A move you make wins over the provider's status for five minutes; after that, the next sync takes the provider's status.

Press `d` on the board, or `D` in the detail view, to delete a task. You confirm with `y`. Deleting a remote ticket removes only Legato's copy, not the ticket.

## Handing a task off to a worker

## Review a card's pull request in passes

The same menu lists your [macros](/legato/using/agents/#macros) for a task, such as **Plan** and **Implement**, each with the agent and model tier it starts when it sets them. Choose one to put the task's agent to work with it: when no agent is running, Legato starts one with the macro's agent and model tier (or your default agent) and the macro as its first message; when one is running, the macro becomes its next message, after its current turn. The message starts with the task's key and title. **Manage macros** opens the Macros page.

Below the task's macros, the menu has a **Macros for #N** item for each open pull request of the card, bottom of the stack first. Choose one to list the macros for that pull request, the suggested one first, and pick one to start it on that pull request. On a phone, long-press the card for the same menu.

A task from a ticket tracker also has **Open in Burnlite** or **Open in Jira** in the menu, which opens the ticket there.

In the TUI, press `c` on a card and choose **Macros ›**: the task's macros come first, then each open pull request's, labelled with its number.

A pull request that needs you has one button on the card, in a box on the colour of its state. A press starts the suggested macro on that pull request: **Address feedback** on your own pull request (one Legato found from the task's branch) when CI fails or a reviewer asked for changes, **Review** on someone else's (one you linked by its number) while it waits for your review. A pull request that needs nothing from you, such as a green one, a draft or a merged one, has no button; its macros are in the card's menu. The message adds the pull request's `owner/repo#number` and its link.

A card with an open pull request offers **Review passes · #N**, one for each pull request: in the web board's `⋯` or right-click menu, and in the TUI under `c` › Workflows. It starts a [review loop](/legato/using/review-passes/#an-agent-as-the-author) on that pull request with the card's own agent as the author, so the agent fixes what the reviewer finds. When the card has several open pull requests in one repository (a stack, in the order the card tracks them), the first item is **Review passes · stack #137–#141**: one loop over every open pull request of the stack, started on the bottom one; each **Review passes · #N** below it then reviews that pull request alone. It needs the `review-pr-passes` workflow (`legato workflow add review-pr-passes`). When a loop is already running on the pull request, the web board opens it instead, and Legato refuses a second one, naming the first.

On a main instance paired with a [worker instance](/legato/running/instances/), the web board can send a task to the worker, which starts an agent on it. Drag the card onto the **Away** lane (a narrow rail while nothing is away), or choose **Hand off…** in the card's `⋯` or right-click menu, or in the panel beside the board. The task then leaves its column for the **Away** lane at the end of the web board, grouped by worker (on a phone, an **Away** section above the columns), and stays read-only on your main until you bring it back with **Return** or **Recall** on its row or in its panel, or **Return all done** on its worker's group. When its worker cannot be reached, lost the task or no longer accepts your main, **Take back** brings it back without its results. See [Hand off tasks to a worker](/legato/using/handoff/#bring-a-task-home).

The TUI has no handoff actions, but it shows an away task in the column it left, with "away on" and the worker's name at the foot of the card, and the handoff's state at its end. The detail view names the worker and the state too. Editing, moving, deleting, linking a pull request, starting an agent or a workflow on an away task is refused with a message in the status bar: return or recall the task from the web UI. On a worker, deleting a card a main handed to it is refused the same way, naming the main.

## Task detail

Press `enter` to open a task. The detail view shows the description as Markdown, the ticket's fields, the pull request and the worktree. In the web UI it also lists the task's notes, review tours, plans and workflow runs.

Two keys copy the task for an agent:

| Key | Copies |
| --- | --- |
| `y` | The description. |
| `Y` | The full context, as Markdown: title, type, priority, epic, labels, link, description, attachments and the task's notes. |

`legato task show <task-id> --format full` prints the same context in a terminal. Agents that Legato starts on a task can read it themselves, so you usually do not need to paste it.

`o` opens the ticket or the pull request in your browser.

## Workspaces

Workspaces split the board into areas, such as one per project. A task is in one workspace or in none. Define workspaces in the configuration:

```yaml
workspaces:
  - name: backend
    color: "#4A9EEF"
  - name: frontend
    color: "#E0A030"
default_workspace: backend
default_import_workspace: backend
```

- Workspace names must be unique, ignoring case.
- `default_workspace` is the workspace a new task gets. Without it, a new task gets the workspace you are looking at.
- `default_import_workspace` is the workspace an imported ticket gets. Without it, Legato uses `default_workspace`.
- A workspace can also have a `path`, which Legato offers as the working directory when you start a [swarm](/legato/using/swarms/) on one of its tasks.

Press `w` to show one workspace, the tasks with no workspace, or all of them. The web UI has the same choice in the board's header. Press `v` to put the selected task in another workspace.

## Finding tasks

Press `/` to search tasks by title or ID. Pick a result to jump to its card. In the web UI, `/` searches every task, archived ones too, and **Filter cards** narrows the board as you type.

## Archiving

Press `X` to archive every card in the Done column. Legato finds that column by its name, `Done`. Archived tasks leave the board but stay in the database. Press `B` in the TUI, or `A` on the web board, to browse archived tasks and restore one.

## Groups

A group gathers the agents of several tasks in the agents view. Press `g` there to put the selected agent's task in a group, or to take it out. You can pick a group or type a new one. Groups you always want offered go in the configuration:

```yaml
groups:
  defaults: [backend, urgent]
```

Groups appear in the TUI only.
