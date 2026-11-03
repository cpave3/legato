---
title: Staccato
description: Link a task to its branch the moment Staccato opens a pull request from an agent's session.
sidebar:
  order: 30
---

Staccato (`st`) manages stacks of Git branches and opens their pull requests. With Legato's Staccato hook installed, a pull request you open with Staccato from inside an agent's session is linked to that agent's task straight away. You don't need to press `p` or wait for Legato to find it.

The hook is optional. Without it, Legato still finds the pull request from the task's branch or commit; see [GitHub and pull requests](/legato/integrations/github/).

## Install the hook

```bash
legato hooks install --tool staccato
```

This writes one script, `legato-pr-link.sh`, into two of Staccato's hook folders:

- `~/.config/staccato/hooks/post-pr-create/`
- `~/.config/staccato/hooks/post-pr-view/`

The folders are under `$XDG_CONFIG_HOME` when you set it. The hooks are global, so you can run the command from any directory. The script calls the `legato` binary that installed it, by its full path, so install the hook again if you move the binary.

To remove the hook:

```bash
legato hooks uninstall --tool staccato
```

## What the hook does

When Staccato opens or views a pull request, it runs the script. The script:

1. Does nothing unless it runs in a session Legato started, where `LEGATO_TASK_ID` is set.
2. Reads the GitHub `owner/repo` from the repository's `origin` remote. It does nothing for a remote that is not on GitHub.
3. Reads the head commit with `git rev-parse HEAD`.
4. Runs `legato task link` with the task, the branch, the repository and the commit.

Staccato may run the hook when it opens the browser on the new pull request page, before the pull request exists. That is fine: the link records the branch and the commit, and Legato looks for the pull request at once and on every poll after that, until it finds it.
