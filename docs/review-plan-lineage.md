# Plan and review lineage

Plans and review tours remain independent task artifacts. When both are used, Legato records the links between agreed scope, review feedback, and follow-up implementation.

## Plan context in reviews

A new review pass snapshots the most recently approved or completed plan for its task. The exact plan revision and Markdown are retained with the pass, so later plan revisions do not change the scope shown to the reviewer. Reviews still work when no approved plan exists.

Review chapters, annotations, messages, and findings belong to a numbered pass. The web review page groups older passes separately and authors new chapters only in the active pass.

## Findings and follow-up plans

Reviewers can flag a whole chapter or selected diff lines as a requested change. Findings are durable and separate from review Q&A.

“Request follow-up plan” persists a request before notifying the task agent, so an offline agent does not lose the feedback. The agent submits a normal approval-gated plan with its origin:

```text
legato plan submit <bundle> --review-pass <pass-id> --finding <finding-id>
```

`LEGATO_REVIEW_PASS_ID` and comma-separated `LEGATO_REVIEW_FINDING_IDS` are environment fallbacks. The plan page and task artifact API retain these origin links. Completing the linked plan resolves its findings and starts the next review pass, which snapshots that completed plan for comparison.

## Regeneration versus follow-up passes

Explicit regeneration replaces the active pass, preserving the tour's repository capture boundary but removing the stale active-pass artifacts. Feedback is required in the web UI and supported by the CLI:

```text
legato review restart --feedback "Focus on error handling" [--task ID] [--name NAME]
```

This differs from completing a linked follow-up plan: that workflow supersedes the earlier pass and creates a new numbered pass so the review iteration remains visible.

## Task history

`GET /api/tasks/{id}/artifacts` returns all plans and review tours for a task, including completed, rejected, reviewed, and superseded history plus typed lineage edges. Active plan and review queues remain limited to actionable work. The web task detail panel loads and displays this history only when the panel is opened.
