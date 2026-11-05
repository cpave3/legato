---
title: Hand off tasks to a worker
description: Send a task from your main instance's web board to a worker instance, which starts an agent on it, and see the task away until it comes back.
sidebar:
  order: 35
---

:::caution[Partly built]
You can hand a task off from the web board, follow it while it is away, and return or recall it, as below. Taking a task back without its results is planned: it does not work in a release yet, and the details can change.
:::

A handoff sends one of your tasks to a [worker instance](/legato/running/instances/): another Legato, such as an always-on VM, that works on it while your laptop sleeps or travels. The worker makes a card for the task and starts an agent on it at once. Your own board keeps the task, read-only, in an **Away** lane until it comes back.

Before you can hand anything off, pair a worker with this Legato. See [Main and worker instances](/legato/running/instances/#pair-a-main-instance-with-a-worker). Until a worker is paired, Settings › Handoff › Workers explains handoff and the board has no Away lane.

## Hand off a task

1. On the web board, drag the task's card onto the **Away** lane, or onto its folded rail. You can also open the task's menu (choose `⋯` on its card, or right-click the card) and choose **Hand off…**, or select the card and choose **Hand off…** in the panel beside the board.
2. The **Hand off** form opens for that task. Dropping a card only opens the form: nothing is handed off until you send it.
3. Fill in the form:
   - **Worker**: the worker that takes the task. A worker without Legato's [sandbox](/legato/security/sandbox/) is listed but cannot be chosen: it refuses every handoff.
   - **Purpose**: **Make a change** (the agent pushes its branches and opens draft pull requests) or **Find out** (the agent answers in the task's notes and pushes nothing).
   - **Start from**: **A new branch**, named `legato/<task ID>`, or **A pushed branch**: work you began and pushed to GitHub. For a pushed branch, type its name in **Branch**. See [Start from a pushed branch](#start-from-a-pushed-branch).
   - **Repository**: one of the repositories in Settings › Agents that has a GitHub name. With none, type its `owner/name`.
   - **Agent tool**: the tool the worker starts. This Legato's default comes first.
4. Choose **Hand off**.

The task's title, description and notes travel to the worker, with the repository and the branch. Your agent's conversation never does.

The worker starts the agent in its sandbox, with its tool's own permission prompts off, and a brief that tells it to work alone, record its decisions in notes, and move the card to Done when it is through. See [On the worker](/legato/running/instances/#on-the-worker).

### Start from a pushed branch

Choose **A pushed branch** to hand off work you have already begun. Push the branch to GitHub first: Legato never pushes for you.

The worker fetches its copy of the repository before it starts, then looks for the branch there. If GitHub does not have it, the handoff fails with "branch not on GitHub", and the Away lane says so. The worker never makes a new, empty branch of that name instead. The repository's default branch fails too: hand off a branch of your own.

The agent works on your branch. Its brief tells it that the branch already has commits: read them against the default branch first, then only add commits. It never rebases, amends or force-pushes the branch, and it reuses the branch's pull request if it has one. Any new branches it stacks on top of yours are named `legato/<task ID>-<a few words>`. To make a change, it may push your branch and its own, and open draft pull requests, without asking.

### Hand off a Jira task

A task imported from Jira hands off like any other. Your main keeps its link to Jira and goes on syncing it while the task is away, and after it comes back. The worker never talks to Jira and needs no Jira access.

On the worker, the card shows the task in two parts: **From Jira ACME-12 (read only)**, a copy of the ticket's text taken when you handed it off, then **Legato details**, the part you wrote in Legato. Only the Legato details can be edited there; the title is read-only too. The agent's brief gives it the same two parts. When the task comes back, only its Legato details come back: its title and its text stay Jira's.

### When a handoff is refused

The form says why, in the words of the main or the worker, and nothing is handed off:

- **The task's agent runs here.** Stop it first: two agents never work on one task. The form says so before you send anything.
- **The worker did not answer.** Nothing was sent. Handing the task to the same worker again is safe: if the worker did get it, it does not make a second card.
- **The worker has no sandbox**, or **does not know the agent tool** you chose. Choose another worker or tool.
- **The worker cannot clone the repository**, for example because its GitHub credential cannot read it.
- **Update the worker**: the worker runs a Legato too old to take a handoff. A worker that cannot yet start from a pushed branch says so.
- **No branch name**: starting from a pushed branch needs the branch's name.

## The Away lane

Once a worker is paired, the web board has an **Away** lane after its columns. A task you hand off leaves its column for the lane. While nothing is away, the lane is folded to a narrow rail, as an empty column is; a card dropped on the rail still opens the Hand off form, and the lane opens again as soon as a task is away. Click the rail, or press `z`, to open the empty columns and the lane. The lane has a group for each worker, with the worker's name, whether your main can reach it, and **Open**, which opens the worker's own web UI. A worker with nothing away says so.

The group's header says "pulled 2 min ago" while your main hears from the worker, "unreachable since 21:40" once it has been down for 30 seconds, or "no longer accepts this main; pair again" when the worker unpaired your main (pair it again from Settings › Handoff › Workers).

Each row in a group shows:

- the task's title and ID;
- "from Doing": the column the task left;
- its state, which changes as the worker works: **Starting** while the worker clones the repository and starts the agent; then what its agent is doing, **Working**, **Needs you** (it asks you something on the worker), **Idle** or **Agent ended**; **Done** once the card is in the worker's Done column; or **Failed** with the reason. Your main shows **Unreachable** while the worker is down, **Lost on worker** when the worker no longer has the task, and **Not accepted** when it no longer accepts your main. Hover the pill for why;
- when you handed it off;
- its pull requests, once it has any: "PR #12 · draft", or "3 PRs · 2 draft · 1 ready". The line is red, and says what blocks it, when a pull request that is ready for review failed CI or has changes requested.

Select a row, as you select a card, to see the task in the panel beside the board. The panel shows the state, the worker, the purpose, where the work started, the branch, the repository, the column it left, its pull requests, and **Open on <worker>**, which opens the task on the worker's board. The description is marked read-only while away.

## Pull requests while away

Your main follows an away task's pull requests itself, with its own GitHub access, as it does for every task (see [GitHub](/legato/integrations/github/)). The worker tells your main which branches its agent pushed, in stack order. When a new branch shows up, your main looks for its pull request at once; after that, the pull request is refreshed with the others on every poll.

The task panel lists each pull request in stack order: its number (which opens it on GitHub), whether it is a draft or ready for review, its checks and its review. A branch whose pull request your main has not found yet says "no PR yet".

Attention treats them as it treats any pull request: a draft raises nothing, and a pull request marked ready that failed CI or has changes requested is a merge blocker. Its **Open task** selects the task in the Away lane.

When the task comes back, its pull requests stay as they are: your main already tracks them.

## Bring a task home

When the worker is done with a task, **return** it. Its row in the Away lane, and its panel, then offer **Return**; each worker's group offers **Return all done (N)**, which returns every task done there in one go. A task comes back with:

- its title and description as they are on the worker, if they were edited there (for a Jira task, its Legato details only);
- the notes written on the worker, headed "From the worker" (your own notes stay as they were, and none comes back twice);
- the repository, and the branches its agent pushed, in the order of the Staccato stack it built, with their pull requests.

It lands in your column with the same name as its column on the worker (usually Done), or else in the column it left. It is no longer away: you can edit it, move it and start an agent on it again. On the worker, its agent stops and its card is archived; the worker's copy of the repository is cleaned up later.

To bring a task back before it is done, **recall** it. Recall is offered in any state, and is the only way back for a handoff that **Failed**: fix the cause, then hand the task off again. A recall brings back whatever exists so far and adds a note naming the worker and the state, for example "Recalled from vm-1, where its agent was working." When the task's agent may still be at work, the board asks you to confirm first, since recalling stops it.

Return and recall are safe to repeat. If the connection drops halfway, your main finishes the return by itself the next time it hears from the worker, and nothing comes back twice. A return to a worker that does not answer fails on the spot and marks the worker unreachable; the task stays away, and trying again later is safe. A task the worker lost cannot be returned or recalled.

A task you hand off again after it came back is a new handoff, with a new card on the worker.

## Staying up to date

Your main keeps a connection open to each worker that holds tasks of yours, and hears of each change as it happens: you never refresh by hand. When your laptop wakes or the network comes back, it reconnects by itself, waiting a little longer after each failed try (up to a minute), and catches up on everything at once. A short drop, such as a Wi-Fi switch or the worker updating, shows nothing; the tasks are marked unreachable only after 30 seconds. A handoff to a worker that does not answer marks it unreachable at once.

**Sync providers** on the web board (or `r` in the TUI) also reconnects to every worker and pulls its tasks.

Your main never takes a task back by itself. A task the worker lost, or one on a worker that is unreachable or no longer accepts your main, stays in the lane until you act on it.

Settings › Handoff › Workers shows when each worker was last pulled. On the worker, Settings › Handoff › Main instances shows when each main last pulled and how many of its handoffs are open there.

## While a task is away

The task changes only on the worker until it comes back. On your main, you cannot edit it, move it, delete it, start an agent on it or hand it off again: the web UI hides those controls, and the TUI and the `legato` command refuse them and name the worker. You can still add notes; they stay on your main.

To talk to the task's agent, open the task on the worker with **Open on <worker>**, in its web UI or the phone app.

The TUI shows away tasks in their columns.

## On a phone

On a phone, the board has an **Away** section above its columns while any task is away. Each worker that holds away tasks has one line, such as "vm-1 · 6 away · 2 done", with **Return all done (N)** when some are done. Tap the line to see its tasks: each row shows what it shows in the Away lane, with the same **Return** and **Recall**. Tap a row to open the task's details. Tap the line again to fold it.
