---
title: On your workstation
description: Run the Legato TUI in a terminal, with its agents in tmux and the web UI beside it.
sidebar:
  order: 10
---

The usual way to run Legato is on the machine you work at. You run the TUI in a terminal, Legato starts agents in tmux sessions, and the web UI runs in the same process so you can also follow your agents from a browser or your phone.

## Start the TUI

```bash
legato
```

The first time, Legato sets up your board's columns and offers to connect Jira. After that, `legato` opens the board. Press `?` to see the keys, or read [Keyboard shortcuts](/legato/reference/keyboard/).

Quit with `q` or `ctrl+c`.

## Agents run in tmux

Press `a` on a card to spawn an agent for that task. Legato starts the agent tool in a tmux session named `legato-<task id>`. Open the agents view with `A` to see each agent's terminal, and attach to one to type into it. Detach again with `ctrl+]`, or the key you set in `agents.escape_key`.

The agents live in tmux, not in Legato. When you quit Legato, they keep working. When you start Legato again, it finds them.

## Turn on the web UI

Add this to `~/.config/legato/config.yaml`:

```yaml
web:
  enabled: true
  port: "3080"   # the default
```

When the TUI starts, it starts the web server on that port. If the port is already in use, for example by another Legato, the TUI skips the web server and runs without it. The key bar shows `Web :3080` while the server runs.

Open `http://localhost:3080` in a browser. The first time, the page asks for a token. Print it in a terminal:

```bash
legato auth token
```

See [Web access and tokens](/legato/security/web-auth/) for what the token protects and how other devices get it.

The web server stops when you quit the TUI.

## What the web UI gives you

The web UI shows the same board and agents as the TUI, and more:

- Each agent's terminal, streamed live, and for Claude Code, Codex and Chimera a conversation view where you can read and answer the agent.
- An ensemble of every agent as a tile.
- An approval tray for what agents ask permission for.
- Attention, plans, reviews, workflows, reports and Legato Learn.
- The credential audit log and the settings.
- Your phone and other devices, with the same page. See [Your phone and other devices](/legato/running/devices/).

See [Web UI](/legato/using/web-ui/) for a tour.

## More than one Legato at once

You can run several Legato TUIs at the same time, for example one in each terminal or project. They share one database, so they show the same tasks and agents.

Each TUI listens on its own socket. When a `legato` command changes something, such as an agent hook that reports the agent's state, it tells every running Legato, and every open board updates at once.

Only one of them runs the web server: the first to take the port. The others skip it.

To send tasks from one Legato to another on a different machine, see [Main and worker instances](/legato/running/instances/).

## Where Legato keeps its files

| What | Where |
| --- | --- |
| Configuration | `~/.config/legato/config.yaml`. `$XDG_CONFIG_HOME/legato/config.yaml` when `XDG_CONFIG_HOME` is set, or the file `LEGATO_CONFIG` names. |
| Database | `~/.local/share/legato/legato.db`, or `$XDG_DATA_HOME/legato/legato.db`. Set `db.path` to put it elsewhere. |
| Data folder | The folder the database is in, unless you set `data_dir`. It holds the web token (`auth-token`), the TLS certificates (`certs/`), cached Jira attachments, workflow artifacts, and Legato's copies of repositories. |
| Sockets | `$XDG_RUNTIME_DIR/legato/`, or `/tmp/legato-<uid>/` without it. |

```yaml
db:
  path: $HOME/legato/legato.db
data_dir: $HOME/legato/data
```

Write these paths in full or with `$HOME`: Legato expands environment variables in the configuration, but not `~` in these two settings.

See the [configuration reference](/legato/reference/configuration/) for every setting.
