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

For a ticket-to-merge walkthrough and the opt-ins, start with [Deliver work through review](/legato/using/delivery/). These GitHub review passes are not [review tours](/legato/using/reviews/) or their [review handoff packets](/legato/using/review-handoff/): reading or completing a tour does not approve a pull request or advance a loop.

This page ties the pieces together: what happens, how to set up a worker for it, what to do when a review needs you, and how to review any pull request.

## How a review runs

See the [delivery lifecycle diagram](/legato/using/delivery/#the-delivery-path) for the review, fix and post-approval re-entry paths.

1. **The author finishes.** It moves its task to Done. The pass waits while the author is still working or waiting for an answer; a background job it left running (a server, a `tail -f`) does not hold the pass. Before a pass starts, every branch must be pushed: its pull request's head on GitHub must be what the author has, with nothing left uncommitted. If not, the author is told once to push or discard. A task with no pull request is done as it would be without review passes.
2. **A pass.** The worker finds the work's whole stack on GitHub: from the task's branches, down base by base to the default branch, and up through the open pull requests stacked on them by the same author, whether the stack was made with `st`, by hand or by another tool (at most 20). It pins each pull request's head, moves the card to its Review column, and starts the reviewer in the task's reviewer slot (the **reviewer** chip on the card). A later pass resumes the same reviewer's conversation, so it remembers what it read and decided; when it cannot, a fresh reviewer starts. The reviewer can read the code but not change it, and it never runs as the author. It uses the review skill the worker names.
3. **The review.** The reviewer submits its findings. Each is **blocking** or a **nit** (optional). A blocking finding must say how it fails (see [What counts as blocking](#what-counts-as-blocking)). Before it posts, Legato reads every pull request's head from GitHub again. If any head moved since the pass pinned it (a push while the reviewer worked), Legato posts nothing for any pull request of the pass, sets the pass aside, and runs it again on the new heads; a pass set aside does not count toward the limit. Otherwise Legato posts one GitHub review per pull request as the reviewer App: an approval for each pull request the pass leaves clean (no blocking finding on it, and no failing required check), a comment review with the findings inline otherwise. In a stack, a clean lower layer is approved and can merge while a layer above is still being fixed; a later pass does not approve again a layer whose head has not moved, though it still reads that layer's head just before, and a head that moved then sets the pass aside. The task is done only when every pull request is clean. If GitHub answers a post with a server error, or not at all, Legato cannot tell whether the review landed. It looks for that review on GitHub before trying again, and posts it only if it is not there, so a pull request never gets the same review twice, and its threads are kept for later rulings. It still reads the heads again first: if one moved, the pass is run again on the new heads, even when every review was found. After ten tries, or an hour, the review is stuck. A clean pass counts as approved only once Legato has read every head from GitHub again after posting and found it is still the head the pass reviewed: until then the task stays out of Done and the main shows "Waiting for GitHub"; a push in between starts another pass.
4. **The author's turn.** When a pass has blocking findings, the card goes back to Doing and the author is told how many findings there are. It answers each one: fixed in a commit it pushes, a reply with its reasons, or (for a nit only) skipped. A finding below the bar, or one for another change, it may answer with a reply that names a follow-up. Legato posts each answer on the pull request as the author App, once: if the worker restarts before it records the post, it finds the answer on GitHub instead of posting it again.
5. **The next pass.** When the author moves the task to Done again, the next pass reviews what changed since the last pass that was posted and rules on each earlier blocking finding, always with a reason: resolved (how it was fixed, or why the author's answer stands), or still open with why. It does not review the rest again. A new blocking finding must be on lines that changed and that the pull request itself changes, unless the reviewer marks it critical and says why it cannot wait; anything else is a nit. So after a rebase onto a newer main, the lines main brought in are not the reviewer's to block on. A disputed finding that the author replied to is also ruled on, even if nothing was pushed.
6. **Done.** A clean pass approves the pull requests. The task is done once their required checks pass too.

On the worker, each handed-off task's review is a run of the built-in [`review-loop`](#review-any-pull-request) workflow: the worker starts it when it takes the task, so comments and checks count from the start, and it shows on the worker's **Workflows** page, linked to the task's card. It runs the review as described here, the card in Done being the author's signal, and ends once the pull requests are approved (a handed-off task's review never goes on past approval). Recalling, dropping or returning the task, or the main leaving or being unpaired, cancels the run: the review stops and nothing more is posted. A handoff's review that was already under way when the worker was updated carries on in a `review-loop` run the worker starts for it, with its passes, findings and answers kept; a pass the reviewer was working on is posted once, when it is done.

### Drafts marked ready after review

A handed-off task's pull requests stay drafts after approval unless the worker's [pull request rule](/legato/reference/configuration/#pull-requests) for the repository says otherwise. Give the `draft` rule `ready_after_review: true` (in Settings › Repositories › Pull requests, **Mark ready once reviewed**):

```yaml
pull_requests:
  rules:
    - match: acme/*
      mode: draft
      ready_after_review: true
```

The worker reads the rule when it starts the task's review. Once review passes approve the whole stack, and Legato has read the heads from GitHub and found them unchanged, it marks each draft ready for review, bottom first, as your GitHub App; a pull request that is already ready is left alone. The author agent is told not to mark them ready itself. If GitHub will not mark one ready, the review run pauses at that step on the worker's **Workflows** page, with **Retry**, **Skip** and **Stop**; the ones below it stay ready. Legato never merges a pull request and never turns on auto-merge: merging is yours.

A review runs at most `review_passes.max_passes` passes per approval window (3 by default). A pass set aside because a head moved is not counted. The count starts again after each confirmed approval, so a stewarded loop can post more than that many passes over its lifetime. **Run another pass** extends only the current window, not the configured limit. At the limit the review is **stuck**, and you decide. From the third pass onward it can stop sooner when two successive passes each find new blocking problems in the author's fixes: the author answers the last one, and then you decide whether another pass is worth it.

### What counts as blocking

A finding is blocking only when it must be fixed before the change merges, and the reviewer can say how it fails: what happens, and what goes wrong. Legato posts that with the finding. Blocking is a bug ordinary use reaches or an agent can trigger, data lost or corrupted, a security or sandbox boundary an agent can cross, the task not done, or the build or tests broken. A problem you would meet only by choosing a setting on purpose, hardening beyond the task, a race with no realistic order of events, and style are nits.

### Comments and checks

The review is not only the reviewer's. While the task is away, the worker polls its pull requests:

- **Comments** from the repository's owners, members and collaborators, and from bots you list in `review_passes.counted_bots` (Legato adds the `[bot]` suffix to a name without it, so `cubic-dev-ai` is the bot `cubic-dev-ai[bot]`, never a person called `cubic-dev-ai`), go to the author to answer, a few minutes after the last one arrives. Other people's comments are not sent to the author. Legato knows its own reviews and comments by the App that posted them, never by the hidden marker in their text: someone who copies the marker into a review of their own is treated as who they are. A comment after a clean pass makes the task not done again until it is answered. When GitHub limits Legato's requests or does not answer, the poll waits and loses nothing: a comment drops off only when GitHub says it was deleted.
- **Checks**: every check on a pull request counts, with nothing to set. A failing one goes to the author with its summary, a link and a few lines of what it reported, unless the base branch fails it too: then it is not the pull request's doing, and the run page and the Shipping page note it ("Also failing on its base: docs") instead. A check still running is waited for; a cancelled one does not count. The checks branch protection requires on the base branch, or `review_passes.required_checks` when it requires none, count even when the base fails them. It is handled when it passes on a commit the author pushes, or when the author replies (a flaky test, say). Reviews do not wait for CI; only the task's done does.

The author reads all of this as data through `legato review-pass feedback --json`. When the review covers a stack, the feedback lists its pull requests bottom first, and the author is told to fix each item on its own branch from the bottom up, restack the layers above (`st restack`, or `git rebase --update-refs` without Staccato), push every branch that moved with `--force-with-lease`, then move the task to Done (for a handoff's loop). If the author's agent has the `address-stack-feedback` skill, it is told to use it. Nothing from GitHub is typed into its terminal. Legato finds the loop from the author's own session, and only that agent may answer: the reviewer and other tasks' agents are refused. On the host, outside any session, you can read, answer and steer a loop by its ID (`--loop rl-…`). See [CLI](/legato/reference/cli/#review-passes).

## Set up a worker

A handed-off task's review passes run on the worker; the main instance needs nothing. A review loop on [any pull request](#review-any-pull-request) runs on the instance you start it on, which needs the same setup.

### 1. The reviewer App

A GitHub App cannot approve a pull request it opened, so the reviewer posts as a second App:

1. Create a GitHub App, such as `legato-reviewer`, with **Pull requests: Read and write**, **Contents: Read and write**, **Checks: Read**, **Commit statuses: Read**, **Actions: Read** and **Metadata: Read**. GitHub counts an App's approval only when it can write the repository's contents. Add **Administration: Read** if Legato is to read which checks branch protection requires; without it, list them in `review_passes.required_checks`. It needs no webhook.
2. Install it on the repositories you hand off.
3. Add it to the worker's credentials with the `github-reviewer` profile. See [The reviewer App](/legato/security/credentials-setup/#the-reviewer-app-for-review-passes).

The `github-reviewer` profile is Legato's own. No agent is given the credential, whatever it asks: Legato makes a token for each post and revokes it after. That is why the reviewer's approval can be trusted. An agent cannot approve its own work with it, or anyone else's.

If you copied custom `github` or `github-reviewer` profiles from an older installation, run `legato doctor`: review posting needs App identity metadata (`actor: "$.app_slug"` under `jwt_exchange.lookup`). The shipped profiles include it; doctor names a custom profile and the required repair when it is missing.

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

In the web UI, the author's chat and the reviewer's chat link to each other: the **Related** panel lists **Reviewer · pass N** (with the loop's state) for the author, and **Author** for the reviewer, on the worker where both run. Click a running agent's row to switch to it. A reviewer that has ended stays there, marked ended, until the loop ends; its row opens the loop instead.

The task panel links to Legato's latest review of each pull request. While a review runs, **Return** is not offered; **Recall** still brings the task back at once, and nothing of the review is posted after it, not even a pass the reviewer has already finished. The note the task comes back with sums up the passes and any items left open.

## When a review is stuck

A review is stuck when:

- the passes reached their limit with a finding still open;
- from the third pass onward, two successive passes each found new blocking problems in the author's fixes;
- the reviewer could not review, ran past its time, or exited twice without submitting (when a reviewer exits without submitting, a crash or Legato restarting mid-pass, Legato runs the same pass once more at the same pull request heads, and that does not count as a pass);
- the author's checkout still did not match GitHub after it was told to push;
- the author ended and could not be brought back to hear its feedback;
- GitHub refused a post;
- GitHub did not confirm a post after ten tries or an hour. The reason names the pull requests.

A stuck review is in [Attention](/legato/using/attention/), and the row, its panel and the Attention item offer:

- **Run another pass**: one more pass past the limit (only this once: the limit itself stays as it was), or another try after a reviewer failed. When GitHub did not confirm a post, it tries that post again instead (finding it on GitHub if it landed), and starts no new pass.
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

A review loop need not start from a handoff. The built-in `review-loop` workflow, which every Legato has with nothing to add, runs one on any open pull request, or a list of them; call it from your own workflows with `workflow.run` and branch on its outcome (see [Reviewing pull requests: review-loop](/legato/using/workflows/#reviewing-pull-requests-review-loop)), or run it from the **Workflows** page or `legato workflow run review-loop --input repo=acme/widget --input pr_number=12`. A `review-pr-passes` workflow you added from an older Legato, with its one inline review step, still runs a loop as before, but that step is deprecated: `legato workflow upgrade <file>` rewrites it to call `review-loop` (see [Moving off the inline review step](/legato/using/workflows/#moving-off-the-inline-review-step)). Either runs one in a repository Legato's reviewer App is installed on: yours, a coworker's, or an agent's you started on this Legato. The loop runs on the instance you start it on, which needs review passes set up as above, with a reviewer App credential of its own.

When several Legato processes share one data folder (the TUI and `legato serve`, say), the review loops run in the one that serves the broker: the first one started, until it exits. You can start a loop from any of them, or from `legato workflow run`: the run's review step waits, its page saying **Waiting for the review engine**, until that process picks it up, which takes a few seconds. If the process running the loops exits, the next one to serve the broker carries on with every loop.

The card menu (in the web UI, and **Workflows** in the TUI's `c` menu) offers a review of each of the card's open pull requests: **Open review loop · #N** when a loop already holds it, whatever started it, else **Review passes · #N · agent** or **· person**, which starts `review-loop` (see [Board](/legato/using/board/)). It is an agent's only when Legato's author App opened the pull request, so a coworker's pull request is never handed to an agent to fix. A review started from the TUI runs even when the loops run in another process, such as `legato serve`. The menu offers these only while some Legato on the data folder runs the review loops. When none does, it says why in place of the item: the review passes setting (`review_passes.enabled`) is off, or "Review passes need `legato serve` running". Start `legato serve` (or the TUI) and open the menu again.

Start `review-loop` from the web **Workflows** page, from the command line (`legato workflow run review-loop --input repo=acme/widget --input pr_number=12`), or from the API (as the Reeve can), with the repository and the pull request's number:

```bash
curl -X POST -H "Authorization: Bearer $TOKEN" \
  -d '{"inputs":{"repo":"acme/widget","pr_number":"12"}}' \
  https://server:3080/api/workflows/review-loop/runs
```

Legato makes the reviewer a clone of its own at the pull request's head (a fork's too) and starts pass 1 at once, in the run's reviewer slot. The clone is never one an author works in, even when an earlier step of the same run cloned the repository. It is on a branch named after the pull request's head branch, so `git status` in it says which pull request is under review; a head in a fork is on `legato-review/pr-<n>` instead. The reviewer's brief names the head branch, its commit, and the branch the checkout is on. Each pass is posted as a GitHub review from the reviewer App, as for a handoff. The author is whoever owns the branch: Legato only reviews, and the author fixes by pushing and answers by replying in the review threads on GitHub. Only the reviewer's findings count: comments from other people are not items of this loop, and neither are required checks unless an agent authors it (below). No card moves.

### Tuning one loop

By default the loop reviews the whole stack the pull request is in (`scope: stack`): Legato follows GitHub's chain from its branch, down base by base to the default branch and up through the open pull requests stacked on it by the same author, and reviews every one of them in one loop, each against its own base, with one GitHub review per pull request. Start it on any layer; the run is named after the stack (`acme/widget#137–#141`). A layer that merges or closes drops out, and the loop ends closed only when none is left open. A push to any layer, such as a fix at the bottom with the layers above restacked, starts the next pass once the heads stay still. Give `scope: pr` to review the named pull request alone. A run on a layer another loop already reviews is refused, naming that loop.

A loop takes its settings from this Legato's `review_passes`: the reviewer, its model or tier, the skill, the maximum passes and the timings. The `review-loop` inputs override some of them for one run: `max_passes`, `reviewer` (an agent tool, or `tool:tier`, such as `codex:careful`) and `skill`. An input left empty uses the setting. An old copy's `github.pr.review_passes` step takes `max_passes` and `reviewer` the same way, and when they template a run input, the input wins. A reviewer this Legato does not offer is refused when the loop is picked up, with the ones it does: the run fails at its review step.

### How the author moves the loop on

After a pass with findings, the run waits for the branch's owner, with no time limit. The next pass starts when:

- **the owner pushes**, and the pull request's head then stays still for `review_passes.quiet_time` (two minutes by default). A push during the wait starts the wait again, so a pass never reviews a half-pushed fix;
- **someone who may dispute replies in a blocking finding's thread** without pushing: a dispute. That is the pull request's author, a repository owner, member or collaborator, or a bot named in `review_passes.counted_bots`, after the pass that raised the finding (or ruled it still open). Anyone else's reply, or one written before that pass, is not a dispute. The next pass reads the reply and rules on it, resolved or still open with why;
- **you press Run another pass** on the run's page (or call `POST /api/workflow-runs/<run>/steps/<step>/another-pass`), at once, whether or not anything was pushed. It is refused while a pass runs or after the loop ended.

Each later pass reviews only what changed since the last one posted, and rules on every earlier blocking finding. A loop that reaches its maximum passes, or whose passes keep finding new blocking problems in the fixes, is **stuck**. A pull request that is merged or closed ends the run with outcome `closed`.

A stuck loop raises one item in [Attention](/legato/using/attention/) on its run, with the reason. A post GitHub did not confirm after ten tries or an hour is stuck too, naming the pull requests. **Run another pass** reviews again, past the limit (or, for a post GitHub did not confirm, tries that post again); **End** ends the loop with outcome `stuck`, and the built-in run goes on to its close step. Both are also on the run's page (Run another pass) and in the TUI's Attention list (Enter, and `x` for End).

### How it ends

A clean pass approves the pull request and ends the run. When the pull request's author is outside the repository (GitHub calls them a contributor, a first-time contributor or none, rather than an owner, member or collaborator), the clean pass posts a comment that opens "clean; not approved: the author is outside the repository" in place of the approval, so text in an outsider's pull request can never talk the reviewer into an approval; approving it is yours to do. The run still ends **approved**, but its `approval_withheld` output is true (false otherwise), and that pull request's entry in `prs` has `approved: false`, so a caller can branch on it. This applies only without `agent_author`: a pull request your agent opened through Legato's GitHub App is approved when clean, though GitHub calls the App's association none. When the owner pushed while it ran, nothing is posted at the old head: the pass is reviewed again on what was pushed, as the same pass. The run ends approved only once Legato has read the heads from GitHub after posting and found them unchanged (the run page says "Clean · waiting for GitHub to confirm the heads" until then), and, on an agent-authored run, once the approval of a pull request held back for a failing check that is now settled is on GitHub. Its outputs are `outcome`, `loop_id`, `passes` (the number posted), `clean_pass`, `confirmed_at`, `findings` (counts: `total`, `blocking`, `nits`, `open`), `prs`, `approval_withheld`, `summary_path` (a Markdown summary), `linked_task` and `ready`. Each `prs` entry has `number`, `url`, `state`, `draft`, `author_task`, `approved`, `head_sha` and `review_url`. The built-in writes no task note: a caller adds one with `task.note` and `body` or `body_path` if wanted. The old inline step's `reviews`, `event` and `withheld_reason` outputs are not outputs of `review-loop`. Cancelling the run stops the reviewer and posts nothing more: not a pass the reviewer has already finished, nor an answer the author gave. The run ends **cancelled**, never approved, even when you cancel it from another Legato window or after its clean pass was posted but before Legato read the heads. Legato checks that the review is still open just before each thing it posts to GitHub, so only a post already on its way when you cancel can still arrive. Either way, Legato removes its clone.

When the review step fails, its loop is kept, and so is its hold on the pull request: no other loop can start on it until you retry, cancel or end the run. **Retry step** picks it up where it stopped, with the same passes, findings and answers: the pass after the last one posted comes next, and a pass that was under way when the step failed runs again under its own number. Legato makes a new clone for it. The retry is refused, as a new run would be, when another loop now reviews the pull request or the pull request is no longer open. While the failed run keeps its loop, no other review starts on its pull requests: **Cancel run** gives it up, and the loop ends, freeing them.

The loop belongs to its run, and ends when the run does: when the run succeeds or is cancelled, Legato stops the reviewer, frees the pull request for another loop and removes its clone. A run that ended while Legato was not running has its loop ended when Legato starts again.

The run is refused at once, with the reason, when:

- review passes are off on this Legato;
- the reviewer App cannot read the repository: the App is not installed on the account, the repository is not among those its installation selects, or there is no such repository (GitHub cannot say which). The find-only `review-pr` workflow reviews without posting;
- the pull request is not open;
- the pull request already has an active review loop, from another run or from a handoff: one loop runs per pull request. The refusal starts with its code, `review_loop_active`, and names that loop by its ID (`rl-` and 12 characters) with a link to it (the run's page, or the handed-off card), for example "review_loop_active: pull request #12 of acme/widget already has an active review loop: run wr-1 (/workflows/runs/wr-1), loop rl-0f3a9c2d81e4". A handoff's loop holds each pull request from the pass that first reviews it until it is approved, which is only once Legato has read the heads from GitHub after its clean pass. A loop that ended, approved, closed or was cancelled does not count. When two runs on one pull request start at the same moment, the first that Legato opens reviews it and the other is refused naming its loop;
- `agent_author` is asked for on a pull request no board task links, even after Legato looked again (below).

A loop outcome is not an undo log or a live approval report. A loop that ends `closed`, `stuck`, `idle` or `ended` can already have posted approvals on clean lower layers, and these remain on GitHub. Even `merged` does not make every `prs[].approved` output true: inspect the current PRs and their review links when taking over.

### An agent as the author

When the pull request belongs to a task on your board (an agent you started opened it, say), start the run with `agent_author: true` and that task's agent is the author. It is told of each pass with the same `[legato review pass]` line a handed-off author gets, and answers with the same `legato review-pass feedback` and `legato review-pass answer` commands; its answers are posted as the author App. The next pass starts once it is idle with every finding answered and it pushed or disputed one, or on the triggers above: no card needs moving to Done. The loop is kept on the run's own task, so its feedback names both the run's task and the card the pull request belongs to, and the reviewer's brief gives that card's title and description as the task under review. An author that exited is resumed from its saved conversation; one that cannot be resumed makes the loop stuck, since Legato never starts a fresh author on code it knows nothing about. A stuck loop with an agent author also offers **Message the author** in Attention. While the agent works on the findings, the run says **Author fixing** and does not need you; an author that asked the Reeve something shows as **Waiting on the Reeve** until it is answered (it needs you if the Reeve has not answered 15 minutes after it was told). It needs you only when the agent waits for your answer, or has stopped or ended with findings unanswered for longer than `review_passes.quiet_time`. A run with a person as author needs you whenever it waits for that author. An agent author also gets each comment and review from the repository's owners, members and collaborators, or from a listed bot, from the first pass on, and answers them as a handed-off author does. It also gets each failing required check, as a handed-off author does: the pull request is not approved while one fails, and the run ends approved only when each passes on a pushed commit or the agent answers it with a reply. A task's agent authors one loop at a time: a second is refused until the first ends, with the code `author_loop_active` and the first loop's ID and link.

### After approval: the steward

With `steward: true` (which needs `agent_author: true`), `review-loop` does not end at approval: Legato's steward looks after the pull requests for their agent until they merge. It never merges or enables auto-merge. The author makes fixes, Legato posts its answers and reviews, and `ready: true` lets Legato mark confirmed drafts ready (and turn them back into drafts when a push sends them back to review: see [Draft until approved](/legato/using/workflows/#draft-until-approved)). It reads each pull request every minute for an hour after anything changes, then every five minutes, as the reviewer App, and:

- sends the agent each counted check that fails after approval, and each new comment or review that counts, as feedback items it reads with `legato review-pass feedback --json`. The line that tells it opens `[legato steward]`, not `[legato review pass]`. Each item names what a fix may need: a push to the branch (`git.push`, or `git.push-workflows` for a change under `.github/workflows`), or, for a job that failed before its tests ran, one re-run with `gh run rerun` (`actions.write`). Nothing is granted ahead: your credential rules decide each, as they always do. A reply alone starts no pass;
- goes back to review on any push: one **re-entry**, however many layers of the stack moved, so a restack counts once. The next pass reviews what changed since the last one, and the pull requests are approved again only after a clean pass that Legato confirms at the new heads. A re-entry's pass does not wait for answers to checks, comments or mergeability items, only to blocking findings; disputing a blocking finding with a reply starts the next pass without a push;
- sends the agent one **mergeability** item per pull request when GitHub reports it has conflicts with its base, or that it is behind a base that requires an up-to-date branch (only when GitHub's merge state says `BEHIND`): merge the base in or restack it, then push. The item closes by itself once GitHub reports the pull request mergeable, and comes back if the problem does. While GitHub is still working it out, the steward reads the pull request again a minute later;
- when a lower layer of a stack merges, tells the agent to restack the layers above it onto that layer's base. The push that follows goes back to review like any other;
- takes in a pull request the agent stacks on the top while the steward watches: it joins the loop, held by it as the others are, and goes to review. With `ready: true` it is marked ready once that review is confirmed. If another loop already holds it, the loop is stuck and names that loop;
- ends the run **merged** when every pull request has merged, **closed** when one is closed without merging, and **idle** after seven days with no push, comment, review or check change.

While an `agent_author` loop holds the pull requests, before approval or after, a workflow's [`pr.changes_requested` trigger](/legato/using/workflows/#when-changes-are-requested) skips them and records why, "held by review loop rl-…": a review requesting changes reaches the agent through the loop instead.

If someone dismisses the approval without pushing, the loop needs you; it does not automatically post another approval to override that decision.

The loop record counts re-entries against `max_reentries` (`review_passes.max_reentries`, 5 by default, which a workflow may lower but never raise). A push after the last re-entry allowed makes the loop **stuck**: no pass starts, no new agent is started, and the run waits for you.

While the steward watches, its waiting step takes three actions (through the run's step action, `POST /api/workflow-runs/{run_id}/steps/{step_id}/action` with `stop_watching`, `watch_again` or `end`):

- **Stop watching** holds the steward: it sends the agent nothing and starts no pass. Every pull request merging still ends the run, and so do one closed without merging and seven quiet days. You can take over the pull request meanwhile.
- **Watch again** lets a held steward watch again. If anything was pushed while it was held, the pull requests go back to review once, however many pushes there were. On a steward stuck at its last re-entry, Watch again allows exactly one more re-entry.
- **End** ends the steward, and the run ends **ended**. The author's agent is left running.

## Every review loop at once: Shipping

**Shipping**, beside Review in the rail (under **More** on a phone), lists every review loop, whichever started it: `ticket-to-merged-pr` or another workflow, the card menu, or a handed-off task. Each row shows:

- the task, the repository and each pull request of the stack, bottom first, linked to GitHub;
- where the loop stands: **Reviewing · pass 2 of 3**, **Changes requested**, **Approved**, **Steward watching**, **Steward held**, **Stuck** (with why, or **Stuck at the re-entry limit**), or **Approved · waiting for the merge** for a loop without a steward whose workflow waits for the merge; for a loop that keeps an agent's pull requests [drafts until approved](/legato/using/workflows/#draft-until-approved), what GitHub shows follows (**Reviewing · pass 2 of 3 · draft**, **Approved · ready for review**), and **Not a draft** with why when your GitHub App could not turn one into a draft;
- the checks (passing, running, or which fail on which pull request) and whether a pull request has conflicts or is behind its base, as the steward last read them, and what last woke the steward and when;
- the author's task and what its agent is doing, and the reviewer;
- **Open run**, and the loop's actions as its run page offers them: **Run another pass**, **Stop watching**, **Watch again** and **End** (which asks first). Message the author is on the run page.

**Needs you** shows the loops that wait on you: a held steward, a stuck loop, a check failing after approval, or an author waiting for you. **Active** shows every open loop, and **Recently ended** those that ended in the last 7 days, with how: merged, closed, ended or idle. The page follows changes as they happen.

The same list is `GET /api/shipping` and `legato review-pass list [--json]`.

To find the author, Legato looks for the board task linked to the pull request. When an agent opens a pull request through Legato's GitHub credential, its task is linked at once, so a run started right after works. When no task is linked yet, Legato looks again in the repository (by branch, commit and ticket key, as its poll does) before it refuses. If it still finds none, the run says so: link the pull request to its task with `legato task link <task-id> --repo owner/name --branch <head branch>`, or `p` on the task's card on the board, and run again. Or run without `agent_author`, and the branch's owner is the author.

The two agents link to each other in the web UI's **Related** panel. The author's chat lists **Reviewer · pass N** with the loop's state, and the reviewer's chat lists **Author**. Without `agent_author`, the reviewer's panel lists the pull request's board task, if there is one, and that task's agent still sees the reviewer.

Legato does not start a loop on local work by itself, but it prompts you: when a task on your board (not handed off) reaches Done or the Review column with open pull requests that no loop reviews, [Attention](/legato/using/attention/) offers **Start review passes**, which reviews the whole stack with the built-in `review-loop`, with the task's agent as the author when Legato's author App opened the pull requests. An agent's loop started this way, or from the card menu, has `ready` on: its pull requests are drafts until it approves them, then ready for review.
