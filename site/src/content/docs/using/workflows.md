---
title: Workflows
description: Reusable recipes of Legato steps and agent assignments, with typed inputs, outputs and artifacts.
sidebar:
  order: 70
---

A workflow is a recipe you write once in YAML and run many times: a list of steps that Legato carries out in order. Some steps are fixed operations, such as checking out a pull request, moving a task or adding a note to it. Others hand an assignment to an agent and wait for it to report its results. Use a workflow for work you repeat, such as reviewing a pull request or triaging a new task, so that every run is done the same way and leaves a record.

Each run keeps its inputs, the progress of every step, each attempt, the outputs and files the steps produced, and the agents that worked on it. You start a run from the command line, the web UI or a board card, or let Legato start it when a task enters a column.

## Definition locations

Legato discovers YAML definitions from:

1. `$XDG_CONFIG_HOME/legato/workflows` (normally `~/.config/legato/workflows`)
2. `.legato/workflows` in the current project

Legato also reads `~/.legato/workflows` for compatibility. Precedence is legacy global, XDG global, then project-local. A later definition overrides an earlier definition with the same `name`. The selected source path is stored on every run.

## Repository presets

Repositories you use often can be configured once, and then chosen in the web UI's workflow and agent forms:

```yaml
repositories:
  - name: Legato
    repo: cpave3/legato
    primary_dir: /home/cameron/Projects/legato
    base_branch: main
```

You can also add repositories in the web UI, under Settings › Agents › Repositories. An older configuration may list them under `workflows.repositories`; Legato reads that list only when the top-level `repositories` list is absent. See the [configuration reference](/legato/reference/configuration/).

For older workflows, the preset control appears when a definition declares at least one of `repo`, `primary_dir`, or `base_branch`, and fills in only those fields. New workflows can declare one typed `repository` input instead. It accepts an existing preset name or Custom values (`repo`, `primary_dir`, and optional `base_branch`) and exposes the resolved snapshot as `${{ workflow.inputs.repository.* }}`.

The web dialogs for spawning an agent, or opening an agent for an existing task, offer each preset that has a `primary_dir` as a tile beside the directories you saved. Picking one puts its folder in the path field, which you can still edit.

## Schema version 1

Version 1 executes steps in declaration order and supports these executors:

- `github.pr.prepare`: deterministically fetch an exact GitHub PR head, create a Yggdrasil worktree, and link the workflow task to the PR.
- `agent.run`: launch a workflow-owned Legato agent with an explicit assignment and completion contract. Set `with.name` to customize the agent's visible name; workflow input references are supported. The agent is removed after completing its step, unless `with.keep_agent: true`: then its outputs are collected and the run moves on as usual, but the agent keeps running on the workflow task, for you to go on talking to. Follow it with a step with `trigger: agent_ended` to end the run (and remove a worktree) when that agent ends. Set `with.engram` to start the agent as one of your [engrams](/legato/agent-tools/engrams/), such as `engram: mneme` or `engram: ${{ workflow.inputs.engram }}`; without it the agent has none. A step whose engram engram does not know fails before the agent starts. Workflows do not use or change the engrams bound to a repository.
- `agent.dispatch`: send a prompt to the selected task's ordinary durable agent, or spawn that task-linked agent in `with.working_dir` when none is active. The workflow does not own or tear down this session. Outputs include `agent_session_id`, `created`, and `working_dir`.
- `agent.stop`: idempotently stop the active agent for the resolved `with.task`. Built-in Learn does not use it because interviews borrow the original task session; curator and critic agents run under the separate generated workflow task.
- `worktree.remove`: validate and remove the shared task's worktree through Yggdrasil, then clear task metadata.
- `file.remove`: idempotently remove one regular file beneath an explicit working directory. Absolute paths, traversal, symlink escapes, and directories are rejected.
- `chimera.session.remove`: remove the exact Chimera session retained by the selected task, then clear its resume and native binding data. The action is idempotent when no session exists, refuses a running agent or a parent session with children, and returns `removed` and `metadata_cleared` booleans. Use an explicit `agent.stop` step first when the workflow should stop an active agent.
- `task.context`: render the selected task's full Markdown context, including structured fields, effective provider/local description, and task notes, as the `content` output.
- `task.working_dir.resolve`: choose a selected task's linked worktree when present, otherwise use the required `with.fallback` repository path. Outputs are `path` and `source` (`worktree` or `repository`).
- `task.note`: add Markdown to a selected task. Pass a resolved `task` input plus either inline `body` or a managed/file `body_path`. Notes are stored locally in Legato and shown in task detail, including for provider-backed tasks; they are not published to Jira.
- `task.metadata.set`: atomically set one or more dotted local metadata paths on a task. Provider sync preserves this metadata.
- `task.move`: move the required structured `with.task` to the required non-empty, exact case-sensitive configured column named by `with.column`. A real move appends the task to the end of that column and uses normal transitions, events, and column-entry triggers; use trigger conditions to avoid authoring loops. Moving to the current column is an idempotent local no-op with no new transition or event. With a ticket provider such as Jira configured, Legato may still queue the mapped Jira transition, so a remote push that failed earlier can recover. A successful step means the move is saved locally and queued, not that Jira has accepted it.
- `repo.ensure`: prepare what a job works in. With an empty `with.repo` it does nothing and outputs `working_dir` as given in `with.working_dir`. With a repository (`owner/name`) it makes Legato's cache of it ready (cloning or fetching with the `github` credential, so a repository Legato cannot read fails here) and outputs `repo`, `ref` and `branch` for the agent step; giving both `repo` and `working_dir` is refused. `agent.run` with `with.repo` (and `with.ref`, `with.branch`) starts its agent in a fresh clone made for the agent's execution, on its own branch.
- Built-in Learn definitions additionally own `agent.interview`, `learning.packet.prepare`, and `learning.proposals.submit`. They are internal orchestration contracts rather than general-purpose user workflow examples; see [Legato Learn](/legato/using/learn/).

Task inputs expose `${{ workflow.inputs.task.metadata.<path> }}`. Metadata is arbitrary JSON, local to Legato, and separate from provider-owned `remote_meta`. `${{ workflow.inputs.task.description }}` is effective Markdown: local tasks receive their description unchanged, while provider-backed tasks receive provider Markdown followed by editable local content under `## Legato details`.

Definitions may set `run_title` to distinguish individual runs while keeping `title` as the reusable workflow label:

```yaml
title: Review pull request
run_title: Review PR #${{ workflow.inputs.pr_number }}
```

`run_title` supports declared `workflow.inputs` references, including expanded task fields and metadata, but not step outputs or `${{ workflow.task }}`. The intrinsic workflow task does not exist until after the run title resolves, so using it here is rejected as circular. The title resolves after typed input and task/repository expansion and must produce a non-empty string. The resolved value is stored on the run and used for its Workflows execution task; omitting it preserves the definition `title`.

Set `after_start` to control web navigation after a manual launch: `workflow` (the default) opens the run, `stay` closes the dialog without changing pages, and `agent` opens Agents focused on the selected task. `agent` requires exactly one required task input. This is presentation-only; it does not change workflow execution or automatic trigger behavior.

Workflow definitions do not support `notify_on_complete`. Successful runs remain in workflow history and do not create Attention items; Attention is reserved for manual gates, waiting agents, and failures. Remove this field from older workflow YAML because schema parsing is strict.

### Run from a board card

Press `c` on a selected TUI board card, or right-click a web board card, and open **Workflows** to list the definitions with exactly one `task` input. On a web card you can also use its action button, from the keyboard or on a touch screen. The input key can have any name and receives the selected card automatically. Definitions with zero task inputs are unrelated to the card, while definitions with multiple task inputs are ambiguous, so neither is shown. Column-entry triggers do not exclude an otherwise eligible workflow.

Declared defaults and unambiguous repository context are prefilled. The form collects remaining string, integer, boolean, JSON, and repository values, honors choices, masks secret values, and leaves validation to the normal workflow start path. Discovery and launch failures remain visible in the menu/form for correction.

### Column-entry triggers and conditions

A workflow may remain manually runnable while also declaring an automatic local column-entry trigger:

```yaml
on:
  task.entered_column:
    column: Triaged
    inputs:
      task: ${{ event.task }}
      repository: ${{ task.metadata.repository }}
    conditions:
      all:
        - field: task.metadata.triaged
          not_equals: true
```

Conditions support nested `all`, `any`, and `not`, and field comparisons using `exists`, `equals`, `not_equals`, or `in`. In addition to task snapshot, metadata, and transition fields, `task.has_active_agent` resolves at trigger time to whether the task has an open agent session. An open session includes working, waiting, idle, and error activity states; completed/dead sessions do not match. `task.has_chimera_session` resolves from the task's current retained Chimera resume ID. Both dynamic fields are re-evaluated when a confirmed trigger starts.

Set `confirm: true` on a column trigger to require explicit approval after its conditions match and inputs resolve. Existing triggers without it continue to start automatically. Confirmable requests remain recoverable in Attention: closing the immediate prompt means **Not now** and makes no state change, while **Skip** terminates the request. Conditions are re-evaluated when the user confirms, so a session that closes while the prompt is pending causes the trigger to skip without creating a workflow run.

Automatic triggers run for explicit local moves, including local moves of provider-backed tasks; pull-sync status changes do not trigger workflows. Trigger requests are durable and idempotent. Missing required inputs leave the card in its destination column and create a recoverable Attention item; for confirmable triggers, supplying those inputs advances to confirmation instead of starting immediately. The workflow can always be started manually as a separate run.

For example, this user-defined workflow offers to stop an open task agent and remove its retained Chimera session when the task enters Done. Put it in a discovered workflow directory; it is not built into Legato. Confirmation is recommended because session deletion cannot be undone. A parent session with child or fork sessions fails safely and retains its resume data until the children are removed.

```yaml
schema_version: 1
name: close-active-agent
title: Close agent and remove Chimera session
description: Stop the task agent and remove retained Chimera history when a task moves to Done.
inputs:
  task: {type: task, required: true}
on:
  task.entered_column:
    column: Done
    confirm: true
    inputs:
      task: ${{ event.task }}
    conditions:
      any:
        - field: task.has_active_agent
          equals: true
        - field: task.has_chimera_session
          equals: true
steps:
  - id: close
    title: Close active agent
    type: agent.stop
    with:
      task: ${{ workflow.inputs.task }}
  - id: remove-session
    title: Remove Chimera session
    type: chimera.session.remove
    with:
      task: ${{ workflow.inputs.task }}
    outputs:
      removed: {type: boolean}
      metadata_cleared: {type: boolean}
```

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
      name: PR Review - ${{ workflow.inputs.pr_number }}
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

Inputs support `string`, `integer`, `boolean`, `json`, `task`, and `repository`. A task input is supplied as an existing Legato task ID and resolves to structured `id`, `title`, `description`, `status`, `priority`, `provider`, `remote_id`, `working_dir`, and local `metadata` fields. A repository input accepts a configured preset name or Custom JSON and resolves to `name`, `repo`, `primary_dir`, and `base_branch`. The web form groups selectable tasks by workspace and hides Done and archived tasks by default; enable **Show done and archived tasks** in the selector to include them. Outputs support scalar/JSON types plus `file`. File output paths support declared workflow input references and resolve separately for each run; for example, `path: "task-triage-${{ workflow.inputs.task.id }}.md"` prevents concurrent task runs from sharing a source file. File outputs infer a media type from known filename extensions when `media_type` is omitted, including `text/markdown` for `.md` and `.markdown`; unknown or extensionless files use `application/octet-stream`. Declare `media_type` when the format is known—an explicit value always overrides inference. Steps default to `trigger: automatic`; set `trigger: manual` to pause the run before that executor starts. A manual step creates no attempt until explicitly triggered with `legato workflow trigger <run-id> <step-id>`. While waiting, the run is non-terminal and contributes to the Workflows attention badge. Set `trigger: agent_ended` to wait the same way for the agent of the workflow task (`${{ workflow.task }}`, such as one started by `agent.dispatch`) to end: when that agent is killed or exits, the step starts by itself, and it can still be triggered by hand. A restart (**Reload agent**) is not an end. A run that reaches such a step with no agent running goes straight on, a Legato that starts finds and starts steps whose agent ended while it was not running, and a run waiting on a running agent does not count toward the Workflows attention badge. Unknown fields, unsupported executors, unsafe paths, forward references, and undeclared references fail validation before a run starts.

Templates support only:

- `${{ workflow.task }}`, `${{ workflow.task.<field> }}`, and `${{ workflow.task.metadata.<path> }}` for the generated workflow execution task
- `${{ workflow.inputs.<key> }}`
- `${{ workflow.inputs.<task-key>.<field> }}` and `${{ workflow.inputs.<task-key>.metadata.<path> }}`
- `${{ workflow.inputs.<repository-key>.<name|repo|primary_dir|base_branch> }}`
- `${{ steps.<earlier-step>.outputs.<key> }}`
- `${{ steps.<earlier-step>.outputs.<file-key>.path }}`
- `${{ outputs.<current-file-output>.path }}`

Whole-field references preserve native types. Values interpolated into text use scalar or canonical JSON formatting. `${{ workflow.task }}` is intrinsic and is not declared or supplied as an input; it has the same task fields as a declared task input and is reloaded before each step so mutable fields reflect earlier changes. It identifies the generated execution task, while `${{ workflow.inputs.task }}` identifies a workflow's selected subject.

A task-triage workflow can remain manually runnable while also reacting to a local column entry:

```yaml
inputs:
  task: {type: task, required: true}
  repository: {type: repository, required: true}
on:
  task.entered_column:
    column: Triaged
    inputs:
      task: ${{ event.task }}
      repository: ${{ task.metadata.repository }}
    conditions:
      all:
        - field: task.metadata.triaged
          not_equals: true
steps:
  - id: triage
    title: Investigate task
    type: agent.run
    with:
      working_dir: ${{ workflow.inputs.repository.primary_dir }}
      prompt: Write actionable triage for ${{ workflow.inputs.task.title }} to `${{ outputs.report.path }}`.
    outputs:
      report: {type: file, path: "task-triage-${{ workflow.inputs.task.id }}.md", media_type: text/markdown}
  - id: note
    title: Add triage note
    type: task.note
    with:
      task: ${{ workflow.inputs.task }}
      body_path: ${{ steps.triage.outputs.report.path }}
  - id: mark-triaged
    title: Mark task triaged
    type: task.metadata.set
    with:
      task: ${{ workflow.inputs.task }}
      values: {triaged: true}
  - id: cleanup
    title: Remove triage report
    type: file.remove
    with:
      working_dir: ${{ workflow.inputs.repository.primary_dir }}
      path: "task-triage-${{ workflow.inputs.task.id }}.md"
  - id: ready
    title: Move task to Ready
    type: task.move
    with:
      task: ${{ workflow.inputs.task }}
      column: Ready
```

Keep the move last: if investigation, note publication, metadata, or cleanup fails, the subject remains in its original column.

A workflow can hold a conversation: `agent.dispatch` on the workflow task starts an agent the workflow does not wait on, and an `agent_ended` step cleans up after it. The person ends the conversation by ending the agent, or with the step's trigger on the run page, which stops the agent first:

```yaml
steps:
  - id: prepare
    title: Prepare pull request
    type: github.pr.prepare
    with: {repo: ${{ workflow.inputs.repo }}, number: ${{ workflow.inputs.pr_number }}, primary_dir: ${{ workflow.inputs.primary_dir }}}
    outputs:
      worktree_path: {type: string}
  - id: discuss
    title: Start the discussion agent
    type: agent.dispatch
    with:
      task: ${{ workflow.task }}
      working_dir: ${{ steps.prepare.outputs.worktree_path }}
      prompt: Get across this pull request's change set, then answer questions about it.
  - id: end
    title: End discussion
    type: agent.stop
    trigger: agent_ended
    with: {task: ${{ workflow.task }}}
  - id: cleanup
    title: Remove worktree
    type: worktree.remove
    with: {expected_path: ${{ steps.prepare.outputs.worktree_path }}}
```

## The agent job workflow

Legato ships one workflow of its own, `legato-job`: one agent on one prompt, with a durable run record. It is listed with the workflows on disk; a definition of yours named `legato-job` replaces it.

| Input | Required | Meaning |
| --- | --- | --- |
| `prompt` | yes | What the agent is asked to do |
| `agent` | no | The agent tool (`claude-code`, `codex`, `chimera`); the default agent tool when empty. A name that is not a configured agent tool is refused when the run starts, and no run is created |
| `working_dir` | no | The directory the agent works in; Legato's default directory for agents when empty |
| `repo`, `ref`, `branch` | no | A GitHub repository (`owner/name`), the branch, tag or commit the job starts from (the default branch when empty), and the branch it works on (`legato/<task id>` when empty, which the GitHub credential lets agents push; GitHub's branch of that name is checked out when it exists). The agent works in a fresh clone made for it at `<data_dir>/jobs/<execution>/<name>`, which borrows the objects of Legato's cache of the repository; give this or `working_dir`, not both. Needs a `github` credential that lists the repository |

A job's clone is kept for `repository_cache.keep_idle` (7 days) after an agent last worked in it, so the task's next agent, or the next part of a stack, can work in it again; then it is removed if every branch in it is pushed; one with commits that were never pushed is kept for seven days, with a note on the task that says where it is and when it goes. Legato checks for such clones every five minutes.

It runs `repo.ensure`, then `agent.run` with `keep_agent: true`: the agent completes the step with a `summary` output and stays running, so you can read its answer in the native panel and go on talking to it. Start it like any workflow:

```bash
legato workflow run legato-job --input prompt="Find why the sync test is flaky" --input working_dir=/srv/legato --json
legato workflow run legato-job --input prompt="Find why the sync test is flaky" --input repo=cpave3/legato --json
```

Another program can start it through the web server's API too; see the internals linked at the foot of this page.

The check on `agent` applies to every workflow: an `agent.run` step whose `agent` is known from the inputs alone and names no configured agent tool is refused when the run starts.

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

Each run makes one task of its own, the execution task, in the **Workflows** workspace, which Legato manages. The **All** view of the board leaves these tasks out; switch to the **Workflows** workspace to see the runs in progress. When a run succeeds, its execution task moves to **Done** and is archived at once. A run that failed or was cancelled stays in its column, for you to deal with.

The execution task is where the run's agents work, and `${{ workflow.task }}` and `LEGATO_TASK_ID` name it. When a workflow takes a `task` input, the task you chose is the run's subject, recorded beside the run; the run's agents still work on the execution task, so an agent already running on your task is not replaced. The run shows on that task's detail.

Fixed steps run one after another until a step hands work to an agent. The run moves on when that agent reports that it completed or failed its step.

## Agent completion

Workflow agents receive these environment variables:

```text
LEGATO_TASK_ID
LEGATO_WORKFLOW_RUN_ID
LEGATO_WORKFLOW_STEP_ID
LEGATO_WORKFLOW_ATTEMPT_ID
```

`LEGATO_TASK_ID` is the generated workflow execution task ID and is also printed in the assignment completion contract.

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

An agent that exits, or whose terminal closes, has not succeeded: only `legato workflow step complete` completes a step. Missing or invalid outputs leave the step incomplete or failed. Learn's interview follows the same rule through `legato learn reflection submit`; see [Legato Learn](/legato/using/learn/). Legato saves the outputs and moves the run on before it removes the agent. If a completion is interrupted and the agent runs the command again, a file that has not changed is accepted again, and a file that changed is refused.

## Artifacts and security

File outputs must match the definition's resolved declared path and resolve beneath the agent attempt's working directory. Legato rejects absolute paths, traversal, symlink escapes, directories, and files larger than the current 10 MiB limit. Accepted files are copied atomically into managed Legato storage with a SHA-256 digest so worktree cleanup does not destroy them.

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

A board task's detail also shows the runs that took it as their `task` input, with the files they produced to download.

Runs update on the page as they progress. When the agent of an unfinished `agent.run` step stops to wait for you, goes idle or disappears, the step is marked as needing attention: the Workflows item in the navigation shows a red count, the run's card says why it stalled, and the run page offers **Prompt to continue** while the agent is still running. Legato never prompts the agent by itself, so it cannot start the same work twice.

## Limits

Schema version 1 runs steps one after another. It has no branching graphs of steps, no parallel steps, no schedules, no shell plugins, no approval gates beyond manual steps, and no steps that publish to outside services.

Internals (the HTTP API, how runs and artifacts are stored, how the web UI is kept up to date) are in [docs/ai/workflows.md](https://github.com/cpave3/legato/blob/main/docs/ai/workflows.md) on GitHub.
