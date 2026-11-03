---
title: Staccato
description: Let agents build stacks of branches with Staccato, take a finished stack into your own checkout, and link pull requests to tasks.
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

## Stacks built by agents

When a change is easier to review as several small pull requests, an agent can build it as a stack with Staccato: each branch stacked on the one before, each with its own pull request. Legato tells agents how when the `st` on its machine can move a stack between clones (`st graph import` exists). It does on Staccato v0.1.0 and later.

- **Branches.** The agent names every branch of the stack `legato/<task>-<a few words>`.
- **One approval.** With Legato's [GitHub credential](/legato/security/credential-broker/), the agent asks once, before its first push, for every branch named that way. You approve the whole stack once, and the approval covers no other task's branches.
- **Pushing.** The agent pushes with `st sync`. A push that waits for your approval makes `st sync` fail with `pending`, and the agent runs it again after you decide.
- **Pull requests.** The agent opens them with `st pr`.

## Taking a stack home

Legato records each stack when the agent ends, and again before its clone is removed, so it is still there after the clone is gone. Open the task in the web UI: its activity has a **Stacks** tab with the branches, which ones are pushed, and, once every branch is pushed, **Download stack**.

Then, in your own checkout of the repository:

```bash
st graph import ~/Downloads/<task>-<repository>-stack.json
```

`st` fetches the branches and adds the stack to your checkout's graph, so you can carry on with it there: restack, add a branch, sync. A branch you have changed yourself is left as it is and reported. See `st graph import --help`.

Legato reads the stack from the clone's files and never runs `st` or `git` in an agent's clone.

## Keeping `st` up to date

On a server that runs a release of `st` (for example one installed as shown in [Agents on a server of their own](/legato/running/agent-server/)), Legato keeps it current with its own updates, in the same mode: **Automatic** runs `st update` as soon as a release is out, **Ask** shows it in Settings › Updates and in Attention, **Off** never looks. An `st` built from source is left alone. See [Releases and updates](/legato/running/updates/).

