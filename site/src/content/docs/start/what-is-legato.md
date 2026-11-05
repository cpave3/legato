---
title: What is Legato?
description: The ideas behind Legato — tasks, agents, Attention, plans and reviews — and how they fit together.
sidebar:
  order: 10
---

Legato is a board of tasks and the AI coding agents that work on them. You keep your work as tasks on a kanban board. You start an agent (Claude Code, Codex or Chimera) on a task, and Legato tracks it: what the agent is doing, when it needs you, what it proposes and what it made.

Legato runs on your machine. It is one binary with a terminal UI (the TUI), a web UI and a command-line interface, and it keeps its data in a local SQLite database.

## Why Legato

When several agents work at once, the work is easy to lose: which terminal has the agent that asked a question, which branch belongs to which ticket, what an agent changed while you were away. Legato puts the work in one place:

- Every agent is attached to a task, and its card shows its state.
- Everything that needs you is in one queue, [Attention](/legato/using/attention/).
- You can answer agents from the terminal, the browser or your phone.

## The parts

### Tasks and the board

A **task** is one piece of work. A **local task** exists only in Legato. A **remote ticket** comes from a ticket provider such as [Jira](/legato/integrations/jira/), and Legato keeps it in step with the provider. Tasks sit in the columns of the **board**, and you can put them in **workspaces** to keep, for example, work and personal tasks apart. See [Board and tasks](/legato/using/board/).

### Agents

An **agent** is an AI coding tool that Legato starts in a tmux session, most often on a task. Legato gives the agent the task, follows its state (running, needs you, background, idle) and shows it on the card. You can attach to the agent's terminal, send it messages and macros, and restart it with its conversation. See [Agents](/legato/using/agents/).

With the **native bridge**, Legato also talks to Claude Code and Codex directly. The web UI then shows the agent's conversation, and you answer its permission requests and questions in Legato. See [Agent tools](/legato/agent-tools/claude-code/).

### Attention and approvals

**Attention** is the one queue of things that need you: a plan to approve, a review to read, a failed workflow, a pull request with a failed check, an update. An **approval** is a request from an agent that waits for your decision, such as a permission, a question or the use of a credential. See [Attention and approvals](/legato/using/attention/).

### Plans and reviews

Before it writes code, an agent can submit a **plan** for you to read, comment on and approve. After it writes code, it can make a **review tour**, which takes you through the change step by step, and a **review handoff**, which records the decisions and evidence behind it. See [Plans](/legato/using/plans/) and [Reviews](/legato/using/reviews/).

### Workflows and swarms

A **workflow** is a recipe of steps, some done by Legato and some by agents, that you can run again. A **swarm** is a group of agents: a conductor that plans the work and workers that do the parts. See [Workflows](/legato/using/workflows/) and [Swarms](/legato/using/swarms/).

### Credentials and the sandbox

Agents can use your SSH keys and API keys **without holding them**: Legato signs a login or adds a key to a request when you allow it. The **sandbox** starts agents where they cannot see the rest of your machine. See [Security](/legato/security/credential-broker/).

## Where Legato runs

| Setup | What it is |
| --- | --- |
| [On your workstation](/legato/running/workstation/) | The TUI in a terminal, with the web UI beside it. The usual setup. |
| [Web server only](/legato/running/web-server/) | `legato serve` without the TUI. |
| [On a server of its own](/legato/running/agent-server/) | An always-on machine where agents work while your laptop is closed. |
| [Main and worker instances](/legato/running/instances/) | Hand a task from your workstation to a worker, and bring it back with its results. Not yet in a release. |
