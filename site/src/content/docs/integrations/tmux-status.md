---
title: tmux status line
description: Show how many agents are working, waiting or idle in a tmux status line.
sidebar:
  order: 80
---

Legato runs each agent in a tmux session. The status line at the bottom of those sessions shows what the other agents are doing, so you see when one needs you without going back to the board. You can put the same summary in your own tmux sessions.

## In Legato's agent sessions

Legato sets the status line of every agent session it starts. You don't need to do anything.

- **A solo agent** shows the other agents: how many are working, waiting, in error and idle. A count of zero is left out, except idle.
- **An agent in a [swarm](/legato/using/swarms/)** shows its swarm: how many subtasks are done out of the total, the latest event and how long ago it was, how many workers are active, and a warning mark when the latest event is a scope warning.

The line updates every five seconds.

### Change it

Legato sets four tmux options on each session: `status-right`, `status-interval` (`5`), `status-style` (`bg=colour235,fg=colour245`) and `status-left` (empty). It applies your `agents.tmux_options` after them, so yours win:

```yaml
agents:
  tmux_options:
    status-style: "bg=default,fg=colour245"
    status-interval: "10"
```

## In your own tmux sessions

`legato agent summary` prints the summary in tmux's style markup, for example:

```text
#[fg=green]2 working #[fg=colour240]· #[fg=yellow]1 waiting #[fg=colour240]· #[fg=colour245]0 idle
```

To show it in every tmux session, add this to `~/.tmux.conf`:

```bash
set -g status-right '#(legato agent summary)'
set -g status-interval 5
```

tmux runs the command with its own environment, so `legato` must be on the `PATH` of the tmux server, or you give its full path. The command reads Legato's database and exits; it does not need Legato to be running.

| Command | Prints |
| --- | --- |
| `legato agent summary` | The working, waiting, error and idle counts of all agents. |
| `legato agent summary --exclude <task-id>` | The same, without one task's agent. |
| `legato agent status <task-id> --format tmux` | The swarm line for an agent in a swarm, or the summary without that task for a solo agent. |

Add `--json` to either command for its output as JSON.
