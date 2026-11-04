---
title: Burnlite
description: Import Burnlite tasks onto the board and keep them up to date, next to your Jira tickets.
sidebar:
  order: 15
---

Legato can track tasks from Burnlite, an issue tracker and estimator, as cards on its board. You import the tasks you want to work on, and Legato keeps their name, description, plan, checklist and state up to date. Burnlite works next to Jira: a board can hold tasks from both.

Legato only reads Burnlite. When you move a card, the task in Burnlite does not change.

## Connect Legato to Burnlite

Legato connects to Burnlite as an OAuth client. You allow it once in Burnlite, and Legato asks only to read your tasks. You can connect from the web UI or from the command line.

### From the web UI

1. Open **Settings › Integrations**.
2. In the Burnlite block, type your Burnlite's address, such as `https://burnlite.example.com`.
3. Select **Connect**. Burnlite opens and asks you to allow Legato.
4. Allow it. Burnlite sends you back to Legato's settings, which say that Legato is connected.

### From the command line

On the machine that runs Legato:

```bash
legato integration connect burnlite https://burnlite.example.com
```

The command opens Burnlite in your browser. If your browser runs on another machine, the browser cannot reach Legato's return address. Copy the address that the browser tried to open, paste it into the command, and press Enter.

`legato integration status burnlite` says whether Legato is connected. `legato integration disconnect burnlite` removes the connection.

:::caution
Burnlite gives Legato a new refresh token every hour. Legato saves each token in its secret store without you. If a passphrase unlocks that store, connecting fails: set `secrets.master_key` to a source that Legato can read on its own. See [Credentials setup](/legato/security/credentials-setup/).
:::

Legato needs a Burnlite whose tasks have an update time (`updatedAt`). If your Burnlite is older, the sync says to update it.

## Import a task

On the board, press `i` in the TUI, or use the import dialog in the web UI. Type part of a task's name, or its key, such as `EST-42`. The results show which provider each task comes from. Select a task to import it.

A search by name looks in the open tasks of your projects. A search by key also finds a closed task.

The [browser extension](/legato/integrations/browser-extension/) can also import the task you have open in Burnlite.

## Choose the projects

By default, a search looks in every Burnlite project. To search fewer projects, list their keys (`EST` in `EST-42`):

- In **Settings › Integrations**, type the keys in the Burnlite block and select **Save**.
- Or set `burnlite.projects` in `config.yaml`. When this setting has keys, it replaces the list in the web UI.

```yaml
burnlite:
  url: https://burnlite.example.com
  projects: [EST, OPS]
```

## What a card shows

Each Burnlite card shows its state, and the state puts the card in a column:

| State | When | Column (default) |
| --- | --- | --- |
| Ready | Open, nobody has claimed it, and no open task blocks it | Ready |
| Blocked | Open, and an open task blocks it | Backlog |
| Claimed | Open, and someone has claimed it | Doing |
| Closed or Not planned | Closed | Done |

A claimed task that is also blocked is Claimed.

The task's detail shows its description, then its plan and its checklist. It also shows its kind (Work or Meta), its milestone, who claimed it, its labels, its size and uncertainty, the tasks that block it, and, for a closed task, how it was resolved.

To put the states in other columns, set `burnlite.columns`:

```yaml
burnlite:
  columns:
    ready: Ready
    blocked: Backlog
    claimed: Doing
    closed: Done
```

When a task changes state in Burnlite, its card moves to that state's column. A card that you moved in the last five minutes stays where you put it, and so does a card in a column with no remote statuses. Set `burnlite.status_sync_enabled: false` to keep cards where you put them. Legato then still updates the task's details.

## Keep tasks up to date

Legato checks Burnlite every 60 seconds (`burnlite.sync_interval_seconds`). It gets the full text only of the tasks that changed. A task that Burnlite no longer has is marked as stale.

If Burnlite stops accepting Legato's tokens, Attention says that Legato lost its connection. Connect again from **Settings › Integrations** or with `legato integration connect burnlite`.

## Settings

| Setting | What it does | Default |
| --- | --- | --- |
| `burnlite.url` | The address that connecting uses when you give none. | none |
| `burnlite.projects` | The projects that a search looks in. | every project |
| `burnlite.sync_interval_seconds` | Seconds between checks. | `60` |
| `burnlite.status_sync_enabled` | Whether a change of state moves the card. | `true` |
| `burnlite.columns.ready`, `.blocked`, `.claimed`, `.closed` | The column of each state. | `Ready`, `Backlog`, `Doing`, `Done` |

See the [configuration reference](/legato/reference/configuration/) for every setting.
