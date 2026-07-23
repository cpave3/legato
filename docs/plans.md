# Collaborative plans

Legato plans are implementation proposals that users can review before an agent changes code. They are distinct from swarm decomposition plans and post-implementation review tours.

## Bundle

Agents normally keep editable sources under `.legato/plans/<name>/`:

- `plan.md`: human-readable Markdown
- `plan.json`: versioned metadata and agent-authored questions

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
legato plan submit <directory> [--task ID] [--name NAME] [--review-pass ID] [--finding ID]...
legato plan show --json [--task ID] [--name NAME]
legato plan feedback --json [--task ID] [--name NAME]
legato plan status --json [--task ID] [--name NAME]
legato plan answer <thread-id> <markdown> [--task ID] [--name NAME]
legato plan complete [--task ID] [--name NAME]
legato plan withdraw [--task ID] [--name NAME]
```

`LEGATO_TASK_ID` and `LEGATO_PLAN_NAME` are environment fallbacks. Review-origin submissions also accept `LEGATO_REVIEW_PASS_ID` and comma-separated `LEGATO_REVIEW_FINDING_IDS`. CLI mutations broadcast `plan_changed` over the existing IPC bus.

Plans linked to review findings use the same proposal and approval lifecycle as standalone plans. Completing a linked plan resolves its findings and opens the next numbered review pass; see [Plan and review lineage](review-plan-lineage.md).

## Collaboration surfaces

The web UI exposes `/plans` and `/plans/:planId`. It renders Markdown, required choices, revision-linked comments, immediate Q&A, and approve/request-changes/reject controls. Selected rendered text is anchored to byte offsets and validated against the immutable revision.

The TUI opens the plan queue with `P`. It provides Markdown reading, option/text answers, general comments, Q&A, and verdict keys. The web UI supports arbitrary rendered-text selection; the terminal surface intentionally uses paragraph/general comments.

Q&A and verdict notifications use the same tmux send-keys mechanism as review tours. Messages are persisted even if the agent is offline; the web API returns a warning rather than losing the question.
