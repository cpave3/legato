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

## Sub-agents

An agent can start sub-agents of its own, which go on working while it waits in the background or sits idle. Their time belongs to the agent's task. A report shows it beside the agent's own:

- **Own working** (Active in the summary) is the agents' own working time, as before.
- **Sub-agent working** is the time their sub-agents spent working. Each sub-agent's time counts once, however deep it is and whatever its parent was doing meanwhile.
- **Aggregate working** is the two added together: agent-time. Five sub-agents that work the same 10 minutes add 50 minutes.
- **Elapsed** is wall-clock time during which any of the agents or sub-agents was working, waiting or stopped on an error, with agents active at the same time counted once. Those five sub-agents add 10 minutes. It is not time you spent yourself, and it leaves out time an agent sat idle or in the background.

An agent that starts five sub-agents after working for 10 minutes, each of which works 10 minutes, has 10 minutes of own working time, 50 of sub-agent working time and 60 of aggregate working time. If the sub-agents ran side by side, the elapsed time is 20 minutes.

Which agent tools report their sub-agents:

| Agent tool | Sub-agents |
| --- | --- |
| [Claude Code](/legato/agent-tools/claude-code/) | Each subagent, from its start to its end, waiting while it asks you for a permission. Claude Code's own helpers (prompt suggestions, compaction) are not counted. |
| Chimera with its Legato mod | The session's own subagents, from spawn to finish, counted as working throughout. A subagent's own subagents are not reported. |
| Chimera without the mod, Codex | Not reported. |

A total that lacks some sub-agent time is marked **†**: it includes agents whose tool does not report sub-agents, or agents from before Legato recorded them. For those, only the agents' own time is known, so the aggregate is a lower bound. Sub-agent time from before recording began cannot be recovered.

A report also counts the tasks created and completed, the agent sessions started, and the pull requests merged in the period.

The report breaks the time down:

| Breakdown | What it lists |
| --- | --- |
| Daily activity | Active and waiting time for each day. |
| Tasks | Time for each task: own, sub-agent and aggregate working time, elapsed time, waiting and errors. |
| Workspaces | Time and task count for each [workspace](/legato/using/board/#workspaces). |
| Directories (Projects in the web UI) | Time and task count for each working directory. |
| Swarms | Time for each [swarm](/legato/using/swarms/), with its elapsed time, how parallel it ran, and its workers and sub-tasks. |
| Executions | Each agent run, with its kind, status and task, the [engram](/legato/agent-tools/engrams/) it ran as, its time and its sub-agents' working time, and how long it lasted from start to end. The TUI leaves this one out. |

Workspaces, directories and swarms count the agents' own time only.

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
