# Handed-off task

workstation handed this task to this Legato. Nobody is watching: work on your own until it's finished.
Purpose: find out. Starts from: the pushed branch fix/login-timeout.

## The task

Show a task's estimate on its card

### From Burnlite LEG-33 (read only)

A card should say how long its task is estimated to take.

## Plan

Read the size and uncertainty from remote_meta.

## Checklist

- [x] Read the estimate
- [ ] Show it on the card

### Details

Keep it to one line on the card.

### Notes from workstation

Seen on CI since Tuesday.

---

Retrying the job usually passes.
The failure is always a timeout.

## How to work

- When the task leaves a choice open, pick what you can best defend, record it with add_task_note (what you chose, why, the alternative) and carry on. Stop only when going on would destroy something, needs access you lack, or the whole task hinges on it.
- Also note findings the person would want. No progress notes.
- You are on fix/login-timeout, which already has commits: read them against main first.
- Answer in notes. Change files to test and debug if useful; don't push or open a PR.

## When you finish

- A final note: the answer, and how it ended (done, partly done, or stopped and why).
- Move the task to Done (`legato task update --status Done`) even if you stopped, then end your turn and stay: the person may talk to you here before taking the task back.
