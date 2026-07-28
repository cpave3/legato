# Workflows

Workflows are reusable, ordered recipes that combine deterministic Legato operations with agent assignments. Each run records typed inputs, step progress, attempts, outputs, artifacts, and associated agent sessions.

## Definition locations

Legato discovers YAML definitions from:

1. `$XDG_CONFIG_HOME/legato/workflows` (normally `~/.config/legato/workflows`)
2. `.legato/workflows` in the current project

Legato also reads `~/.legato/workflows` for compatibility. Precedence is legacy global, XDG global, then project-local. A later definition overrides an earlier definition with the same `name`. The selected source path is stored on every run.

## Repository presets

Frequently used repository/path pairs can be configured once and selected from web workflow and agent launch forms:

```yaml
workflows:
  repositories:
    - name: Legato
      repo: cpave3/legato
      primary_dir: /home/cameron/Projects/legato
      base_branch: main
```

For workflows, the preset control appears only when a definition declares at least one of `repo`, `primary_dir`, or `base_branch`. Selecting a preset prefills only those declared inputs; undeclared preset values are never submitted.

The web dialogs for spawning an ephemeral agent or opening an agent for an existing task also list these presets. Selecting one copies its `primary_dir` into the working-directory field. The field remains editable, and a manual edit switches the selector to **Custom** without discarding the entered path.

## Schema version 1

Version 1 executes steps in declaration order and supports four executors:

- `github.pr.prepare`: deterministically fetch an exact GitHub PR head, create a Yggdrasil worktree, and link the workflow task to the PR.
- `agent.run`: launch a configured Legato agent with an explicit assignment and completion contract.
- `worktree.remove`: validate and remove the shared task's worktree through Yggdrasil, then clear task metadata.
- `task.note`: add Markdown to a selected task. Pass a resolved `task` input plus either inline `body` or a managed/file `body_path`. Local notes are stored in Legato and shown in task detail; Jira-backed notes are also published as Jira comments. Provider failures fail the step rather than silently dropping the note.

```yaml
schema_version: 1
name: review-pr
title: Review pull request
description: Prepare and review a GitHub pull request.
inputs:
  repo:
    type: string
    required: true
  pr_number:
    type: integer
    required: true
steps:
  - id: prepare
    title: Prepare exact PR head
    type: github.pr.prepare
    with:
      repo: ${{ workflow.inputs.repo }}
      number: ${{ workflow.inputs.pr_number }}
      primary_dir: /path/to/local/repository
      base_branch: main
    outputs:
      worktree_path: {type: string}
      head_sha: {type: string}
      metadata: {type: json}

  - id: review
    title: Run three-tier review
    type: agent.run
    with:
      agent: chimera
      tier: large
      working_dir: ${{ steps.prepare.outputs.worktree_path }}
      prompt: |
        Run the `3-tier-review` skill against this pull request. Do not modify
        or publish to the PR. Write the consolidated review to
        `${{ outputs.review.path }}` and report structured findings.
    outputs:
      review:
        type: file
        path: review.md
        media_type: text/markdown
      findings:
        type: json
```

Inputs support `string`, `integer`, `boolean`, `json`, and `task`. A task input is supplied as an existing Legato task ID and resolves to structured `id`, `title`, `description`, `status`, `priority`, `provider`, and `remote_id` fields. The web form groups selectable tasks by workspace and hides Done and archived tasks by default; enable **Show done and archived tasks** in the selector to include them. Outputs support scalar/JSON types plus `file`. Steps default to `trigger: automatic`; set `trigger: manual` to pause the run before that executor starts. A manual step creates no attempt until explicitly triggered with `legato workflow trigger <run-id> <step-id>`. While waiting, the run is non-terminal and contributes to the Workflows attention badge. Unknown fields, unsupported executors, unsafe paths, forward references, and undeclared references fail validation before a run starts.

Templates support only:

- `${{ workflow.inputs.<key> }}`
- `${{ workflow.inputs.<task-key>.<field> }}`
- `${{ steps.<earlier-step>.outputs.<key> }}`
- `${{ steps.<earlier-step>.outputs.<file-key>.path }}`
- `${{ outputs.<current-file-output>.path }}`

Whole-field references preserve native types. Values interpolated into text use scalar or canonical JSON formatting.

A typical task-triage tail writes the agent's managed report back to the selected task:

```yaml
inputs:
  task: {type: task, required: true}
  primary_dir: {type: string, required: true}
steps:
  - id: triage
    title: Investigate task
    type: agent.run
    with:
      working_dir: ${{ workflow.inputs.primary_dir }}
      prompt: Write actionable triage for ${{ workflow.inputs.task.title }} to `${{ outputs.report.path }}`.
    outputs:
      report: {type: file, path: triage.md, media_type: text/markdown}
  - id: note
    title: Add triage note
    type: task.note
    with:
      task: ${{ workflow.inputs.task }}
      body_path: ${{ steps.triage.outputs.report.path }}
    outputs:
      note_id: {type: string}
      task_id: {type: string}
```

## Running and inspecting

```bash
legato workflow list --json
legato workflow validate review-pr --json
legato workflow run review-pr \
  --input repo=owner/repository \
  --input pr_number=123 \
  --json
legato workflow runs --status running --json
legato workflow show <run-id> --json
legato workflow trigger <run-id> <step-id> [--force] --json
legato workflow retry <run-id> <step-id> [--force] --json
legato workflow cancel <run-id> --json
```

For a `worktree.remove` step, `--force` forwards Yggdrasil's force option and discards uncommitted worktree changes. It can be selected when first triggering a manual removal or when retrying a failed removal; other step types reject it.

A run creates one shared local execution task. Unless an explicit workspace is supplied, Legato places it in an automatically managed **Workflows** workspace. When the run succeeds, the execution task moves to **Done**; failed or cancelled runs remain in their current column for intervention. When a workflow accepts a `task` input, the selected target is stored separately as `related_task_id`; agent sessions continue to use the execution task so an existing task's active agent session is not replaced. Deterministic steps execute synchronously until an agent step starts. The run then advances when that agent explicitly completes or fails its step.

## Agent completion

Workflow agents receive these environment variables:

```text
LEGATO_WORKFLOW_RUN_ID
LEGATO_WORKFLOW_STEP_ID
LEGATO_WORKFLOW_ATTEMPT_ID
```

Complete a step using declared output names:

```bash
legato workflow step complete \
  --output findings='[{"severity":"high","message":"..."}]' \
  --artifact review=review.md \
  --json
```

Report an unrecoverable failure:

```bash
legato workflow step fail "review tool unavailable" --json
```

Agent or tmux exit alone never means success. Missing or invalid outputs leave the step incomplete or failed. Completion is persisted and downstream steps are advanced before Legato removes the completed agent session, because the callback may be running inside that session. Managed artifact collection is replay-safe: rerunning an interrupted completion callback reuses an existing `(attempt, output)` artifact only when its source path and digest still match; changed content is rejected.

## Artifacts and security

File outputs must match the definition's declared path and resolve beneath the agent attempt's working directory. Legato rejects absolute paths, traversal, symlink escapes, directories, and files larger than the current 10 MiB limit. Accepted files are copied atomically into managed Legato storage with a SHA-256 digest so worktree cleanup does not destroy them.

Workflow JSON outputs have a 10 MiB size limit and maximum depth of 32; input JSON is type-checked but does not yet apply those limits. Definition templates are intentionally not a general expression or shell language. The deterministic PR executor uses an exact head SHA and head repository identity, including fork PRs, rather than trusting a branch name.

Workflow prompts may ask agents to invoke local skills and tools. Remote mutation or publication is not a native workflow operation; definitions must explicitly authorize any such agent behavior.

## Web UI

The **Workflows** destination includes:

- reusable definitions and validation errors;
- generated typed-input launch forms;
- active and historical runs;
- ordered step status, resolved inputs, outputs, and errors;
- deterministic PR/worktree details;
- all agent attempts and live terminal links;
- managed artifacts with Markdown preview/download;
- retry and cancellation controls;
- manual-step actions, including an expected-path confirmation before Yggdrasil worktree removal.

Board task detail also shows related workflow runs and their downloadable managed artifacts when the run selected that task through a `task` input.

Run views update through `workflow_changed` and agent activity WebSocket events. An unfinished `agent.run` step is marked as needing attention when its session becomes waiting, idle, or disappears. The Workflows navigation item shows a red count, run cards explain the stall, and the run page offers **Prompt to continue** for a live stalled agent. Legato does not prompt automatically, avoiding duplicate or unsafe agent work. Mobile layouts use stacked step cards, contained JSON/code scrolling, touch-sized actions, and safe-area navigation spacing.

## Extension model

Workflow persistence and UI are executor-neutral. Future deterministic executors can implement the service executor contract and render type-specific details without replacing runs, steps, attempts, outputs, or artifacts. DAGs, parallel execution, schedules, shell plugins, approval gates, and native external publishers are not part of schema version 1.
