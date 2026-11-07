---
title: Swarms
description: Split one task across several agents, with a conductor that plans the work and workers that each do a part.
sidebar:
  order: 80
---

A swarm splits one task across several agents. You start a **conductor** on the task. The conductor explores the code, writes a plan of sub-tasks, and asks you to approve it. Then it starts a **worker** for each sub-task, follows their progress, checks their work, and finishes the swarm when the task is done. Use a swarm for work that falls into parts that can go on side by side.

Swarms are off by default. Set `swarm.enabled: true` in `config.yaml` and restart Legato to use them; until then the swarm commands, the terminal UI keys and the web **Start swarm** buttons are not available.

Only the conductor plans. Workers do one focused sub-task each and cannot start workers of their own.

## Starting a swarm

1. Select the task's card on the board.
2. Press `S`.
3. Check the working directory and press `enter`.

Legato offers the path of the card's workspace as the working directory, when the workspace has one. In the web UI, choose **Start swarm** in the task's detail, or **Start swarm** on the Agents page. The web UI offers the task's worktree, then the directory you used last for the task, then the default directory.

Legato refuses to start a second swarm on a task that still has one. Cancel the old swarm first.

## Approving the plan

When the conductor proposes its plan, Legato shows it to you: an overlay in the TUI, a dialog in the web UI.

| TUI key | Web | What happens |
| --- | --- | --- |
| `y` | **Approve** | The conductor starts the workers. |
| `e` | (TUI only) | Opens the plan in your editor. Save it, then press `y` to approve your version. |
| `n` | **Reject with notes** | You write what to change. The conductor revises the plan and proposes it again. |
| `esc` | **Dismiss** | Closes the plan without a decision. The conductor keeps waiting. |

A plan has steps, and each step has sub-tasks. The sub-tasks of one step run side by side; the next step starts when the conductor moves on to it. Each sub-task has a title and can name a role, an agent tool, a tier, a scope and a prompt.

A conductor can add work to a running swarm by proposing an extension plan. You approve it the same way; the dialog's title says it is an extension.

## While the swarm runs

Each worker runs in its own session and reports to the conductor: progress, questions, and when it has built its part. The conductor reads those reports, sends follow-ups, and closes each worker when its part is done.

On the board, the task's card shows the swarm's progress, such as `2/5 ◊` in the TUI or `2/5 workers` in the web UI.

In the agents view, a swarm's agents stay together, conductor first. In the TUI:

| Key | Action |
| --- | --- |
| `M` | Swarm actions: send a message to the agent, close a worker, or finish the swarm (on the conductor) |
| `l` | Show or hide worker details |

In the web UI, select any agent of the swarm to see the conductor with every worker beside it. Click a worker to open it next to the conductor. Each of the swarm's agents in the list has a `⋯` menu with the same actions. A **Swarm events** panel lists what the workers reported; **Drain** marks the events read.

## Finishing and cancelling

When the work is done, the conductor finishes the swarm. Legato stops every worker and adds the conductor's summary to the task's description. The conductor stays running, so you can still ask it questions. Stop it in the agents view when you are done.

To cancel a swarm at any point, press `C` on the board, or **Cancel swarm** in the task's detail in the web UI. Cancelling stops the conductor and every worker and removes the sub-tasks. The task and its history stay. Stopping the conductor from the web UI's agent menu cancels the swarm the same way.

## Scope

A sub-task's scope is a list of file patterns it owns, such as `internal/service/**`. When two workers' scopes cover the same file, Legato warns the conductor. To refuse to start a worker whose scope overlaps another's:

```yaml
swarm:
  strict_scope: true
```

## Choosing agent tools and models

The conductor and the workers can use different agent tools:

```yaml
swarm:
  conductor:
    agent: claude-code
    model: claude-opus-4-7     # optional
  worker:
    agent: chimera
    model: manticore/luna      # optional
```

- The conductor uses `swarm.conductor.agent`, else `swarm.worker.agent`, else Legato's default agent tool.
- A worker uses the sub-task's own `agent`, else `swarm.worker.agent`.
- `swarm.worker.model` applies only to workers that use `swarm.worker.agent` and name no tier.

### Tiers

A tier is a named launch profile of an agent tool, usually a model. The conductor sees your tiers and their descriptions and picks one for each sub-task:

```yaml
adapters:
  claude-code:
    tiers:
      small:
        description: "fast and cheap; renames and single-file fixes"
        launch_args: ["--model", "claude-haiku-4-5"]
      large:
        description: "deep reasoning; multi-file refactors and tricky bugs"
        launch_args: ["--model", "claude-opus-4-7"]
```

A plan that names a tier the sub-task's agent tool does not have is refused. A sub-task with no tier uses the tool's base `launch_args`.

## Other settings

| Setting | What it does |
| --- | --- |
| `swarm.max_concurrent_agents` | How many workers of one swarm run at once. 4 unless set. |
| `swarm.max_subtasks_per_plan` | The most sub-tasks a plan may have. 10 unless set. |
| `swarm.max_steps_per_plan` | The most steps a plan may have. 10 unless set. |
| `swarm.brief_kickoff_delay_ms` | How long Legato waits after starting an agent before telling it to read its brief. 250 unless set. |
| `swarm.prompts.<role>.<tool>` | Your own role prompt for a role on one agent tool. |
| `adapters.chimera.modes` | A Chimera mode for each role, such as `worker: legato-worker`. You write the mode files. |

See the [configuration reference](/legato/reference/configuration/) for every setting, and the [command reference](/legato/reference/cli/) for the `legato swarm` commands the conductor and workers use.
