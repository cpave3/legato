---
title: Deliver work through review
description: Take a task from implementation through incremental GitHub review, optional stewardship and an operator-owned merge.
sidebar:
  order: 69
---

Use a workflow to connect implementation, GitHub review and follow-up after approval. **Ship** is the initiative shorthand for this machinery, not a separate command or screen. You choose the workflow and its opt-ins; Legato never merges a pull request or enables auto-merge.

:::caution[Build requirement]
The review-loop and steward features described here need a Legato build that includes them. [Review passes](/legato/using/review-passes/) documents the current release caveat. Do not assume an older installed binary has the built-in or its latest inputs: check `legato version`, `legato workflow list --json` and `legato workflow validate <file>` on the instance that will run it.
:::

## Choose the right kind of review

| You want to… | Use |
| --- | --- |
| Understand an agent's diff, ask questions and flag changes yourself | A [review tour](/legato/using/reviews/), optionally with a [review handoff packet](/legato/using/review-handoff/) explaining decisions and evidence. Neither posts a GitHub approval. |
| Have an independent agent post GitHub reviews, then review fixes incrementally | The built-in [`review-loop`](/legato/using/review-passes/#review-any-pull-request) workflow. A **review pass** is one review within that loop, not a tour's numbered pass. |
| Keep an agent responsible for CI, comments and stack maintenance after approval | Opt into the [steward](/legato/using/review-passes/#after-approval-the-steward), the post-approval part of an agent-authored review loop. |
| Run implementation, review and outcome-dependent cleanup together | A [workflow](/legato/using/workflows/), calling `review-loop` with `workflow.run`. |

Turning off tour prompts with `agents.reviews: false` does not configure review passes. Conversely, enabling review passes does not require you to read or complete a tour.

## Choose what you enable

| Choice | Default / effect |
| --- | --- |
| `review_passes.enabled` | `false`. Enable it on the instance that runs the review engine, with its reviewer App and agent tools configured. Settings alone start no loop on local work. |
| `scope` | With `pr_number`, the whole **stack** by default; `pr` limits review to that PR. `prs` instead names an explicit bottom-first list (at most 20). |
| `agent_author` | `false`: the branch's owner fixes and replies on GitHub. With `true`, linked board tasks' agents fix and answer feedback; Legato must find the author task and be able to resume its agent. |
| `ready` | `false`: drafts stay drafts. With `true`, Legato marks approved, head-confirmed drafts ready, bottom first. This is not merging. With `agent_author: true` too, Legato also keeps the pull requests drafts until then, and turns them back into drafts whenever they go back to review, so ready for review means approved at the head ([Draft until approved](/legato/using/workflows/#draft-until-approved)). |
| `steward` | `false`: the built-in ends at approval. With `true`, it watches after approval and re-enters review after pushes; needs `agent_author: true`. |
| `max_passes` | Uses this instance's setting when 0: 3 by default, **per approval window**, not over the loop's entire life. Confirmed approval resets the window. |
| `max_reentries` | Uses this instance's setting when 0: 5 by default. A workflow may lower it, never raise it. One restack moving several heads uses one re-entry. |
| A make-a-change task handed to a worker | Its worker starts the built-in with the handoff binding when review passes are enabled for that task. It ends at approval, without steward. Ready-after-review follows the worker's repository rule. |
| Bundled `ticket-to-merged-pr` | Explicitly enables agent author, stack scope, ready and steward. Other templates, workflows of your own and local copies need not do so. |

Workflow files and resolved inputs are [snapshotted per run](/legato/using/workflows/#what-a-started-run-keeps); an open loop also keeps its selected reviewer and limits. Editing a template or a default does not retrofit ready or steward onto runs already using an older snapshot. Adding a template copies it into your workflows; it is not a subscription to later template updates.

## Before you start

1. On the instance doing the review, [set up review passes](/legato/using/review-passes/#set-up-a-worker): agent tools (`agents.mcp.enabled`), a configured reviewer tool/model/skill and the separate, host-only reviewer App installed on the repository. Choose another model from the author's. The reviewer App needs the [documented GitHub permissions](/legato/using/review-passes/#1-the-reviewer-app), including access to checks. Without Administration read, configure the required-check fallback explicitly.
2. For an agent author, give its normal author credential access to the repository and link its task's PRs. An agent opening a PR through Legato's GitHub credential is linked automatically; otherwise [link the task](/legato/using/review-passes/#after-approval-the-steward). The reviewer credential is never handed to that agent.
3. Check your credential rules for the operations the author may need: scoped pushes, PR creation and replies, restacks/force-with-lease, and, if necessary, an Actions rerun (`actions.write`) or workflow-file push (`git.push-workflows`). Starting a workflow grants no blanket permission. Permission requests can still need your answer mid-run.
4. Make sure a Legato process is running the review engine. With several processes on one data folder, only the **broker holder** runs it; a run started from another process waits for that holder. If it exits, the next holder picks up the durable loops. See [engine ownership](/legato/using/review-passes/#review-any-pull-request).

For the bundled ticket-to-merge path, also configure an **Implement** macro that opens a PR and reports `pr_number`. Closing a Burnlite task needs a connected Burnlite account with task-write access; non-Burnlite cards skip that close. Workflow fixed steps use the instance's configured integrations, not an agent's permission prompt as a substitute.

## The delivery path

![Implementation leads to review and author fixes. A clean pass is confirmed at GitHub's heads, optionally marked ready, then either ends approved or enters steward watch. A push returns from watch to incremental review. Only an operator-owned merge leads to merged.](/legato/diagrams/delivery-review.svg)

1. **Implement and push.** The task's own agent opens the PR or stack and completes its workflow assignment with `pr_number`. A step with declared outputs waits for that completion, not merely an idle conversation. The author stays available for feedback.
2. **Review at pinned heads.** Legato finds the stack, gives a read-only reviewer its own slot and clone, and posts one review per PR. Each layer is reviewed against **its own base**, not every layer against `main`. One active loop holds each PR; another loop is refused with a link to the holder.
3. **Fix, answer, review only the changes.** The author fixes blocking findings or disputes them with reasons; nits are optional. Later passes review changes since the last posted pass and rule on earlier blocking findings. They do not start a full review again. New blockers normally must be in changed PR lines; a critical exception needs a reason. A rebase does not make unrelated new `main` lines fair game for blocking. In a stack, fix bottom-up, restack and push every moved branch. [Review passes](/legato/using/review-passes/#how-a-review-runs) explains the exact bar and retry rules.
4. **Confirm, then optionally mark ready.** With `ready` on an agent's loop, the pull requests are drafts from the moment the loop opens until Legato marks them ready here, and go back to drafts on a push, a blocking pass or a newly failing check. A moved head discards the stale pass without using a pass allowance. Clean lower layers may be approved before the whole stack is clean. Legato re-reads the heads after posting before confirming approval. On an agent-authored loop, failing counted checks also need settling; a reply can explain a flaky failure, so approval is not a guarantee that every CI job is green. A person-authored loop counts reviewer findings, not other comments or checks. A clean outside contributor's PR gets a comment, not an App approval: inspect `approval_withheld`, not just `outcome: approved`.
5. **Optionally watch after approval.** The steward routes counted comments, failing checks and conflicts to the author. It asks for a restack when a lower layer merges, takes in a new PR stacked on top and re-enters incremental review on a push. A reply to a check/comment alone starts no review; a disputed blocking finding can. It polls every minute for an hour after changes, then every five minutes. [The steward's detailed rules](/legato/using/review-passes/#after-approval-the-steward) cover limits, new layers and mergeability.
6. **You own the merge.** Merge on GitHub when ready, in stack order as appropriate. The workflow observes the outcome and runs only the branches you wrote. It does not merge for you, close every ticket, move every card or stop every author by default.

## Try the bundled ticket-to-merge recipe

After setup, copy and inspect the bundled definition before starting it:

```bash
legato workflow add ticket-to-merged-pr
legato workflow validate ticket-to-merged-pr
legato workflow run ticket-to-merged-pr \
  --input task=<existing-task-id> --input repo=acme/widget
```

Use a task whose agent is not already running: this template starts its task agent and does not set `reuse: true`. Its automatic column trigger ships **paused**; enabling it makes entry into Doing ask for confirmation and any missing inputs. You can keep it paused and run by hand. Follow the parent and folded child on **Flows › Runs**, with step outputs and attempts on their run pages.

The bundled recipe's cleanup is deliberate:

| Child outcome | Parent action |
| --- | --- |
| `merged` | Close the Burnlite ticket if applicable; stop the task agent. |
| `closed` or `idle` | Note the task, keep the ticket open, stop the task agent. Idle means seven days without a push, comment, review or check change. |
| `stuck` or `ended` | Note the task, keep the ticket open and leave the task agent running for you. `stuck` reaches the parent only after you end the stuck review; `ended` is End during stewardship. |
| Review refused, failed, cancelled or stopped | Pause the parent's review call: Retry, Skip or Stop. These are failures, not a successful `outcome` value. |

A skipped call does not manufacture `merged` outputs. Inspect what remains before continuing. The template does not close a ticket just because review approved it; it waits for the steward's `merged` outcome.

## A smaller outcome-branching workflow

For a PR already linked to an agent's board task, this complete version 2 file calls the built-in, opts into ready and steward, and records the result on that task. It does not start an author from scratch or merge anything. Save it as `review-and-watch.workflow.yaml` in a [discovered folder](/legato/using/workflows/#definition-locations), then validate it before running.

```yaml
schema_version: 2
name: review-and-watch
title: Review and watch an agent's pull requests
inputs:
  task: {type: task, required: true}
  repo: {type: string, required: true}
  pr_number: {type: integer, required: true}
steps:
  - id: review
    type: workflow.run
    on_failure: pause
    with:
      workflow: review-loop
      inputs:
        repo: ${{ workflow.inputs.repo }}
        pr_number: ${{ workflow.inputs.pr_number }}
        scope: stack
        agent_author: true
        ready: true
        steward: true
        max_reentries: 2
  - id: merged
    if: {field: steps.review.outputs.outcome, equals: merged}
    type: task.note
    with:
      task: ${{ workflow.inputs.task }}
      body: The reviewed stack merged. Ticket closure is still yours.
  - id: takeover
    if: {field: steps.review.outputs.outcome, in: [closed, idle, stuck, ended]}
    type: task.note
    with:
      task: ${{ workflow.inputs.task }}
      body: "Review ended ${{ steps.review.outputs.outcome }}. The author remains available; inspect the loop before taking over."
```

Here `max_reentries: 2` needs an instance budget of at least 2. The task input selects where the notes go; it does not itself link the PR or select its author. For a person-authored PR use `agent_author: false` and `steward: false`. Without steward, the success branch to consider is `approved`, and [`approval_withheld`](/legato/using/review-passes/#how-it-ends) still matters. Read the built-in's [output contract](/legato/using/workflows/#reviewing-pull-requests-review-loop) before branching: `findings` is counts and `prs` is per-PR state; there is no `reviews` list or automatic summary-note step.

## If the journey stops

**Shipping**, beside Review in the rail, lists every review loop with where it stands, its checks and what its steward last did, and takes the actions below from its row. See [Shipping](/legato/using/review-passes/#every-review-loop-at-once-shipping).

| What you see | What to do |
| --- | --- |
| Waiting for the review engine | Check the enabled setting and broker-holder process, not the reviewer's chat. |
| Waiting for the author / permission / question | Open the author or answer in Needs you. Idle is not an output-bearing step's completion signal. |
| Review stuck | Read the reason. Run another pass extends the current window; it cannot repair missing permissions or a lost author conversation. Message the author or End and take over. |
| Paused while marking drafts ready | Retry after fixing the refusal, Skip that operation, or Stop. Drafts already marked ready stay ready; there is no rollback. |
| "Not a draft" on a pull request | Your GitHub App could not turn it into a draft. The loop goes on; fix the App's permission, or treat that pull request's ready state as meaning nothing until it is approved. |
| Failed review call | Retry resumes the failed child and its held loop. Cancel the run to release its PRs if abandoning it. Do not start a competing loop. |
| Steward watching, but you want control | Stop watching **holds** it: no author messages or passes, but merge/close/idle still ends it. Watch again resumes and coalesces held pushes into one re-entry. End finishes stewardship and leaves the author running. |
| Steward at re-entry limit | No pass or new author starts. Watch again permits exactly one more re-entry; it does not raise the instance default. |
| Suspended agent step after lost tmux | Resume preserves the conversation/attempt; Retry starts fresh. See [crash recovery](/legato/using/workflows/#after-a-crash-suspended-steps). |

Cancel is not undo: posts already sent may arrive, ready pull requests remain ready and drafts remain drafts, and the task's own `on: <task>` agent is not stopped by cancelling its workflow. A workflow-owned reviewer is stopped and its clone removed when its loop ends. An idle, held, ended or stuck loop is not permission to merge; inspect CI, findings, approval and the current heads yourself.

For authoring beyond this recipe, use [workflow composition](/legato/using/workflows/#calling-a-workflow-workflowrun), [conditions and failure handling](/legato/using/workflows/#schema-version-2) and [loops](/legato/using/workflows/#loops). These let you add gates and recovery without changing the built-in review contract.
