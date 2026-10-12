# Collaborative plans

Legato plans are implementation proposals that users can review before an agent changes code. They are distinct from swarm decomposition plans and post-implementation review tours.

## Bundle

Agents normally keep editable sources under `.legato/plans/<name>/`:

- `plan.md`: human-readable Markdown
- `plan.json`: versioned metadata and agent-authored questions
- image files that `plan.md` shows, by convention in `assets/` (see Image files)

`legato plan submit <directory>` validates both files and stores an immutable SQLite snapshot. Later submissions for the same task/name create revisions, so source edits cannot invalidate existing comments.

`plan.json` uses `schema_version: 1`, requires `title`, and optionally contains `summary` and `questions`. Questions have stable `id`, `kind` (`single_choice`, `multiple_choice`, or `free_text`), `prompt`, optional `rationale`, `required`, `options`, and `recommended_options`. Choice options have unique `id`, `label`, and optional `description`.

## Lifecycle

```text
draft files → proposed → changes_requested → proposed → approved
                         └────────────────────→ rejected → reopened
```

Required questions must be answered before approval. Approval can opt into **Clean up plan files after implementation**. Requesting changes submits current draft comments and notifies the agent to retrieve structured feedback. Rejection prevents implementation unless reopened and subsequently approved.

After successful implementation and verification, the agent runs `legato plan complete`. Completion archives the plan from active queues while retaining revisions, comments, responses, and transcript history. If cleanup was selected, the command validates and removes only the submitted bundle directory before recording completion.

## CLI

```text
legato plan validate <markdown-file>
legato plan submit <directory> [--task ID] [--name NAME] [--review-pass ID] [--finding ID]...
legato plan show --json [--task ID] [--name NAME]
legato plan feedback --json [--task ID] [--name NAME]
legato plan status --json [--task ID] [--name NAME]
legato plan answer <thread-id> (<markdown> | --file <path|->) [--task ID] [--name NAME]
legato plan complete [--task ID] [--name NAME]
legato plan withdraw [--task ID] [--name NAME]
```

`legato plan validate` is task-free and read-only. It checks Markdown structure plus case-insensitive `mermaid` and canonical `wireframe` fences. Valid files print `Plan is valid.`. Invalid files report all visual errors with source lines and exit `2`. The command uses the same pinned Mermaid and Wiremark parser versions as the web reader. On first use, Node and npm install these parsers into the user cache; later runs reuse the cache. A missing runtime or failed package install exits `6` with the dependency error. Plan submission runs the same Markdown visual checks before it stores a revision. Both also check the image files the Markdown names, against the folder the Markdown is in (see Image files).

`legato plan answer` takes its Markdown as an argument or, with `--file`, from a file or standard input. A file keeps Markdown from the shell, and an answer read from one can show image files (see Image files).

`LEGATO_TASK_ID` and `LEGATO_PLAN_NAME` are environment fallbacks for lifecycle commands. Review-origin submissions also accept `LEGATO_REVIEW_PASS_ID` and comma-separated `LEGATO_REVIEW_FINDING_IDS`. CLI mutations broadcast `plan_changed` over the existing IPC bus.

Plans linked to review findings use the same proposal and approval lifecycle as standalone plans. Completing a linked plan resolves its findings and opens the next numbered review pass; see [Plan and review lineage](review-plan-lineage.md).

## Image files

A plan can show image files: a screenshot of the screen it changes, a mockup, a before and after. The Markdown stays the document that is approved and commented on; an image is a file the Markdown names.

```markdown
![The board today, with the header scrolled out of view](assets/board.png)
```

**In `plan.md`.** A path with no scheme names a file of the bundle. `legato plan submit` and `legato plan validate` turn the plan down, with the file and the line, when the file:

- is missing, or is not inside the bundle (an absolute path, a path that climbs out, a link that leads out). A plan shows only files of its own bundle, so the bundle is whole by itself and shows its images in any Markdown preview;
- is not PNG, JPEG, GIF, WebP or BMP by its content. The name does not count. SVG is refused, because it is a document: use a `mermaid` or `wireframe` block for a drawing;
- is larger than 25 MiB, or is one of more than 60.

An image at a web address (`https://…`) is not a file of the plan. The browser loads it from that address when the plan is read, as before.

**In an answer.** `legato plan answer <thread-id> --file answer.md` can name image files too: by an absolute path, or relative to the answer file's folder (the working directory for standard input). The same checks apply, except that the file may be anywhere.

**What is stored** (`internal/service/document_images.go`). The Markdown is stored as the agent wrote it, so comment offsets hold. Beside it, the revision or the message records its images: the path as written, and the name of Legato's copy. The copy is at `<data_dir>/plan-images/<plan-id>/<sha256 of the content>.<ext>`, so the same image in ten revisions is one file. The folder is `0700` and a copy `0600`.

- A request that is turned down stores no plan and no copy.
- Copies are not removed by count. They stay while the plan stays, so a completed plan keeps its images.
- `legato plan withdraw` removes the plan's folder. Deleting a task removes its plans from the database and leaves their files; a web server removes those when it starts (`PlanService.RemoveOrphanImages`), with a copy that no revision and no answer names, and leaves alone what changed in the last hour.
- A plan ID that is not a plain name (a `--name` with spaces) gets a folder named by a hash of the ID.

**Serving.** `GET /plan-images/{folder}/{file}` gives out a copy, with the rules of `/agent-ui-images/` (see [Agent tools](ai/agent-mcp.md) § Image files): no token, because an `<img>` cannot send one and the address is the hash of the content; only its own copies, only raster content; `nosniff`, a sandboxing content security policy, `private, immutable` caching; `404` for anything else. The plan API names each image: `images: [{name, url}]` for the revision's Markdown and `message_images: {<message id>: [...]}` for answers, on `GET /api/plans/{id}`.

**Reading.** The web plan page draws an image from Legato's copy, at most 24 rem high, and opens it over the whole window on a click or with Enter (`ImageLightbox`), with the plan's other images to step through. A block that is one image has the comment control **Comment on image `<alt text>`**; the comment targets the image's Markdown, as a comment on a diagram targets its fence. An image whose copy is gone is named by its alt text. The TUI shows an image as its alt text and path.

**Reviews** keep image files the same way, under `<data_dir>/review-images/<tour-id>/` and `GET /review-images/{folder}/{file}`:

- Chapter narration is Markdown. `legato review chapter <title> --narration-file <path|-> --include …` and `legato review chapter edit <id> --narration-file <path|->` read it from a file, which keeps it from the shell. An image path in it is absolute or relative to that file's folder.
- `legato review answer <step-id> --file <path|->` shows image files the same way.
- The tour API names them on `GET /api/review/tours/{id}`: `step_images` by step ID, `message_images` by message ID, and `plan_images` by pass ID for the images of the plan revision a pass captured.
- Removing a tour removes its folder. An edited chapter leaves the copies its old narration named; the start-up sweep removes them (`ReviewService.RemoveOrphanImages`).
- A handoff packet keeps a chapter's narration as text and shows an image as its alt text.

Checked in a browser at 1500 px and 390 px against a scratch Legato: a plan with two images, an answer with one, a chapter with a before and after pair, the captured plan inside the review, and a review answer with an image. Every image loaded with no `Authorization` header, the larger view opened from a click and from the keyboard, Escape closed it and left the page, and a comment on an image was stored with the image's Markdown as its selected text.

## Collaboration surfaces

The web UI exposes `/plans` and `/plans/:planId`. It renders Markdown, required choices, revision-linked comments, immediate Q&A, and approve/request-changes/reject controls. Fenced `mermaid` blocks render as architecture diagrams, and canonical fenced `wireframe` blocks render as Wiremark UI mockups. Both visual types render in this reader and in captured plan context shown within a review tour, can be clicked to toggle a larger scrollable preview, and fall back to readable source when rendering fails. Selected rendered text is anchored to byte offsets and validated against the immutable revision. A comment placed on a diagram or mockup targets the complete original fenced Markdown block for agent feedback, not the generated SVG. Image files of the bundle are drawn in place and open full size (see Image files).

The TUI opens the plan queue with `P`. It provides Markdown reading, option/text answers, general comments, Q&A, and verdict keys. The web UI supports arbitrary rendered-text selection; the terminal surface intentionally uses paragraph/general comments. The TUI continues to show Mermaid and Wiremark blocks as fenced source rather than SVG visuals.

Q&A and verdict notifications use the same tmux send-keys mechanism as review tours. Messages are persisted even if the agent is offline; the web API returns a warning rather than losing the question.
