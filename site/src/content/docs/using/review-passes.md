---
title: Review passes
description: Have a worker instance review a handed-off task's pull requests with a second agent, and let the first one work through what it finds, before the task is done.
sidebar:
  order: 36
---

:::caution[Not yet released]
Review passes are built but not yet in a release: they work in Legato built from its main branch, and the details can change.
:::

A task you [hand off](/legato/using/handoff/) to a worker comes back with pull requests. Review passes have the worker review them before the task is done. When the agent working on the task (the author) finishes, a second agent, the **reviewer**, on another model, reviews every pull request and posts a GitHub review. The author then works through what the reviewer found, and through comments people leave and checks that fail, until a pass is clean. Only then is the task done on your board.

This page ties the pieces together: what happens, how to set up a worker for it, and what to do when a review needs you.

## How a review runs

```text
author finishes ─▶ pass 1: reviewer reviews ─▶ clean? ── yes ─▶ checks pass ─▶ done
                         ▲                         │
                         │                         no
                         │                         ▼
                         └── author pushes ◀── author fixes or replies
```

1. **The author finishes.** It moves its task to Done. Before a pass starts, every branch must be pushed: its pull request's head on GitHub must be what the author has, with nothing left uncommitted. If not, the author is told once to push or discard. A task with no pull request is done as it would be without review passes.
2. **A pass.** The worker pins each pull request's head, moves the card to its Review column, and starts the reviewer in the task's reviewer slot (the **reviewer** chip on the card). A later pass resumes the same reviewer's conversation, so it remembers what it read and decided; when it cannot, a fresh reviewer starts. The reviewer can read the code but not change it, and it never runs as the author. It uses the review skill the worker names.
3. **The review.** The reviewer submits its findings. Each is **blocking** or a **nit** (optional). A blocking finding must say how it fails (see [What counts as blocking](#what-counts-as-blocking)). Legato posts one GitHub review per pull request as the reviewer App: an approval when the pass is clean, a comment review with the findings inline otherwise.
4. **The author's turn.** When a pass has blocking findings, the card goes back to Doing and the author is told how many findings there are. It answers each one: fixed in a commit it pushes, a reply with its reasons, or (for a nit only) skipped. A finding below the bar, or one for another change, it may answer with a reply that names a follow-up. Legato posts each answer on the pull request as the author App.
5. **The next pass.** When the author moves the task to Done again, the next pass reviews what changed since the last one and rules on each earlier blocking finding: resolved, or still open with why. It does not review the rest again. A new blocking finding must be on what changed, unless the reviewer marks it critical and says why it cannot wait; anything else is a nit. A disputed finding that the author replied to is also ruled on, even if nothing was pushed.
6. **Done.** A clean pass approves the pull requests. The task is done once their required checks pass too.

A pass runs at most `review_passes.max_passes` times. After that the review is **stuck**, and you decide. It stops sooner when two passes in a row each find new blocking problems in the author's fixes: the author answers the last one, and then you decide whether another pass is worth it.

### What counts as blocking

A finding is blocking only when it must be fixed before the change merges, and the reviewer can say how it fails: what happens, and what goes wrong. Legato posts that with the finding. Blocking is a bug ordinary use reaches or an agent can trigger, data lost or corrupted, a security or sandbox boundary an agent can cross, the task not done, or the build or tests broken. A problem you would meet only by choosing a setting on purpose, hardening beyond the task, a race with no realistic order of events, and style are nits.

### Comments and checks

The review is not only the reviewer's. While the task is away, the worker polls its pull requests:

- **Comments** from the repository's owners, members and collaborators, and from bots you list in `review_passes.counted_bots`, go to the author to answer, a few minutes after the last one arrives. Other people's comments are counted but not sent. A comment after a clean pass makes the task not done again until it is answered.
- **Required checks** are the checks branch protection requires on each pull request's base branch, or `review_passes.required_checks` when it requires none. A failing one goes to the author with its summary, a link and a few lines of what it reported. It is handled when it passes on a commit the author pushes, or when the author replies (a flaky test, say). Reviews do not wait for CI; only the task's done does.

The author reads all of this as data through `legato review-pass feedback --json`. Nothing from GitHub is typed into its terminal. See [CLI](/legato/reference/cli/#review-passes).

## Set up a worker

Review passes run on the worker. The main instance needs nothing.

### 1. The reviewer App

A GitHub App cannot approve a pull request it opened, so the reviewer posts as a second App:

1. Create a GitHub App, such as `legato-reviewer`, with **Pull requests: Read and write**, **Contents: Read and write**, **Checks: Read**, **Commit statuses: Read**, **Actions: Read** and **Metadata: Read**. GitHub counts an App's approval only when it can write the repository's contents. Add **Administration: Read** if Legato is to read which checks branch protection requires; without it, list them in `review_passes.required_checks`. It needs no webhook.
2. Install it on the repositories you hand off.
3. Add it to the worker's credentials with the `github-reviewer` profile. See [The reviewer App](/legato/security/credentials-setup/#the-reviewer-app-for-review-passes).

The `github-reviewer` profile is Legato's own. No agent is given the credential, whatever it asks: Legato makes a token for each post and revokes it after. That is why the reviewer's approval can be trusted. An agent cannot approve its own work with it, or anyone else's.

### 2. The settings

Turn review passes on in the worker's `config.yaml`:

```yaml
review_passes:
  enabled: true
  reviewer: {agent: codex, model: gpt-5.5, skill: 3-tier-review}
  github_credential: reviewer-app
  counted_bots: ["cubic-dev-ai[bot]"]
```

Choose a reviewer on another model from your authors: a second look by the same model finds less. The board needs the `Review` column (or the one `review_passes.review_column` names). Every setting is on [Configuration](/legato/reference/configuration/#review-passes).

The worker tells your main instance that it runs review passes. When you hand a task off, the Hand off form has a **Review passes** section, starting at the worker's own settings: turn them off for this task, or choose another reviewer, tier or model, skill, or number of passes.

## What your board shows

While the task is away, its row in the Away lane shows the review in place of the agent:

| The row says | What is happening |
| --- | --- |
| **Reviewing · pass 2** | A reviewer is at work. |
| **Waiting for a reviewer** | The worker's reviewers are all busy; this pass is next. |
| **Author fixing · 3 blocking, 1 comment** | The author is working through the feedback. |
| **Approved · waiting for 2 checks** | The pass was clean; required checks are still running. |
| **Done · approved** | Clean, approved, and the checks passed. |
| **Review stuck: …** | The review needs you. Why is in the row. |

The task panel links to Legato's latest review of each pull request. While a review runs, **Return** is not offered; **Recall** still brings the task back at once. The note the task comes back with sums up the passes and any items left open.

## When a review is stuck

A review is stuck when:

- the passes reached their limit with a finding still open;
- two passes in a row each found new blocking problems in the author's fixes;
- the reviewer could not review, ran past its time, or exited without submitting;
- the author's checkout still did not match GitHub after it was told to push;
- the author ended and could not be brought back to hear its feedback;
- GitHub refused a post.

A stuck review is in [Attention](/legato/using/attention/), and the row, its panel and the Attention item offer:

- **Run another pass**: one more pass past the limit, or another try after a reviewer failed.
- **Message the author**: your words reach the author on the worker, marked as from you. Use it to settle a dispute ("the reviewer is right, bound the retries") or to unblock it.
- **Return as is**: bring the task home with its pull requests as they are, and review them yourself.
- **Recall**: stop the agents and bring the task home now.

## Try it on a test repository

To see a review run end to end before you rely on it:

1. Make a test repository on GitHub with branch protection on `main` that requires one approving review and one check, such as a workflow that fails when a file named `FAIL` exists.
2. Install both the author App and the reviewer App on it, and give the worker both credentials.
3. On your main instance, make a task such as "Add a greeting function, and add a file named FAIL", and hand it off to the worker as **Make a change** on the test repository.
4. Watch the Away lane: the author opens a pull request and finishes, a pass reviews it, the failing check goes back to the author, and the task is done once the author removes `FAIL` and the next pass approves.
5. On GitHub, see the reviewer App's review, its inline findings, the author App's answers, and a resolved thread for each finding the next pass resolved.
