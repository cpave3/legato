---
title: GitHub and pull requests
description: See each task's pull request, with its CI, reviews and comments, on the board.
sidebar:
  order: 20
---

Legato finds the GitHub pull request for each task and shows its state on the card: whether CI passes, whether the review approved it or asked for changes, and how many comments it has. It works for local tasks and Jira tasks alike. Legato only reads from GitHub. It never comments, approves, merges or pushes.

## Set it up

Legato needs a GitHub token that can read your repositories. The easiest way is the GitHub CLI:

```bash
gh auth login
gh auth status
```

With the default settings, Legato runs `gh auth token` once at start and keeps the token in memory. It never writes the token anywhere.

```yaml
github:
  auth: auto        # auto, gh or token
  # token: ${LEGATO_GITHUB_TOKEN}
  poll_interval_seconds: 600
  resolved_poll_interval_seconds: 600
```

| Setting | What it does | Default |
| --- | --- | --- |
| `github.auth` | `auto` uses `github.token` when it is set and `gh auth token` when it is not. `gh` always uses the GitHub CLI. `token` always uses `github.token`. | `auto` |
| `github.token` | A token to use instead of the GitHub CLI's. Required with `auth: token`. | none |
| `github.poll_interval_seconds` | Seconds between searches for the pull request of tasks that have none yet. | `600` |
| `github.resolved_poll_interval_seconds` | Seconds between refreshes of pull requests Legato already found. | `600` |

To check that your token can see a private repository, run `gh repo view OWNER/REPO`.

:::note
Your token may be allowed to write. Legato still only reads: it refuses to send any GitHub request that is not a read, before the request leaves your machine.
:::

## How Legato finds a pull request

To find a task's pull request, Legato first needs to know the task's repository, and if it can, its branch and commit. Legato calls this the task's code context. It learns it from, in this order of trust:

1. What you set by hand (`p`, `legato task link`, `legato task github context`). Legato never overwrites it.
2. The task's [Yggdrasil worktree](/legato/integrations/yggdrasil/).
3. The directory you start the task's agent in.
4. Your `repositories` list, when exactly one repository there names the task's Jira project or workspace.
5. A branch whose name holds the task's key, once Legato knows the repository.

Your `repositories` list ties repositories to Jira projects and workspaces:

```yaml
repositories:
  - name: legato
    repo: cpave3/legato
    primary_dir: /home/me/Projects/legato
    base_branch: main
    jira_projects: [LEG]
    workspaces: [Legato]
```

On each poll, Legato fetches each repository's recent pull requests once and matches them to tasks by:

1. The task's exact head commit.
2. The task's exact repository and branch.
3. One branch that contains the task's key or ticket key.

Legato never links by title, or by a default branch. When two pull requests could match, Legato links neither and says the match is ambiguous. A task with no repository is left alone.

## Link a pull request yourself

On the board, press `p`, type the repository as `owner/repo` and the PR number, and press `enter`. Legato shows the PR's title, branch and state. Press `y` to link it. A pull request you link stays linked until you rediscover it.

From a shell, link a branch to a task:

```bash
legato task link <task-id> --branch feature/login --repo owner/repo
```

`--branch` defaults to the current Git branch, and `--sha` records the head commit. To remove the link and the code context, run `legato task unlink <task-id>`. Hooks such as [Staccato's](/legato/integrations/staccato/) use these commands.

To set a task's repository and branch without linking:

```bash
legato task github context <task-id> --repo owner/repo --branch feature/task
```

## Refresh and rediscover

| Action | Board key | Command |
| --- | --- | --- |
| Fetch the linked PR again | `ctrl+r` | `legato task github refresh <task-id>` |
| Forget the PR and match again, keeping the code context | `ctrl+p` | `legato task github rediscover <task-id>` |
| Sync Jira and GitHub now | `r` | |

The web UI's task detail has the same refresh and rediscover buttons, and shows the task's repository and branch.

## What the card shows

A card with a pull request shows its state on one line, such as `✓ CI · ✓ review · 2 comments`:

- **CI**: passing, failing or pending.
- **Review**: approved, or changes requested.
- **Comments**: how many the pull request has.
- A draft shows `draft PR`.

The task detail view shows the repository and branch, and says when Legato found no match, found more than one, or could not reach GitHub.

A pull request that is not a draft and is still open appears in [Attention](/legato/using/attention/) under **Merge blockers** when CI fails, when it needs a review, or when a reviewer asked for changes.

## Agents and GitHub

The token on this page is Legato's own, for reading pull requests. Your agents' GitHub access, for cloning, pushing and opening pull requests, is separate. See [Setting up credentials](/legato/security/credentials-setup/).
