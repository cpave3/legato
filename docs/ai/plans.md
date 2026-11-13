## Collaborative plans: internals

The user guide is [Plans](../../site/src/content/docs/using/plans.md) (on the site at `/legato/using/plans/`). This file keeps what an agent working on Legato's code needs and a user does not: storage, the API, service names, and how the image files are kept and swept. The CLI verbs are in [CLI](cli.md) § Collaborative plan verbs; the web page in [Web UI](web-ui.md) § Collaborative plans.

Legato plans are distinct from swarm decomposition plans (`legato swarm propose-plan`, [Swarm](swarm.md)) and from review tours.

### Storage and revisions

`legato plan submit <directory>` validates `plan.md` and `plan.json` and stores an immutable SQLite snapshot (`plan_revisions`). A later submission for the same task and name is a new revision, so editing the source bundle never invalidates existing comments: comments are anchored to byte offsets in the revision they were made on, and the server checks a selection against that revision.

`plan.json` is `schema_version: 1`, requires `title`, and may contain `summary` and `questions`. A question has a stable `id`, `kind` (`single_choice`, `multiple_choice` or `free_text`), `prompt`, optional `rationale`, `required`, `options` and `recommended_options`. A choice option has a unique `id`, a `label` and an optional `description`.

Lifecycle statuses: `proposed`, `changes_requested`, `approved`, `rejected` (reopened back to proposed), and `completed`. Approval stores the cleanup choice (`PlanService.Approve(ctx, planID, cleanupAfterImplementation)`); `legato plan complete` then validates the bundle path (`validatePlanBundleForCleanup`) before removing it and records `cleaned_up`.

CLI mutations broadcast `plan_changed` over the IPC bus; the web server forwards it as the `plan_changed` WebSocket message. Q&A and verdict notifications reach the agent by the same delivery as review tours (`[legato plan] …` messages through the agent service). Messages are persisted even when the agent is offline; the web API then answers with a warning rather than losing the question. `legato plan claim` and `legato plan inbox` use the shared artifact claim and interaction queue (`ArtifactClaim`, `ArtifactInteraction`) that review tours also use.

**Reading by task.** `GET /api/tasks/{id}/plans` (`PlanService.TaskPlans`) lists a task's plans newest first, whatever their status, each as a `PlanQueueItem` (`plan_id`, `name`, `title`, `summary`, `status`, `revision`, `unanswered_required`, `updated_at`); `404` for a task that does not exist. `GET /api/plans/{plan_id}` reads one: `revision.markdown`, `plan.status`, `plan.latest_revision`. Both are classed `read`, so the Reeve reads them without asking. A plan an agent submitted on a worker instance stays in the worker's database: the main does not see the plans of a task while it is away.

Review-origin submissions record `--review-pass` and `--finding` (or `LEGATO_REVIEW_PASS_ID`, comma-separated `LEGATO_REVIEW_FINDING_IDS`) as the plan's origin. `GET /api/tasks/{id}/artifacts` returns the lineage edges; see [Reviews](reviews.md).

### Visual checks

`legato plan validate` and `submit` check Markdown structure plus case-insensitive `mermaid` and canonical `wireframe` fences with the same pinned Mermaid and Wiremark parser versions as the web reader. On first use Node and npm install those parsers into the user cache; later runs reuse it. A missing runtime or a failed install exits `6` with the dependency error; invalid visuals exit `2` and list every error with its source line. The web reader renders both lazily on plan detail and in a captured plan inside a review; other Markdown surfaces do not enable Wiremark. A comment on a diagram or mockup targets the complete original fenced block, not the generated SVG.

### Image files

`internal/service/document_images.go` keeps the image files that the Markdown of plans and reviews names. It finds them with `planvalidate.Images`, copies them by content hash, lists, opens and sweeps them for `PlanService` and `ReviewService`.

**What is stored.** The Markdown is stored as the agent wrote it, so comment offsets hold. Beside it, the revision or the message records its images (the `images` column, migration 065; see [Database](database.md)): the path as written and the name of Legato's copy. The copy is at `<data_dir>/plan-images/<plan-id>/<sha256 of the content>.<ext>`, so the same image in ten revisions is one file. The folder is `0700` and a copy `0600`.

- A request that is turned down stores no plan and no copy.
- Copies are not removed by count. They stay while the plan stays, so a completed plan keeps its images.
- `legato plan withdraw` removes the plan's folder. Deleting a task removes its plans from the database and leaves their files; a web server removes those when it starts (`PlanService.RemoveOrphanImages`), with any copy that no revision and no answer names, and leaves alone what changed in the last hour.
- A plan ID that is not a plain name (a `--name` with spaces) gets a folder named by a hash of the ID.

**Serving.** `GET /plan-images/{folder}/{file}` gives out a copy with the rules of `/agent-ui-images/` (see [Agent tools](agent-mcp.md) § Image files): no token, because an `<img>` cannot send one and the address is the hash of the content; only its own copies, only raster content; `nosniff`, a sandboxing content security policy, `private, immutable` caching; `404` for anything else. `GET /api/plans/{id}` names each image: `images: [{name, url}]` for the revision's Markdown and `message_images: {<message id>: [...]}` for answers.

**Reading.** The web plan page draws an image from Legato's copy, at most 24 rem high, and opens it over the whole window on a click or with Enter (`ImageLightbox`), with the plan's other images to step through. A block that is one image has the comment control **Comment on image `<alt text>`**; the comment targets the image's Markdown, as a comment on a diagram targets its fence. An image whose copy is gone is named by its alt text. The TUI shows an image as its alt text and path.

**Reviews** keep image files the same way, under `<data_dir>/review-images/<tour-id>/` and `GET /review-images/{folder}/{file}`:

- Chapter narration is Markdown. `legato review chapter <title> --narration-file <path|-> --include …` and `legato review chapter edit <id> --narration-file <path|->` read it from a file, which keeps it from the shell. An image path in it is absolute or relative to that file's folder.
- `legato review answer <step-id> --file <path|->` shows image files the same way.
- `GET /api/review/tours/{id}` names them: `step_images` by step ID, `message_images` by message ID, and `plan_images` by pass ID for the images of the plan revision a pass captured.
- Removing a tour removes its folder. An edited chapter leaves the copies its old narration named; the start-up sweep removes them (`ReviewService.RemoveOrphanImages`).
- A review handoff packet keeps a chapter's narration as text and shows an image as its alt text.

Checked in a browser at 1500 px and 390 px against a scratch Legato: a plan with two images, an answer with one, a chapter with a before and after pair, the captured plan inside the review, and a review answer with an image. Every image loaded with no `Authorization` header, the larger view opened from a click and from the keyboard, Escape closed it and left the page, and a comment on an image was stored with the image's Markdown as its selected text.
