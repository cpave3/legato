---
title: Review passes
description: Have a second agent review pull requests in passes, posted as GitHub reviews, until a clean pass approves them; for a handed-off task's pull requests, or for any pull request through a workflow.
sidebar:
  order: 36
---

:::caution[Not yet released]
Review passes are built but not yet in a release: they work in Legato built from its main branch, and the details can change.
:::

Review passes review pull requests in a **review loop**: a reviewer agent posts each pass as a GitHub review, the **author** fixes or answers what it found, and the passes repeat until one is clean. A loop runs for a task you hand off to a worker, or for [any pull request](#review-any-pull-request), started from a workflow.

A task you [hand off](/legato/using/handoff/) to a worker comes back with pull requests. Review passes have the worker review them before the task is done. When the agent working on the task (the author) finishes, a second agent, the **reviewer**, on another model, reviews every pull request and posts a GitHub review. The author then works through what the reviewer found, and through comments people leave and checks that fail, until a pass is clean. Only then is the task done on your board.

This page ties the pieces together: what happens, how to set up a worker for it, what to do when a review needs you, and how to review any pull request.

## How a review runs

```text
author finishes ─▶ pass 1: reviewer reviews ─▶ clean? ── yes ─▶ checks pass ─▶ done
                         ▲                         │
                         │                         no
                         │                         ▼
                         └── author pushes ◀── author fixes or replies
```

1. **The author finishes.** It moves its task to Done. The pass waits while the author is still working or waiting for an answer; a background job it left running (a server, a `tail -f`) does not hold the pass. Before a pass starts, every branch must be pushed: its pull request's head on GitHub must be what the author has, with nothing left uncommitted. If not, the author is told once to push or discard. A task with no pull request is done as it would be without review passes.
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

A handed-off task's review passes run on the worker; the main instance needs nothing. A review loop on [any pull request](#review-any-pull-request) runs on the Legato you start it on, which needs the same setup.

### 1. The reviewer App

A GitHub App cannot approve a pull request it opened, so the reviewer posts as a second App:

1. Create a GitHub App, such as `legato-reviewer`, with **Pull requests: Read and write**, **Contents: Read and write**, **Checks: Read**, **Commit statuses: Read**, **Actions: Read** and **Metadata: Read**. GitHub counts an App's approval only when it can write the repository's contents. Add **Administration: Read** if Legato is to read which checks branch protection requires; without it, list them in `review_passes.required_checks`. It needs no webhook.
2. Install it on the repositories you hand off.
3. Add it to the worker's credentials with the `github-reviewer` profile. See [The reviewer App](/legato/security/credentials-setup/#the-reviewer-app-for-review-passes).

The `github-reviewer` profile is Legato's own. No agent is given the credential, whatever it asks: Legato makes a token for each post and revokes it after. That is why the reviewer's approval can be trusted. An agent cannot approve its own work with it, or anyone else's.

### 2. The settings

Review passes need Legato's agent tools (`agents.mcp.enabled`, on by default): the reviewer reports its findings through the `submit_review` tool, so Legato refuses to start with review passes on and the tools off.

Turn review passes on in the worker's `config.yaml`:

```yaml
review_passes:
  enabled: true
  reviewer: {agent: codex, model: gpt-5.5, skill: 3-tier-review}
  github_credential: reviewer-app
  counted_bots: ["cubic-dev-ai[bot]"]
```

Choose a reviewer on another model from your authors: a second look by the same model finds less. The board needs the `Review` column (or the one `review_passes.review_column` names). Every setting is on [Configuration](/legato/reference/configuration/#review-passes).

The worker tells your main instance that it runs review passes. When you hand a task off, the Hand off form has a **Review passes** section, starting at the worker's own settings: turn them off for this task, or choose another reviewer, tier or model, skill, or number of passes. The Reeve, or any client of the API, can make the same choices with the `review` field of `POST /api/tasks/<id>/handoff` (see [API](/legato/reference/api/#handing-a-task-to-a-worker)). A worker on v0.16.0 refuses any such change, from the form or the API, with "this worker does not say what its review passes can do": update it.

## What your board shows

While the task is away, its row in the Away lane shows the review in place of the agent:

| The row says | What is happening |
| --- | --- |
| **Reviewing · pass 2** | A reviewer is at work. |
| **Waiting for the author · working** | The task is in Done, but the author is still in a turn (or **· it waits for an answer**: it is waiting for you). The pass starts once it stops. |
| **Waiting for the author to push** | The author was told its branches do not match GitHub. |
| **Waiting for GitHub** | GitHub did not answer; the worker tries again. |
| **Starting the next pass** | Nothing holds the pass; it starts in a moment. |
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

## Review any pull request

A review loop need not start from a handoff. The `review-pr-passes` workflow runs one on any open pull request in a repository Legato's reviewer App is installed on: yours, a coworker's, or an agent's you started on this Legato. The loop runs on the Legato you start it on, which needs review passes set up as above, with a reviewer App credential of its own.

Add the workflow with `legato workflow add review-pr-passes` (`--project` for the repository's own workflows folder), then start it from the web **Workflows** page, from the command line (`legato workflow run review-pr-passes --input repo=acme/widget --input pr_number=12`), or from the API (as the Reeve can), with the repository and the pull request's number:

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"inputs":{"repo":"acme/widget","pr_number":"12"}}' \
  https://server:3080/api/workflows/review-pr-passes/runs
```

Legato makes its own clone of the pull request's head (a fork's too) and starts pass 1 at once, in the run's reviewer slot. Each pass is posted as a GitHub review from the reviewer App, as for a handoff. The author is whoever owns the branch: Legato only reviews, and the author fixes by pushing and answers by replying in the review threads on GitHub. Only the reviewer's findings count: comments from other people and required checks are not items of this loop. No card moves.

### Tuning one loop

A loop takes its settings from this Legato's `review_passes`: the reviewer, its model or tier, the skill, the maximum passes and the timings. The `review-pr-passes` inputs override two of them for one run: `max_passes`, and `reviewer` (an agent tool, or `tool:tier`, such as `codex:careful`). An input left empty uses the setting. In a workflow of your own, the `github.pr.review_passes` step's `max_passes` and `reviewer` do the same, and when they template a run input, the input wins. A reviewer this Legato does not offer is refused when the run starts, with the ones it does.

### How the author moves the loop on

After a pass with findings, the run waits for the branch's owner, with no time limit. The next pass starts when:

- **the owner pushes**, and the pull request's head then stays still for `review_passes.quiet_time` (two minutes by default). A push during the wait starts the wait again, so a pass never reviews a half-pushed fix;
- **someone replies in a blocking finding's thread** without pushing: a dispute. The next pass reads the reply and rules on it, resolved or still open with why;
- **you press Run another pass** on the run's page (or call `POST /api/workflow-runs/<run>/steps/<step>/another-pass`), at once, whether or not anything was pushed. It is refused while a pass runs or after the loop ended.

Each later pass reviews only what changed since the one before, and rules on every earlier blocking finding. A loop that reaches its maximum passes, or whose passes keep finding new blocking problems in the fixes, is **stuck**. A pull request that is merged or closed ends the run with outcome `closed`.

A stuck loop raises one item in [Attention](/legato/using/attention/) on its run, with the reason. **Run another pass** reviews again, past the limit; **End** ends the loop with outcome `stuck`, and the run goes on to its note step. Both are also on the run's page (Run another pass) and in the TUI's Attention list (Enter, and `x` for End).

### How it ends

A clean pass approves the pull request and ends the run, unless the owner pushed while it ran: then the next pass reviews what was pushed. Its outputs are the `outcome` (`approved`), the number of `passes` and `findings`, the `reviews` posted with their links, `summary_path` (a Markdown summary) and `linked_task`, the board task the pull request belongs to (empty when none does). The workflow's second step adds the summary as a note to that task, and skips the note when there is none. Cancelling the run stops the reviewer and posts nothing more. Either way, Legato removes its clone.

The run is refused at once, with the reason, when:

- review passes are off on this Legato;
- the reviewer App cannot read the repository: the App is not installed on the account, the repository is not among those its installation selects, or there is no such repository (GitHub cannot say which). The find-only `review-pr` workflow reviews without posting;
- the pull request is not open;
- the pull request already has an active review loop, from another run or from a handoff: one loop runs per pull request. The refusal names that loop and links to it (the run's page, or the handed-off card). A loop that ended, approved, closed or was cancelled does not count;
- `agent_author` is asked for on a pull request no board task links.

### An agent as the author

When the pull request belongs to a task on your board (an agent you started opened it, say), start the run with `agent_author: true` and that task's agent is the author. It is told of each pass with the same `[legato review pass]` line a handed-off author gets, and answers with the same `legato review-pass feedback` and `legato review-pass answer` commands; its answers are posted as the author App. The next pass starts once it is idle with every finding answered and it pushed or disputed one, or on the triggers above. An author that exited is resumed from its saved conversation; one that cannot be resumed makes the loop stuck, since Legato never starts a fresh author on code it knows nothing about. A stuck loop with an agent author also offers **Message the author** in Attention. A task's agent authors one loop at a time: a second is refused until the first ends.
