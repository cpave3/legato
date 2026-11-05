---
title: Main and worker instances
description: Pair the Legato you work from with another Legato, which will work on the tasks you hand it.
sidebar:
  order: 50
---

:::caution[Partly built]
You can pair a main instance with a worker, as below, and [hand a task off](/legato/using/handoff/) from the main's web board. Bringing a task back (return, recall, take back) is planned: it does not work in a release yet, and the details can change. Until then, to run agents on another machine and keep their results, use [a server of its own](/legato/running/agent-server/) as a second server in the web UI and the phone app.
:::

A person can run more than one Legato. Main and worker instances let one Legato send a task to another, which works on it and sends the result back. With them, you can keep your board on your laptop and run the agents on an always-on machine, without moving the task by hand.

## The two roles

- **Main instance**: a Legato that you work from. It owns its tasks. You can have more than one, for example a workstation and a laptop.
- **Worker instance**: a Legato that works on tasks that main instances hand to it. One worker can serve several main instances.

Each Legato has an **instance ID**. Legato makes it once and never changes it. Other instances know a Legato by its ID, not by its name.

## Pair a main instance with a worker

You pair a main instance with a worker once, by four words, as you pair a phone. The main gets a token that can only hand tasks off, check on them and bring them back. It never gets the worker's own web token, so a stolen main cannot drive the worker's web UI.

1. On the worker, open Settings › Handoff › Main instances and choose **Pair a main instance**. Or run:

   ```bash
   legato pair --main
   ```

   It shows the worker's address, four words and how long they last. The worker's web server must be running.
2. On the main, open Settings › Handoff › Workers and choose **Add a worker**.
3. Enter the worker's address, the four words, and this main's name on the worker. The name starts as the main's host name; the worker keeps it in lowercase letters, digits and dashes.
4. Choose **Pair**.

Both sides then list each other. The worker lists the main under Main instances, with its instance ID, when it paired, when it last pulled and its open handoffs. The main lists the worker under Workers, with its address, its instance ID, when it last pulled and whether its sandbox is on. A worker without the sandbox refuses handoffs.

The words work once, for five minutes and five tries, like a phone's. A phone cannot use a main's words, and a main cannot use a phone's.

- **Pairing again** with the same main replaces its token. Renaming the main this way keeps it the same main: the worker knows it by its instance ID.
- **Another main under the same name** is refused. Unpair the first one, or choose another name.
- **An old worker** that cannot pair a main makes the main say "update the worker" before anything is sent.
- **Unpair** under Main instances on the worker revokes the main's token at once.

## The life of a handed-off task

```text
 main instance                         worker instance
 ─────────────                         ───────────────
 task ── handoff ───────────────────▶  works on the task
 (away)                                      │
   ◀──────────────────────────────── return  │  with what the worker did
   or: recall ──────────────────────▶        │  take the task back early
   or: take back                             │  end it without the worker
```

1. **Handoff.** The main instance sends one of its tasks to a worker instance. See [Hand off tasks to a worker](/legato/using/handoff/).
2. **Away.** While the worker holds the task, the task is "away" on the main instance.
3. The task then comes back in one of three ways:
   - **Return.** The worker finishes, and the task comes back with what the worker did.
   - **Recall.** The main instance takes the task back before the worker finishes.
   - **Take back.** The main instance ends the away task by itself, without the worker's results. It does this when the worker cannot be reached or no longer has the task.

## On the worker

When a main hands a task to a worker, the worker:

- makes an ordinary card for it in a workspace named after the main, in the Doing column (or In Progress, else the first column). The card says where it came from under its title: "from workstation · K7Q2M9XA", the main's name and its own ID for the task;
- adds the main's notes to the card, marked as from the main;
- starts an agent on the card at once, in a fresh clone of the task's repository on the branch `legato/<the main's task ID>`, or on the pushed branch the task starts from, which the worker first fetches from GitHub. The agent always runs in Legato's [sandbox](/legato/security/sandbox/), with its tool's own permission prompts off, and with a brief that tells it to work alone, record its decisions in notes, open draft pull requests only (or, to find something out, answer in notes and push nothing) and move the card to Done when it is through.

The worker refuses a handoff before it makes anything when it has no sandbox, does not know the agent tool asked for, or cannot clone the repository. If the agent then fails to start, for example because the clone failed, the handoff is marked failed with the reason. A pushed branch that GitHub does not have fails the handoff with "branch not on GitHub". A handoff accepted just before the worker stopped starts when the worker starts again.

On the worker, a handed-off card works like any other: you can move it, edit it, add notes and talk to its agent. But you cannot archive or delete it there: the worker says "this task belongs to workstation; return or recall it from there", and archiving the Done column's cards leaves it there. The task goes back to its main.

A card for a task from Jira shows Jira's text read-only, as a copy taken at handoff, then the task's Legato details, which are the one part you can edit there; its title is read-only too. The worker never syncs it with Jira. See [Hand off a Jira task](/legato/using/handoff/#hand-off-a-jira-task).

On the main, the task is **away** while the worker holds it: the main refuses to edit it, move it, start an agent on it or hand it off again, from the web UI, the TUI and the `legato` command, and says which worker has it. Notes can still be added there.

## Not to be confused with

- A **review handoff** is the packet of decisions and evidence that an agent writes for a review. See [Review handoff](/legato/using/review-handoff/).
- A **delegation** is a request from [Argus](/legato/integrations/argus/) that a Legato fix or find out something about a case.
- A **remote ticket** is a task from a ticket provider such as Jira, not a task on a worker instance.

The [glossary](/legato/reference/glossary/) has these terms and the words to avoid.
