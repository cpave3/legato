---
title: Reviews
description: Read an agent's change as a guided tour of chapters and notes, ask it questions, and flag what must change.
sidebar:
  order: 60
---

A review tour is an agent's change set out for you to read. The agent groups the diff into chapters in a sensible reading order, explains each one, and marks the parts that carry risk. You read the tour in the web UI or the TUI, ask the agent about any step or any lines, flag changes you want, and mark the review complete. Use a review tour when an agent has made a change you need to understand before you merge it.

A review tour is not the same as a [plan](/legato/using/plans/), which you agree before the work starts. When you use both, the review shows the plan the work was meant to follow.

## Turning reviews off

By default Legato tells agents to make a review tour and a handoff packet at the end of their work. To stop that, set this in `config.yaml`:

```yaml
agents:
  reviews: false
```

Agents started after that are told to skip review tours and handoff packets unless you ask for a review. The review pages and the `legato review` commands still work, so you can still ask an agent for a tour. To give agents a different end-of-work step for one repository, add [guidance for that repository](/legato/using/agents/#guidance-for-a-repository).

## How a tour is built

Legato starts a tour by itself. When an agent starts in a Git repository, Legato records the commit it starts from. From then on, every commit the agent makes, and any uncommitted work, becomes a step of the tour for that task.

The agent then shapes the tour with `legato review` commands:

1. It makes ordinary commits with useful messages.
2. It groups the diff into **chapters**. A chapter can take hunks from several files and commits, and carries a title, a narration in Markdown, and an optional risk (`low`, `medium`, `high` or `unsure`):

   ```bash
   legato review chapter "Validate the search query" --narration-file narration.md \
     --include internal/service/search.go:2 --include web/src/Search.tsx:1 --risk high
   ```

3. It adds **notes** to single hunks or lines that need local context:

   ```bash
   legato review annotate "Empty queries return early" --file internal/service/search.go --hunk 2
   ```

   A note attaches to the newest commit unless the agent names one, and `--lines` narrows it to lines of the hunk. Notes stay attached when unrelated lines move.

4. When the work is tested, it marks the tour ready:

   ```bash
   legato review ready "Search the board by title"
   ```

`ready` records the commit the tour covers. Any hunk that no chapter includes goes into a chapter named **Remaining changes**, so nothing in the diff is left out. If the agent wrote no chapters, you read the tour commit by commit, with uncommitted work as a step named **Uncommitted changes**.

Narration can show image files, such as a screen before and after the change, with the same rules as a plan's images (see [Plans](/legato/using/plans/#image-files)). An image path is absolute, or relative to the narration file's folder.

A task can have more than one tour: the agent names each with `--name`, for example one per feature. The [CLI reference](/legato/reference/cli/) lists every `legato review` command.

:::tip
Agents learn these commands from `legato help` and `legato review --help`. A skill or a prompt that tells your agent to build a tour for each change makes tours consistent.
:::

## When a tour needs you

When the agent marks a tour ready:

- the board card's agent strip shows **ready for review** while the agent is idle;
- the tour appears under **Ready for review** in [Attention](/legato/using/attention/);
- it appears in the review queue.

## Reading a tour in the web UI

Open **Review** in the navigation, or go to `/review`, and choose a tour. On a wide screen the page shows the tour's steps on the left, the selected step's diff in the middle, and the questions and answers on the right. On a phone the steps and the Q&A open as panels.

- **Steps.** Each chapter shows its title, risk and narration above its diff. Notes appear with the hunks they belong to.
- **Viewed.** Each hunk has a **Viewed** box that folds it away. **Mark step reviewed** marks the whole step.
- **Ask a question.** Use **Ask question** to ask about the step. To ask about particular lines, drag across the line numbers in the diff, or click one line and then another in the same hunk. Your question is sent to the agent with those lines. Ctrl+Enter (Cmd+Enter on a Mac) sends it.
- **Answers.** The agent answers with `legato review answer`. The answer appears in the Q&A pane as soon as it is stored. Asking the latest question again replaces its answer.
- **Flag change.** Flag a chapter, or the lines you selected, as a change you require. This records a finding.
- **Complete review.** Marks every step reviewed. If the agent commits more work later, the new steps come back to the queue next to the chapters you already read.
- **Plan context.** When the task has an approved or completed plan, the tour shows the plan as it was when the review started.

The queue page can select several tours and remove them with **Remove selected**, for example stale tours. Removing a tour does not touch the task, the repository, the worktree or Git history.

The **Handoff** view of a tour shows a [review handoff](/legato/using/review-handoff/), when the agent wrote one: a briefing on the change's behaviour, decisions and evidence.

## Reading a tour in the TUI

Press `V` on the board to open the review queue. Select a tour with `j` and `k` and open it with Enter.

| Key | Action |
| --- | --- |
| `j`, `k` | Select the next or previous step |
| `d`, `u` (or Ctrl+D, Ctrl+U) | Scroll the diff down or up |
| `g`, `G` | Jump to the top or bottom of the diff |
| Space | Mark the step reviewed, or not |
| `a` | Ask a question |
| `c` | Complete the review |
| `x` | Delete the review (confirm with `y`) |
| Esc, `q` | Back to the queue |

In the queue, `r` refreshes the list and `x` deletes the selected tour. The TUI draws narration as Markdown and shows an image as its alt text.

## Findings and follow-up plans

A finding is a change you require, attached to a chapter or to lines. Findings are kept apart from questions and answers.

When you choose **Request follow-up plan**, Legato records the request and then tells the task's agent. The request is kept if the agent is not running. The agent answers with an ordinary [plan](/legato/using/plans/), linked to the review:

```bash
legato plan submit <bundle> --review-pass <pass-id> --finding <finding-id>
```

You approve that plan as you approve any other. When the agent completes it, its findings are resolved and the review starts a new **pass**: a fresh round of chapters, numbered, with the completed plan as its context. Earlier passes stay on the page under their numbers, so you can see each round.

## Regenerating a tour

When a tour is badly organised, choose **Regenerate review** and tell the agent what to change, for example "Focus on API compatibility, reduce chapter size". The agent rebuilds the current pass from the same starting commit. An agent can do the same:

```bash
legato review restart --feedback "Focus on error handling"
```

Regenerating replaces the current pass. A follow-up plan, by contrast, keeps the earlier pass and adds a new one.

`legato review discard` removes a tour completely, as **Delete review** does. Neither touches the repository.

## Review briefings on the branch

Some agents write a review briefing for the pull request a branch will become: HTML pages under `.review/<branch-slug>/`, committed with the branch. On GitHub, the PR Review Briefings browser extension shows them. Legato shows them earlier, before the pull request exists.

In the web UI, open the agent and its **Artifacts** panel. Each page of the briefing is a tab after the agent's artifacts, with the branch's own folder first. Legato finds the pages that the branch adds or changes compared with `origin/HEAD` (else `main`, else `master`), committed or not. It reads the list again every 15 seconds and when the window gets the focus. A new or changed page marks the **Artifacts** switch, but never opens the panel by itself.

## Sharing a review on GitHub

Legato does not post reviews to GitHub. To share an agent's explanation of a change, have it write a [review handoff](/legato/using/review-handoff/) and export it as Markdown for a pull request description or comment. GitHub remains the place for the discussion and the verdict.

## Task history

A task's detail panel in the web UI lists all its plans and review tours, including completed, rejected and superseded ones, and how they link to each other. The plan and review queues show only the work that needs you.

Internals (the tour model, passes and lineage, the API) are in [docs/ai/reviews.md](https://github.com/cpave3/legato/blob/main/docs/ai/reviews.md) on GitHub.
