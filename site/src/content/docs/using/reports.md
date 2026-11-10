---
title: Reports
description: See how much time your agents spent working and waiting, by day, task, workspace, swarm and run.
sidebar:
  order: 90
---

Reports show where your agents' time went over a day, a week or a month: how long they worked, how long they waited for you, and on which tasks. Use them to see which tasks took the most agent time, or how much of it went to waiting on you.

## What is measured

Legato records each agent's state as it changes: working, waiting for you, or stopped on an error. A report adds up that time for the period you choose.

- **Active time** is time an agent spent working.
- **Waiting time** is time an agent spent waiting for you.
- **Error time** is time an agent spent stopped on an error.
- **Active ratio** is the share of that time the agents spent working.

Time is added across agents. Two agents that work for an hour side by side count as two hours of active time, so a swarm's active time can be longer than the time it took. Legato knows activity only from the moment it started recording it. A report says when that was, and does not compare periods before it.

A report also counts the tasks created and completed, the agent sessions started, and the pull requests merged in the period.

The report breaks the time down:

| Breakdown | What it lists |
| --- | --- |
| Daily activity | Active and waiting time for each day. |
| Tasks | Time for each task. |
| Workspaces | Time and task count for each [workspace](/legato/using/board/#workspaces). |
| Directories (Projects in the web UI) | Time and task count for each working directory. |
| Swarms | Time for each [swarm](/legato/using/swarms/), with its elapsed time, how parallel it ran, and its workers and sub-tasks. |
| Executions | Each agent run, with its kind, status and task, the [engram](/legato/agent-tools/engrams/) it ran as, its time, and how long it lasted from start to end. The TUI leaves this one out. |

## In the TUI

Press `T` on the board. The report opens on today.

| Key | Action |
| --- | --- |
| `tab` | Change between day, week and month |
| `h` / `l` | Previous or next period |
| `j` / `k` | Scroll the task list |
| `C` | Copy the report as Markdown |
| `esc` / `q` | Back to the board |

A week starts on Monday.

## In the web UI

Open **Reports** in the navigation (on a phone, under **More**). The page opens on this week. Choose **Day**, **Week** or **Month**, and use the arrows to step back through earlier periods. Each figure shows how it changed from the period before. The breakdown tables sort by any column.

**Copy Markdown** copies the same report the TUI copies.
