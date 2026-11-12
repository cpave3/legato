---
title: Board and tasks
description: The kanban board of tasks, what a card shows, and how you create, move, find and archive tasks.
sidebar:
  order: 10
---

The board is where your tasks live. Each column is a stage of work, and each card is a task. A card also shows the agent that works on the task, the state of its pull request, and anything that went wrong. You use the board in the TUI (run `legato`) and on the **Board** page of the [web UI](/legato/using/web-ui/).

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
- The ticket type, the workspace (in the All view), the swarm's progress, the review mark, and the state of the pull request: CI, review and comments, such as `✓ CI · ✓ review · 2 comments`. A task built as a stack of branches has several pull requests; the card names each with its state, from the bottom of the stack: `#11 ✗ CI · #12 draft PR`.
- A warning when Legato could not send a change to the ticket provider (`⚠ sync failed` in the web UI).
- An agent strip at the foot, when an agent works on the task.

The agent strip shows the agent's state and how long it has been in it:

| State | Meaning |
| --- | --- |
| running | The agent is working. |
| needs you | The agent waits for you: a permission, a question, or a plan to approve. |
| background | The agent's turn ended, but something it started still runs and will wake it: a shell, a Monitor, a subagent or a scheduled prompt. |
| idle | The agent is alive, and nothing will wake it but a message. |
| error | The agent stopped on an error. |
| mixed (TUI), Agents disagree (web) | The task's agents are in different states. |

`ready for review` replaces `idle` when the agent marked its review tour ready. When Legato runs a second agent on the task, such as a reviewer, the strip adds a chip for it, `reviewer · running`, and says `needs you` when either agent waits for you. A card with no live agent shows the time agents spent on it. The web UI also names the agent tool in the strip, and draws a moving light round the edge of a card whose agent is live. You can turn that light off in **Settings › Appearance**.

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

Press `m` to move the selected task. Each column has a letter, usually the first letter of its name, so `m` then `d` moves a card to Doing. In the web UI you can also drag a card to another column.

With a ticket provider, moving a card also moves its ticket. If Legato cannot send the move, the card stays where you put it and shows a warning. A move you make wins over the provider's status for five minutes; after that, the next sync takes the provider's status.

Press `d` on the board, or `D` in the detail view, to delete a task. You confirm with `y`. Deleting a remote ticket removes only Legato's copy, not the ticket.

## Handing a task off to a worker

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
