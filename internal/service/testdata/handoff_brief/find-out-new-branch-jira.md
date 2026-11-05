# Handed-off task

workstation handed this task to this Legato. Nobody is watching: work on your own until it's finished.
Purpose: find out. Starts from: a new branch.

## The task

Checkout times out on large carts

### From Jira ACME-12 (read only)

Carts over 50 items time out at checkout.

Acceptance: checkout under 2s.

### Details

Start with the cart serializer.

### Notes from workstation

Seen on CI since Tuesday.

---

Retrying the job usually passes.
The failure is always a timeout.

## How to work

- When the task leaves a choice open, pick what you can best defend, record it with add_task_note (what you chose, why, the alternative) and carry on. Stop only when going on would destroy something, needs access you lack, or the whole task hinges on it.
- Also note findings the person would want. No progress notes.
- Answer in notes. Change files to test and debug if useful; don't push or open a PR.

## When you finish

- A final note: the answer, and how it ended (done, partly done, or stopped and why).
- Move the task to Done (`legato task update --status Done`) even if you stopped, then end your turn and stay: the person may talk to you here before taking the task back.
