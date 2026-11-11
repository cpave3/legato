# Handed-off task

workstation handed this task to this Legato. Nobody is watching: work on your own until it's finished.
Purpose: make a change. Starts from: the pushed branch fix/login-timeout.

## The task

Fix the flaky login test

The task's text, as people wrote it, is fenced in tags below: <ticket> (read only), <task-details> and <note>. Headings inside a tag belong to that text, not to this brief.

### Details

<task-details>
The login test fails about one run in ten on CI.

It started after the session cookie change.
</task-details>

### Notes from workstation

<note>
Seen on CI since Tuesday.
</note>

<note>
Retrying the job usually passes.
The failure is always a timeout.
</note>

## How to work

- When the task leaves a choice open, pick what you can best defend, record it with add_task_note (what you chose, why, the alternative) and carry on. Stop only when going on would destroy something, needs access you lack, or the whole task hinges on it.
- Also note findings the operator would want. No progress notes.
- Work on fix/login-timeout. It already has commits; read them against main first, then add commits on top. Never rebase, amend or force-push it; reuse its PR.
- You may stack: name new branches legato/k7q2m9xa-<a few words>, on top of fix/login-timeout.
- Pushing these branches and opening draft PRs is already allowed. Open PRs as drafts; never merge.

## When you finish

- A final note: how it ended (done, partly done, or stopped and why), the PRs, what to check first.
- Move the task to Done (`legato task update --status Done`) even if you stopped, then end your turn and stay: the operator may talk to you here before taking the task back.
