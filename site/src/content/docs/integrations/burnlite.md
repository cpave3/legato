---
title: Burnlite
description: Import Burnlite tasks onto the board and keep them up to date, next to your Jira tickets.
sidebar:
  order: 15
---

Legato can track tasks from Burnlite, an issue tracker and estimator, as cards on its board. You import the tasks you want to work on, and Legato keeps their name, description, plan, checklist and state up to date. Burnlite works next to Jira: a board can hold tasks from both.

Legato reads Burnlite. When you move a card, the task in Burnlite does not change. Legato writes to Burnlite only from workflow steps: `burnlite.task.close` closes a task's ticket, for example when its pull request merges (see [Close a ticket from a workflow](#close-a-ticket-from-a-workflow)), `ticket.create` and `ticket.comment` create a task or add a Note to one (see [Create a ticket or add a Note from a workflow](#create-a-ticket-or-add-a-note-from-a-workflow)), and `ticket.update` claims a task or changes its stage (see [Claim a task or change its stage from a workflow](#claim-a-task-or-change-its-stage-from-a-workflow)).

## Connect Legato to Burnlite

Legato connects to Burnlite as an OAuth client. You allow it once in Burnlite. One connection serves two things:

- **The board**, which reads your tasks and closes a ticket from a workflow.
- **Agents**, which get Burnlite's tools through the [MCP server connection](/legato/agent-tools/mcp-tools/#tools-of-mcp-servers-you-connect) named `burnlite`.

Legato asks Burnlite once for everything both of them need: `read:tasks write:tasks write:projects`. Agents get only the tools that your rules allow. You can connect from the web UI, from the command line, or from **Settings › Integrations › MCP servers**. Wherever you connect, the board and agents use the same connection.

### From the web UI

1. Open **Settings › Board and tickets › Burnlite**.
2. In the Burnlite block, type your Burnlite's address, such as `https://burnlite.example.com`.
3. Select **Connect**. Burnlite opens and asks you to allow Legato.
4. Allow it. Burnlite sends you back to Legato's settings, which say that Legato is connected.

If there is no MCP connection to Burnlite yet, connecting makes one, named `burnlite`, and Settings says so. That connection has no rules, so agents get no Burnlite tools until you add rules to it in **Settings › Integrations › MCP servers**.

### From the command line

On the machine that runs Legato:

```bash
legato integration connect burnlite https://burnlite.example.com
```

The command opens Burnlite in your browser. If your browser runs on another machine, the browser cannot reach Legato's return address. Copy the address that the browser tried to open, paste it into the command, and press Enter.

`legato integration status burnlite` says whether Legato is connected, and which MCP connection shares the connection. `legato integration disconnect burnlite` disconnects the board and agents both, and asks Burnlite to revoke Legato's tokens.

### Disconnect

**Disconnect** in **Settings › Board and tickets › Burnlite** stops the sync and takes Burnlite's tools away from agents. Legato asks Burnlite to revoke its tokens. The MCP connection and its rules stay, ready for you to connect again. **Disconnect** on the `burnlite` connection in **MCP servers** does the same thing: the board shows that it is not connected.

To take Burnlite's tools away from agents and keep the sync, turn the MCP connection off in **MCP servers**. The switch applies only to agents.

### A Burnlite on your own computer

If you run Burnlite on the same machine as Legato, type its address with the loopback address, such as `http://127.0.0.1:3000`. Legato reaches that address only for this connection. A name such as `localhost` is refused.

:::caution
Burnlite gives Legato a new refresh token every hour. Legato saves each token in its secret store without you. If a passphrase unlocks that store, connecting fails: set `secrets.master_key` to a source that Legato can read on its own. See [Credentials setup](/legato/security/credentials-setup/).
:::

A connection made before Legato could close tickets reads only. A `burnlite.task.close`, `ticket.create`, `ticket.comment` or `ticket.update` step on it fails and says to connect again: connect again as above, and allow the new access.

Older versions of Legato kept two connections to Burnlite: one for the board and one for agents' MCP connection. The first time a newer Legato starts, it joins them. It keeps the connection with the wider access, or the newer one when they have the same access. It asks Burnlite to revoke the other connection's tokens, and keeps the MCP connection's rules. Before it changes anything, it saves a copy of the database beside it, named `<database>.<time>.leg-544.bak`. Its log says which connection it kept and which one it revoked.

Legato needs a Burnlite whose tasks have an update time (`updatedAt`). If your Burnlite is older, the sync says to update it.

## Import a task

On the board, press `i` in the TUI, or use the import dialog in the web UI. Type part of a task's name, or its key, such as `EST-42`. The results show which provider each task comes from. Select a task to import it.

A search by name looks in the open tasks of your projects. A search by key also finds a closed task, or a milestone.

### Import a milestone

Import a milestone by its key, such as `EST-3`, the same way. Its card has the milestone's name, and its description is the milestone's description followed by a **Tasks** list: a line for each task in the milestone, with its key (a link to Burnlite), its name and its state, ticked when it is closed. Each sync brings the list up to date, as tasks are added to the milestone, leave it, are claimed or are closed. The tasks themselves are not imported: import any you want as cards of their own.

The [browser extension](/legato/integrations/browser-extension/) can also import the task or milestone you have open in Burnlite.

A [workflow](/legato/using/workflows/#tickets-and-cards-as-data) can import tasks and milestones with `ticket.import`, and list them with `tickets.list`: a milestone's frontier, a project's tasks by label, or the milestones ready to build.

## Choose the projects

By default, a search looks in every Burnlite project. To search fewer projects, list their keys (`EST` in `EST-42`):

- In **Settings › Board and tickets › Burnlite**, type the keys in the Burnlite block and select **Save**.
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

If Burnlite stops accepting Legato's tokens, Attention says that Legato lost its connection. The board and agents both lost it. Connect again from **Settings › Board and tickets › Burnlite**, from **MCP servers**, or with `legato integration connect burnlite`.

## Hand off a Burnlite task

A Burnlite task you [hand off to a worker](/legato/using/handoff/#hand-off-a-task-from-jira-or-burnlite) keeps its link and its sync here. The worker gets a read-only copy of the task's text (its description, plan and checklist) and your Legato details, never talks to Burnlite, and sends back only the Legato details.

## Settings

| Setting | What it does | Default |
| --- | --- | --- |
| `burnlite.url` | The address that connecting uses when you give none. | none |
| `burnlite.projects` | The projects that a search looks in. | every project |
| `burnlite.sync_interval_seconds` | Seconds between checks. | `60` |
| `burnlite.status_sync_enabled` | Whether a change of state moves the card. | `true` |
| `burnlite.columns.ready`, `.blocked`, `.claimed`, `.closed` | The column of each state. | `Ready`, `Backlog`, `Doing`, `Done` |

See the [configuration reference](/legato/reference/configuration/) for every setting.

## Close a ticket from a workflow

A [workflow](/legato/using/workflows/) step closes the Burnlite ticket a card was imported from:

```yaml
- id: close-ticket
  title: Close the ticket
  type: burnlite.task.close
  with:
    task: ${{ workflow.inputs.task }}
    summary: Merged in ${{ steps.merged.outputs.url }}
    optional: true
```

`reason` is `completed` (the default) or `not_planned`. `summary` is one line, cut at 200 characters. With `optional: true`, a card that is not a Burnlite ticket is skipped; without it, the step fails. The card follows at the next sync.

## Create a ticket or add a Note from a workflow

A `ticket.create` step creates a Burnlite task, and a `ticket.comment` step adds a Note to one. Both use Legato's connection, which must be able to write (see above), and record `legato` as the holder.

```yaml
- id: raise
  title: File the task
  type: ticket.create
  with:
    provider: burnlite
    project: LEG
    summary: Fix login for SSO users
    description: Seen in #support today.
    labels: [bug]
    size: Medium
    uncertainty: Low
- id: seen-again
  title: Note it was seen again
  type: ticket.comment
  with:
    provider: burnlite
    key: ${{ steps.raise.outputs.key }}
    body: Seen again in prod today.
```

A Work task (`kind: work`, the default) needs a `size` (`Small`, `Medium`, `Large` or `X-Large`) and an `uncertainty` (`Low`, `Medium`, `High` or `Extreme`). A Meta task (`kind: meta`) takes neither. `milestone` adds the task to a Milestone, by its key. `type`, `priority` and `fields` are for Jira, and the step fails if you set them. When Jira is not set up, you can leave `provider` empty. `assignee: me` claims the new task as `legato` after the step creates it. If the claim fails, the task is still created, and the step reports the problem in `warnings`. See [Workflows](/legato/using/workflows/) for both steps.

## Claim a task or change its stage from a workflow

A `ticket.update` step claims a task, changes its stage, or both:

```yaml
- id: take
  title: Take the task
  type: ticket.update
  with:
    provider: burnlite
    key: LEG-42
    assignee: me
    status: building
```

- Burnlite has no users, only claims by a holder's name. `assignee: me` claims the task as `legato`, the holder Legato writes every ticket as. `assignee: none` releases the claim of whoever holds it. Other values fail the step.
- A claim made this way holds the task under `legato`. An agent that later claims the task under its own name gets `already_claimed`. Leave the claim to the agent when the agent claims its own tasks.
- `status` is a stage: `idea`, `exploring`, `ready`, `building`, `done` (or `closed`) or `dropped`. `done` closes the task as completed, and `dropped` closes it as not planned. A closed task is not reopened: moving it to an open stage fails. Any other status fails, and the step lists the stages.
- A task already claimed by `legato`, or already at that stage, is left alone. With `optional: true`, a missing task, a claim another holder has, or a status the task cannot take becomes a warning instead of a failure.
