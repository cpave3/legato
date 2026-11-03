---
title: Yggdrasil, worktrees and fresh clones
description: Give each task a Git worktree of its own with Yggdrasil, and say how a fresh checkout is made ready for an agent.
sidebar:
  order: 40
---

When several agents work in one repository at once, each needs a checkout of its own. Legato can make a Git worktree for a task with Yggdrasil (`yg`), remember it, and start the task's agents there. It can also clone a repository afresh for an agent. Either way, a repository can say how a new checkout is made ready: which dependencies to install and which local files to copy in.

## Task worktrees

### Turn them on

1. Install Yggdrasil and make sure `yg` is on your `PATH`.
2. Turn worktrees on in `config.yaml`:

   ```yaml
   worktrees:
     yggdrasil:
       enabled: true
   ```

3. Restart Legato.

### Make a worktree for a task

1. On the board, select the task and press `b`.
2. Check the three fields: **Primary directory** (your main checkout of the repository), **Branch** and **Base branch**. Press `tab` to move between them.
3. Press `enter`.

Legato runs `yg new` in the primary directory and records the worktree's path, branch and base branch on the task. When you start an agent for the task, Legato offers that path first. The worktree also tells Legato the task's repository and branch, which it uses to [find the pull request](/legato/integrations/github/).

In the web UI, the task detail has **Create worktree**, which does the same, with your repository presets to pick the primary directory from.

A task has one worktree at a time.

### Worktrees you make yourself

If you make worktrees with `yg` outside Legato, install Legato's Yggdrasil hook so that Legato still records them. Run this in the repository that holds `.yggdrasil.toml`:

```bash
legato hooks install --tool yggdrasil
```

This adds one command to `post_create` under `[hooks]` in `.yggdrasil.toml` and keeps the hooks you already have. The command runs `legato task worktree set` with Yggdrasil's details of the new worktree. It does nothing when `legato` is not on `PATH`, or when `LEGATO_TASK_ID` is not set, so a worktree you make outside an agent's session is not recorded.

To remove only Legato's command:

```bash
legato hooks uninstall --tool yggdrasil
```

You can also record or clear a worktree by hand:

```bash
legato task worktree set <task-id> --path /work/task --primary-dir ~/Projects/app --branch task-123 --base-branch main
legato task worktree clear <task-id>
```

## Setting up a fresh checkout

A new clone has no installed dependencies and none of your untracked local files, such as `.env.local`. A repository can say how to make one ready, in a `.legato.toml` file at its root:

```toml
[setup]
run = ["cd apps && bun install", "task generate"] # run in order with sh -c
copy = [".env.local", "apps/*/.env.local"]         # copied from your main checkout
link = ["local.settings.json"]                     # linked to your main checkout's file
```

- `run`: commands to run in the new clone, in order. They stop at the first one that fails.
- `copy`: files to copy from the repository's `primary_dir`. Patterns such as `apps/*/.env.local` work.
- `link`: files to link to the same file in `primary_dir`.

A path to copy or link must be inside the repository. Legato reads `.legato.toml` strictly, and an unknown key is an error.

Without a `.legato.toml`, Legato reads the repository's `.yggdrasil.toml` instead: its `[hooks] post_create` commands are the commands to run, and its `[provision]` `copy` and `link` are the files. Legato leaves Yggdrasil's other settings alone. So a repository already set up for Yggdrasil needs nothing more.

### When it runs

Legato makes a checkout ready only when it has just cloned the repository for an agent. Joining a clone that exists, reloading an agent and resuming one run nothing.

1. Legato copies and links the files from your main checkout. Without a `primary_dir`, as on a server, it copies nothing.
2. Legato runs the commands in the agent's terminal, in the agent's [sandbox](/legato/security/sandbox/) when it has one, before the agent tool starts.
3. The agent tool starts, whether the setup worked or not. When it failed, Legato adds a note to the task and tells the agent where the log is.

The output goes to the terminal and to `.git/legato-setup.log` in the clone. The commands get Yggdrasil's `YG_*` variables, so a `.yggdrasil.toml`'s hooks work unchanged. They also get `LEGATO_CLONE=1`; a hook that makes sense only in a Yggdrasil worktree can test it and skip itself.

To run the setup again by hand in a clone, run this in its folder:

```bash
legato clone setup
```

`--base <ref>` names the ref the clone was made from (the default branch when you leave it out). `--primary <path>` names the main checkout.

## Fresh clones from the repository cache

Instead of a worktree of your own checkout, an agent can start in a fresh clone that Legato makes. Legato keeps a bare copy of each GitHub repository under its data directory and makes each clone from it, on a branch of the agent's own. It needs a `github` credential that lists the repository; see [Setting up credentials](/legato/security/credentials-setup/).

```yaml
repository_cache:
  enabled: true   # offer "Clone" in the web UI's spawn dialogs
  max_age: 5m     # fetch first when the last fetch is older than this
  keep_idle: 7d   # how long to keep a clone no agent works in; 0 keeps them
```

With `repository_cache.enabled`, the web UI's spawn dialogs have a **Folder | Clone** switch under **Where**. **Clone** offers a new clone of each repository in your list that names a `repo`, and the clones agents already worked in. The web UI's **Remote** page lists the clones and removes them. When no agent has worked in a clone for `repository_cache.keep_idle`, Legato removes it. A clone with commits that the cache does not have is kept seven days more, with a note on its task.

:::caution
Legato does not check a clone for uncommitted changes before it removes it.
:::

This is how agents work on a server with no checkouts of yours. See [Agent server](/legato/running/agent-server/).
