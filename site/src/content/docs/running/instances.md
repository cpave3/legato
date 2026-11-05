---
title: Main and worker instances
description: Pair the Legato you work from with another Legato, which will work on the tasks you hand it.
sidebar:
  order: 50
---

:::caution[Partly built]
You can pair a main instance with a worker, as below. Handing tasks off is planned: it does not work in a release yet, and the details can change. Today, to run agents on another machine, use [a server of its own](/legato/running/agent-server/) as a second server in the web UI and the phone app.
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

1. **Handoff.** The main instance sends one of its tasks to a worker instance.
2. **Away.** While the worker holds the task, the task is "away" on the main instance.
3. The task then comes back in one of three ways:
   - **Return.** The worker finishes, and the task comes back with what the worker did.
   - **Recall.** The main instance takes the task back before the worker finishes.
   - **Take back.** The main instance ends the away task by itself, without the worker's results. It does this when the worker cannot be reached or no longer has the task.

## Not to be confused with

- A **review handoff** is the packet of decisions and evidence that an agent writes for a review. See [Review handoff](/legato/using/review-handoff/).
- A **delegation** is a request from [Argus](/legato/integrations/argus/) that a Legato fix or find out something about a case.
- A **remote ticket** is a task from a ticket provider such as Jira, not a task on a worker instance.

The [glossary](/legato/reference/glossary/) has these terms and the words to avoid.
