---
title: Plans
description: Agents propose a plan before they change code; you comment, answer its questions and approve it.
sidebar:
  order: 50
---

A plan is an agent's proposal for a piece of work, written before it changes any code. You read it in the web UI or the TUI, answer the questions the agent asks, comment on any part of it, and then approve it, ask for changes or reject it. The agent starts work only after you approve. Use a plan when a change is large enough that you want to agree the approach first.

A plan is not the same as a swarm plan, which splits work between several agents (see [Swarms](/legato/using/swarms/)), or a review tour, which you read after the work is done (see [Reviews](/legato/using/reviews/)).

## How an agent writes a plan

The agent writes the plan as a folder, the bundle, normally under `.legato/plans/<name>/`:

- `plan.md`: the plan itself, in Markdown. This is what you read, comment on and approve.
- `plan.json`: the plan's title and the questions the agent wants you to answer.
- image files that `plan.md` shows, by convention in `assets/` (see [Image files](#image-files)).

Then it submits the bundle:

```bash
legato plan submit .legato/plans/search
```

Legato checks both files and keeps a copy of them as a revision. When the agent submits the bundle again, that is a new revision. Your comments stay with the revision you made them on, so the agent can edit its files without losing them.

### `plan.json`

```json
{
  "schema_version": 1,
  "title": "Search the board by title",
  "summary": "Add a search overlay to the TUI and the web board.",
  "questions": [
    {
      "id": "scope",
      "kind": "single_choice",
      "prompt": "Should search include archived tasks?",
      "rationale": "Archived tasks are hidden everywhere else.",
      "required": true,
      "options": [
        {"id": "no", "label": "No", "description": "Only tasks on the board."},
        {"id": "yes", "label": "Yes"}
      ],
      "recommended_options": ["no"]
    }
  ]
}
```

- `schema_version` is `1`, and `title` is required. `summary` and `questions` are optional.
- Each question has an `id` that stays the same across revisions, a `kind` (`single_choice`, `multiple_choice` or `free_text`), a `prompt`, and optionally a `rationale`, `required`, `options` and `recommended_options`.
- Each option has a unique `id`, a `label` and an optional `description`.

### Diagrams and mockups

`plan.md` can hold two kinds of drawing as fenced blocks:

- a `mermaid` block, drawn as a diagram;
- a `wireframe` block, drawn as a [Wiremark](https://wiremark.dev) mockup of a screen.

The web UI draws both. Click one to see it larger. When a drawing cannot be drawn, you see its source instead. The TUI always shows the source.

Check a plan before you submit it:

```bash
legato plan validate .legato/plans/search/plan.md
```

It prints `Plan is valid.`, or lists every error with its line and exits with status `2`. It needs no task. The first time it runs, it uses Node and npm to install the Mermaid and Wiremark parsers into your user cache; when Node is missing or the install fails, it exits with status `6`. `legato plan submit` runs the same checks.

### Image files

A plan can show image files: a screenshot of the screen it changes, a mockup, a before and after. The Markdown is still what you approve and comment on; an image is a file the Markdown names.

```markdown
![The board today, with the header scrolled out of view](assets/board.png)
```

A path with no scheme names a file of the bundle. `legato plan submit` and `legato plan validate` refuse the plan, and name the file and the line, when the file:

- is missing, or is not inside the bundle (an absolute path, a path that climbs out, a link that leads out). Because a plan shows only files of its own bundle, the bundle is complete by itself and shows its images in any Markdown preview;
- is not PNG, JPEG, GIF, WebP or BMP by its content. The file name does not count. SVG is refused, because it is a document: use a `mermaid` or `wireframe` block for a drawing;
- is larger than 25 MiB, or is one of more than 60.

An image at a web address (`https://…`) is not a file of the plan. Your browser loads it from that address when you read the plan.

Legato keeps its own copy of each image, so the agent can change or delete the file afterwards. A completed plan keeps its images.

## The lifecycle

```text
draft files → proposed → changes_requested → proposed → approved → completed
                         └────────────────────→ rejected → reopened
```

1. The agent submits the plan. It is **proposed** and appears in your plan queue and in [Attention](/legato/using/attention/).
2. You answer its questions, comment, and ask it questions. You must answer every required question before you can approve.
3. You decide:
   - **Approve**: the agent may start. You can tick **Clean up plan files after implementation** to have the bundle folder removed when the work is done.
   - **Request changes**: your draft comments are sent, and the agent is told to read them. It submits a new revision, which is proposed again.
   - **Reject**: the agent must not implement the plan. You can **Reopen** it later and then approve it.
4. When the agent has implemented and checked the work, it runs `legato plan complete`. The plan leaves the active queue, but its revisions, comments, answers and conversation are kept. If you asked for cleanup, this command removes the bundle folder, and only that folder.

An agent can also withdraw a plan that is not yet approved, with `legato plan withdraw`.

## Reading a plan in the web UI

Open **Plans** in the navigation, or go to `/plans`. The queue lists the plans that need a decision. Open one to read it.

- **Questions.** Answer the agent's questions in the plan. You cannot approve until every required one has an answer.
- **Comments.** Select any text, a diagram or an image, and comment on it. A comment on a diagram or an image is about the whole block. Comments are drafts until you request changes. **General feedback** is for comments about the plan as a whole.
- **Q&A.** Ask the agent a question about the plan. It answers in the conversation beside the plan, without a new revision. When the agent is not running, the question is kept and you see a warning.
- **Images.** An image is drawn in place. Click it, or press Enter on it, to see it over the whole window, with the plan's other images to step through.
- **Decision.** Choose **Approve**, **Request changes** or **Reject**. A rejected plan offers **Reopen**.

## Reading a plan in the TUI

Press `P` on the board to open the plan queue. Select a plan with `j` and `k` and open it with Enter.

| Key | Action |
| --- | --- |
| `j`, `k` | Scroll |
| Tab | Select the next question |
| `e` | Answer the selected question |
| `c` | Comment on the plan |
| `?` | Ask the agent about the plan |
| `a` | Approve |
| `r` | Request changes |
| `x` | Reject |
| `o` | Reopen |
| Esc, `q` | Back to the queue |

The TUI comments on paragraphs or on the plan as a whole; it does not select arbitrary text as the web UI does. It shows diagrams and mockups as their source, and an image as its alt text and path.

## Commands

Agents use these; you rarely need them. The [CLI reference](/legato/reference/cli/) has every flag.

```text
legato plan validate <markdown-file>
legato plan submit <directory> [--task ID] [--name NAME] [--review-pass ID] [--finding ID]...
legato plan show --json [--task ID] [--name NAME]
legato plan feedback --json [--task ID] [--name NAME]
legato plan status --json [--task ID] [--name NAME]
legato plan answer <thread-id> (<markdown> | --file <path|->) [--task ID] [--name NAME]
legato plan claim --task <plan-task-id> [--name NAME]
legato plan inbox [--json]
legato plan complete [--task ID] [--name NAME]
legato plan withdraw [--task ID] [--name NAME]
```

- Inside an agent session, `--task` defaults to `LEGATO_TASK_ID` and `--name` to `LEGATO_PLAN_NAME`.
- `legato plan answer` answers a question you asked in Q&A. With `--file`, it reads the Markdown from a file, or from standard input with `--file -`, so the shell cannot change it. An answer read from a file can show image files: by an absolute path, or relative to the answer file's folder (the working directory for standard input). The same image checks apply, except that the file may be anywhere.
- `legato plan claim` hands an existing plan to the agent that runs it, and `legato plan inbox` prints the comments and questions waiting for that agent.

## Plans that come from a review

When you flag changes in a review and ask for a follow-up plan, the agent submits an ordinary plan, linked to the review:

```bash
legato plan submit <bundle> --review-pass <pass-id> --finding <finding-id>
```

It is approved in the same way. Completing it resolves those findings and starts the next pass of the review. See [Reviews](/legato/using/reviews/#findings-and-follow-up-plans).

Internals (storage, the API, how image copies are kept and removed) are in [docs/ai/plans.md](https://github.com/cpave3/legato/blob/main/docs/ai/plans.md) on GitHub.
