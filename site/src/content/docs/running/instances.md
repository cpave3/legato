---
title: Main and worker instances
description: Planned — hand a task from the Legato you work from to another Legato that does the work.
sidebar:
  order: 50
---

:::caution[Not built yet]
This page describes a planned feature. Nothing here works in a release yet, and the details can change. Today, to run agents on another machine, use [a server of its own](/legato/running/agent-server/) as a second server in the web UI and the phone app.
:::

A person can run more than one Legato. Main and worker instances let one Legato send a task to another, which works on it and sends the result back. With them, you can keep your board on your laptop and run the agents on an always-on machine, without moving the task by hand.

## The two roles

- **Main instance**: a Legato that you work from. It owns its tasks. You can have more than one, for example a workstation and a laptop.
- **Worker instance**: a Legato that works on tasks that main instances hand to it. One worker can serve several main instances.

Each Legato has an **instance ID**. Legato makes it once and never changes it. Other instances know a Legato by its ID, not by its name.

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
