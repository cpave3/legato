## Review tours: internals

The user guide is [Reviews](../../site/src/content/docs/using/reviews.md) (on the site at `/legato/using/reviews/`). This file keeps the model and the API behind it. The CLI verbs are in [CLI](cli.md); the web page in [Web UI](web-ui.md) § Review tours and § Review briefings on the branch; image files of narration and answers in [Plans](plans.md) § Image files; review handoff packets in [Review handoff](review-handoff.md).

Agents are told to capture tours only while `agents.reviews` is on (the default); with it off, the review blocks of the role and general prompts are left out and every launch prompt says to skip reviews unless asked. See [Configuration](config.md) § Config.

### The tour model

`internal/service/review.go` (`ReviewService`). A task has a default tour and any number of named ones (`--name`, `LEGATO_REVIEW_NAME`); `EnsureReviewTour` creates one on first use.

- **Capture.** `AgentService` calls `ReviewService.BeginCapture` before spawning an agent whose working directory is a Git repository: it records `HEAD` as the tour's `base_sha` (once) and the repository path. For a task with a Yggdrasil worktree and a base branch, `Sync` uses the merge-base with that branch instead. A legacy session with no base starts at `HEAD`.
- **Sync** (`legato review sync`, and before `ready` and `complete`) inserts one `commit` step per commit ahead of the base (a `Legato-Subtask:` trailer links a swarm sub-task), marks steps of commits no longer reachable as orphaned, and keeps one `dirty` step, **Uncommitted changes**, while the worktree has changes (re-flagged unreviewed when its fingerprint changes). New work on a `reviewed` tour puts it back to `ready`.
- **Chapters** (`kind = chapter`) group hunks across files and commits (`--include <path>:<hunk>`, 1-based). Annotations (`legato review annotate`) attach to a commit step, a file (a `note` step), or a hunk and line range with content-derived anchors (`rhn-…` IDs), so unrelated line shifts keep them attached.
- **Ready** syncs, freezes `HEAD` into the tour and the active pass, and rebuilds the generated **Remaining changes** chapter (risk `unsure`) from hunks no authored chapter covers. Statuses: `capturing` → `ready` → `reviewed`.
- **Complete** marks every step reviewed, records `last_reviewed_sha`, and sets the tour and the pass to `reviewed`.
- Events: `review_changed` (IPC from the CLI, `EventReviewChanged` in process, the WebSocket message of the same name) with a `tour_id` and a kind (`synced`, `chapter`, `annotated`, `ready`, `reviewed`, …).
- Agents receive questions as `[legato review] Question on step <id> …` messages with the quoted-heredoc `legato review answer <step-id> --file -` command; `legato review claim` and `inbox` share the plan service's claim and interaction queue.

### Plan and review lineage

Plans and review tours stay independent task artifacts; when both are used, Legato records the links between agreed scope, review feedback and follow-up implementation.

- A new review pass snapshots the most recently approved or completed plan for its task: the exact revision and Markdown are kept with the pass, so later plan revisions do not change the scope shown. The revision's image files are drawn with it while the plan exists (`plan_images` by pass ID on `GET /api/review/tours/{id}`). Reviews work without a plan.
- Chapters, annotations, messages and findings belong to a numbered pass. The web page groups older passes separately and authors new chapters only in the active pass.
- A finding (`ReviewService.CreateFinding`) flags a whole chapter or selected diff lines as a requested change. Findings are durable and separate from Q&A.
- **Request follow-up plan** (`RequestPlan`) persists the request before notifying the task agent, so an offline agent does not lose it. The agent submits a normal plan with `--review-pass` and `--finding` (or `LEGATO_REVIEW_PASS_ID` and comma-separated `LEGATO_REVIEW_FINDING_IDS`). The plan page and the task artifact API keep those origin links. Completing the linked plan resolves its findings and starts the next pass (`AdvancePass`), which snapshots that completed plan.
- **Regeneration** (`RestartWithFeedback`; `legato review restart [--feedback …]`, feedback required in the web UI) replaces the active pass, keeping the tour's repository capture boundary and removing the stale active-pass artifacts. A completed follow-up plan instead supersedes the earlier pass and creates a new numbered one, so the iteration stays visible.
- `GET /api/tasks/{id}/artifacts` returns every plan and review tour of a task, completed, rejected, reviewed and superseded ones included, with typed lineage edges. The active plan and review queues show only actionable work. The web task detail panel loads this history only when the panel is opened.

### Queue and removal

`POST /api/review/tours/bulk-delete` validates every selected tour and removes all their review artifacts in one transaction; a missing tour leaves the whole selection untouched. Removal keeps tasks, repositories, worktrees, agent records and Git history, and deletes the tour's review handoff history and image copies. A completed tour stays out of the queue until new commits or annotations arrive; those unreviewed steps then appear beside the chapters already reviewed. The Attention `reviews` producer lists tours marked ready with visible unreviewed work ([Attention](attention.md)).
