---
title: Jira
description: Import Jira tickets onto the board, keep them up to date, and move their Jira status when you move the card.
sidebar:
  order: 10
---

Legato can track Jira tickets as cards on its board. You import the tickets you want to work on, and Legato keeps their title, description, priority and status up to date. When you move a card, Legato moves the ticket in Jira too. Everything stays in Legato's local database, so the board keeps working when Jira can't be reached.

Without Jira settings, Legato is a local board, and nothing on this page applies. Legato can also track tasks from [Burnlite](/legato/integrations/burnlite/), on the same board.

## Set it up

You need your Jira site's address, your email address and a Jira API token. Legato talks to Jira's REST API version 3, which signs in with an email and an API token.

1. Make an API token at <https://id.atlassian.com/manage-profile/security/api-tokens>.
2. Put the token in an environment variable in your shell profile (`~/.bashrc`, `~/.zshrc`):

   ```bash
   export LEGATO_JIRA_TOKEN=your-api-token-here
   ```

3. Add a `jira` block to `~/.config/legato/config.yaml`:

   ```yaml
   jira:
     base_url: https://yourcompany.atlassian.net
     email: you@company.com
     api_token: ${LEGATO_JIRA_TOKEN}
     project_keys:
       - PROJ
     jql_filter: ""              # optional: more JQL for the refresh
     sync_interval_seconds: 60   # how often to pull from Jira
     status_sync_enabled: true   # false: refresh tickets, never change statuses
   ```

4. Restart Legato.

Legato expands `${LEGATO_JIRA_TOKEN}` when it loads the file, so the token itself is never in the config file. Jira is on only when `jira.base_url`, `jira.email` and `jira.api_token` are all set.

| Setting | What it does | Default |
| --- | --- | --- |
| `jira.base_url` | Your Jira site. | none |
| `jira.email` | The account Legato signs in as. | none |
| `jira.api_token` | That account's API token. | none |
| `jira.project_keys` | The projects that ticket search covers. | none |
| `jira.jql_filter` | JQL that Legato adds to the query that refreshes your tracked tickets. | empty |
| `jira.sync_interval_seconds` | Seconds between pulls. | `60` |
| `jira.status_sync_enabled` | Whether Jira statuses and board columns follow each other. | `true` |
| `jira.attachment_max_size_bytes` | The largest image attachment Legato downloads. | 25 MiB |

See the [configuration reference](/legato/reference/configuration/) for every setting.

### The setup wizard

The first time Legato starts with an empty database, it runs a short setup wizard in the terminal. The wizard makes the default columns (Backlog, Ready, Doing, Review, Done) and asks whether to configure Jira. If you answer yes, it:

1. Asks for the base URL, your email and the API token, and checks them against Jira.
2. Lists your projects and lets you pick one.
3. Reads that project's statuses and maps them to columns by name: Backlog, Doing, Review, Done, and Unmapped for any status it does not know.
4. Makes the board's columns from that mapping, in Legato's database.
5. Saves Jira's settings in the database (`jira.base_url`, `jira.email`, `jira.project_keys`), and the token you typed in Legato's own secret store as `jira-api-token`, which `jira.api_token` names (`legato://jira-api-token`).

Nothing is written to `config.yaml`, so Settings can change Jira later. A `jira` section you write in `config.yaml` yourself still wins.

## Map columns to Jira statuses

Each column in `board.columns` lists the Jira statuses that belong in it. A column can also name the Jira transition that moves a ticket into it:

```yaml
board:
  columns:
    - name: Backlog
      remote_statuses: ["To Do", "Open", "Backlog"]
    - name: Ready
      remote_statuses: ["Ready for Dev", "Selected for Development"]
    - name: Doing
      remote_statuses: ["In Progress", "In Development"]
      remote_transition_id: "21"   # the Jira transition into this status
    - name: Review
      remote_statuses: ["In Review"]
    - name: Done
      remote_statuses: ["Done", "Closed"]
```

- `remote_statuses`: a ticket whose Jira status is in this list goes in this column.
- `remote_transition_id`: when you move a card into this column, Legato runs this transition on the ticket. A column without one moves the card in Legato only.
- A column with no `remote_statuses` is local only. A ticket in that column stays there whatever its Jira status does.

Jira's transition IDs depend on your project's workflow. To see the transitions a ticket has, ask Jira for them:

```bash
curl -u you@company.com:$LEGATO_JIRA_TOKEN \
  https://yourcompany.atlassian.net/rest/api/3/issue/PROJ-123/transitions
```

:::note
Legato reads `board.columns` into its database only on the first run. After you change the columns, run `legato columns sync` to apply them. Tasks in a column you removed move to `Unmapped` when you have that column, and to the first column when you do not.
:::

## Import tickets

Legato never adds tickets on its own. You choose each ticket:

1. On the board, press `i`.
2. Type at least two characters of a summary or a ticket key. Legato searches the projects in `jira.project_keys` and lists the most recently updated tickets first.
3. Use `j` and `k` to pick a ticket, and press `enter` to import it.

The ticket goes into the column that its Jira status maps to, or Backlog when no column has that status. If you are looking at one workspace, the task joins that workspace. The web UI's board has the same import on `i`.

To choose a workspace for every import, set `default_import_workspace` to the name of one of your `workspaces`.

## How sync works

Every `jira.sync_interval_seconds`, Legato pulls the tickets you track. It updates the title, description, priority, type, assignee, labels and epic of each ticket that changed in Jira. Press `r` on the board to sync now.

- **Status.** When a ticket's Jira status maps to another column, Legato moves the card.
- **Your moves win for five minutes.** After you move a card, Legato keeps your column for five minutes, even if Jira still shows the old status. After that, the Jira status wins on the next pull.
- **Pushing moves.** When you move a card into a column with a `remote_transition_id`, Legato moves the card at once and runs the transition in the background.
- **Failed pushes.** When a transition fails, the card keeps its new column and shows a warning mark. Legato tries the transition again on the next sync.
- **Offline.** When Jira can't be reached, the board works from the local database and the status bar says why the sync failed. Legato waits and tries again when Jira asks it to slow down.
- **Archived tasks** are not refreshed.

### Turn status sync off

Set `jira.status_sync_enabled: false` to keep pulling ticket details without changing any status:

- Cards stay in their Legato columns when the Jira status changes.
- Moving a card does not transition its Jira ticket.
- A ticket you import still starts in the column its current status maps to.

## Descriptions

A Jira task has two descriptions:

- **Jira's description.** Legato converts it to Markdown and replaces it on every sync. You can't edit it in Legato.
- **Legato details.** Your own notes on the task. Jira never sees them, and sync never changes them.

The task detail view shows Jira's description first, then your notes under a "Legato details" heading. Press `e` in the detail view to edit your notes in your editor. A Jira task's title comes from Jira, so `t` does not rename it. Press `o` to open the ticket in your browser.

A Jira task you [hand off to a worker](/legato/using/handoff/#hand-off-a-task-from-jira-or-burnlite) keeps its link and its sync here. The worker gets a read-only copy of Jira's description and your Legato details, never talks to Jira, and sends back only the Legato details.

## Image attachments

Legato downloads each image attached to a tracked ticket when you import it and on every sync. It removes the copies of images that are no longer on the ticket, and the folder of a task you archive. It does not download other attachments, or images larger than `jira.attachment_max_size_bytes`.

The copies are in the `attachments` folder of Legato's data directory. `legato task show` prints their paths, so an agent working on the task can read them.
