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
2. `.legato/workflows` in the checkout of each [repository preset](#repository-presets) that has a `primary_dir`
3. `.legato/workflows` in the current project

Legato ships example workflows; `legato workflow add` lists them, and `legato workflow add <name>` writes one to your global folder (or `.legato/workflows` with `--project`), keeping a file of that name unless you add `--force`:

```bash
legato workflow add review-pr-passes
legato workflow run review-pr-passes --input repo=acme/widget --input pr_number=12
```

The templates Legato ships use your default agent tool and your [macros](/legato/using/agents/#macros), need no extra tools, and arrive with every trigger paused (`enabled: false`), so nothing runs until you switch it on:

| Template | What it does |
| --- | --- |
| `triage` | When a card enters **Triage**, an agent triages it in the repository's checkout; the triage becomes a note, the task is marked triaged and moves to **Ready**. Skips a task already triaged. Needs a repository preset with a `primary_dir`. |
| `review-pr` | Reviews a pull request (`repo`, `pr_number`) in a fresh clone with your **Review** macro, then stays open for your questions. The run ends when the agent does. |
| `discuss-pr` | An agent in a fresh clone gets across a pull request's change set and answers your questions about it. |
| `address-feedback` | When someone requests changes on a pull request one of your tasks owns, gives the task's agent your **Address feedback** macro, starting it if none is running. |
| `review-pr-passes` | Reviews a pull request in passes posted to GitHub; see [Review passes](/legato/using/review-passes/). |
| `learn` | When a card enters **Review**, asks the card's running agent what it learned; a curator and a critic agent turn that into candidate learnings on the **Learning** page. See [Legato Learn](/legato/using/learn/#setting-it-up). |
| `ticket-to-merged-pr` | Takes a card (`task`) from its ticket to a merged pull request: the card's own agent works in a fresh clone of `repo` on your `macro` (**Implement** unless you give another; it must open a pull request) and stays, the pull request is reviewed in passes while that agent fixes the findings, then the run waits for the merge, closes the card's Burnlite ticket (a card not from Burnlite is skipped) and stops the agent. A paused trigger offers to start it when a card enters **Doing**. Needs review passes, GitHub access, and Burnlite connected with write access for the close. |

In the web UI, **New workflow** on the **Workflows** tab (and the tab itself when you have no workflows) shows the templates: each says what starts it, what it asks for and what it does. Choose one to see its outline, then **Use this template** to open it in the editor. Nothing is written until you press **Create**, in the folder you pick (yours, the project's, or a repository's checkout). If a workflow already has the template's name, the new one gets another.

The browser extension starts `review-pr` and `discuss-pr` with the repository and number only; a workflow of yours of the same name that declares a `primary_dir` input still gets the matching repository preset's folder.

Legato also reads `~/.legato/workflows` for compatibility. Precedence is legacy global, XDG global, each repository's folder in the order the presets are listed, then project-local. A folder that is more than one of these is read once, at its highest precedence. A later definition overrides an earlier definition with the same `name`. The Workflows tab shows only the one that wins, with a note that it replaces the others. The selected source path is stored on every run.

## Repository presets

Repositories you use often can be configured once, and then chosen in the web UI's workflow and agent forms:

```yaml
repositories:
  - name: Legato
    repo: cpave3/legato
    primary_dir: /home/cameron/Projects/legato
    base_branch: main
```

You can also add repositories in the web UI, under Settings › Repositories. An older configuration may list them under `workflows.repositories`; Legato reads that list only when the top-level `repositories` list is absent. See the [configuration reference](/legato/reference/configuration/).

For older workflows, the preset control appears when a definition declares at least one of `repo`, `primary_dir`, or `base_branch`, and fills in only those fields. New workflows can declare one typed `repository` input instead. It accepts an existing preset name or Custom values (`repo`, `primary_dir`, and optional `base_branch`) and exposes the resolved snapshot as `${{ workflow.inputs.repository.* }}`.

The web dialogs for spawning an agent, or opening an agent for an existing task, offer each preset that has a `primary_dir` as a tile beside the directories you saved. Picking one puts its folder in the path field, which you can still edit.

## Schema version 1

Version 1 executes steps in declaration order and supports these executors. Legato also reads files with `schema_version: 2`, which say how a step starts with `start:` instead of `trigger:` (see [Schema version 2](#schema-version-2)):

- `github.pr.prepare`: deterministically fetch an exact GitHub PR head, create a Yggdrasil worktree, and link the workflow task to the PR.
- `agent.run`: launch a workflow-owned Legato agent with an explicit assignment and completion contract. Set `with.name` to name the agent in lists of agents (workflow input references are supported). It names that agent only: the run and its task keep the run's title. The agent is removed after completing its step, unless `with.keep_agent: true`: then its outputs are collected and the run moves on as usual, but the agent keeps running on the workflow task, for you to go on talking to. Follow it with a step with `trigger: agent_ended` to end the run (and remove a worktree) when that agent ends. Set `with.engram` to start the agent as one of your [engrams](/legato/agent-tools/engrams/), such as `engram: mneme` or `engram: ${{ workflow.inputs.engram }}`; without it the agent has none. A step whose engram engram does not know fails before the agent starts. Workflows do not use or change the engrams bound to a repository.
- `agent.run` works in `with.working_dir`, in a fresh clone of `with.repo`, or in a scratch folder. A folder is for persistent work: a repository or project the agent changes, which outlives the run. Work that is about no folder, such as a daily digest, a summary or a check of a dashboard, sets `with.scratch: true` instead. Each attempt then works in a new, empty folder of its own (`<data_dir>/workflow-scratch/<attempt>`), which the sandbox shows the agent, as it shows a job clone. Legato removes the folder when the step completes. A step that fails, or keeps its agent, keeps its folder for a look, and Legato removes it a week later once nothing works in it. A retry gets a new folder. Nothing left in the folder is kept, so the step's result goes in its declared outputs: an artifact output copies a file out before the folder goes, and the agent's brief says so. `scratch` cannot be given with `working_dir` or `repo`.

  ```yaml
  - id: check-news
    title: Write the AI news digest
    type: agent.run
    with:
      prompt: Write a short digest of today's AI news.
      scratch: true
    outputs:
      digest: {type: string}
  ```
- `agent.run` with `with.task` (a task input or a task ID) runs the agent as that task's own agent instead of the workflow's: it shows on the task's card, reads that task's context, keeps its stage on the card, and its conversation can be resumed like any agent on the card. With `with.repo` it works in a fresh clone on the task's branch (`legato/<task>`), as an agent started on the card would; a live clone of the task is used again. A macro is sent as one started on the card is: the task's key and title, then the macro's text. The step still waits for the agent to complete it with its outputs, and removes the agent then unless `keep_agent` is set. A step whose task already has an agent running fails without touching it: stop that agent first, or message it with `agent.dispatch`. `task` cannot be given with `working_dir`, `scratch` or `name`.
- `agent.run` with `with.task` and `with.slot` runs the agent in a named slot of that task's card, such as `triage`, `plan` or `review`, beside the card's own agent instead of in its place. It shows on the card and in the agents list and graph as "<card> · triage", keeps its stage there, and is removed after its step like any other, unless `keep_agent` is set. With `with.repo` it gets a fresh clone of its own on the branch `legato/<task>-<slot>`, so it never works in the card's agent's clone or pushes the card's branch, and the card's next agent never works in its clone. It leaves the card's repository and branch as they were. A step whose slot already has an agent running fails without touching it. A slot is named with lowercase letters, digits and dashes, starts with a letter, and is at most 32 characters long. It is written as it is, with no `${{ }}`, and is checked when the file loads. `main`, the default, is the card's own agent, and `reviewer` is kept for review passes. `slot` needs `task`. For example:

  ```yaml
  - id: triage
    title: Triage
    type: agent.run
    with:
      task: ${{ workflow.inputs.task }}
      slot: triage
      repo: ${{ workflow.inputs.repo }}
      prompt: Triage this task and write what you find in a note.
  ```

  ```yaml
  - id: implement
    title: Implement the ticket
    type: agent.run
    with:
      task: ${{ workflow.inputs.task }}
      macro: Implement
      repo: acme/widget
      keep_agent: true
    outputs:
      pr_number: {type: integer}
  ```
- `agent.dispatch`: send a prompt to the selected task's ordinary durable agent, or spawn that task-linked agent in `with.working_dir` (where agents start by default when empty) when none is active. With a macro, `with.repo` and `with.pr_number` name the pull request in the message. The workflow does not own or tear down this session. Outputs include `agent_session_id`, `created`, and `working_dir`.
- Both `agent.run` and `agent.dispatch` take what to ask either as `with.prompt` or as `with.macro`, the name of one of your [macros](/legato/using/agents/#macros), not both. A macro is read when the step runs, so an edit applies to the next run. A step that names no `agent` or `tier` uses the macro's. `agent.dispatch` sends it as a macro started on the task would be: the task's key and title, then the macro's text.

  ```yaml
  - id: review
    title: Review
    type: agent.dispatch
    with:
      task: ${{ workflow.inputs.task }}
      macro: Review
      working_dir: ${{ steps.clone.outputs.working_dir }}
  ```
- `agent.stop`: idempotently stop the active agent for the resolved `with.task`. The `learn` template does not use it, because its interview borrows the card's own agent; its curator and critic run under the run's own task.
- `agent.compact`: compact the running agent of `with.task` with its tool's own compaction, as `/compact` in its panel does (see [Compacting an agent](/legato/using/agents/#compacting-an-agent)). `${{ workflow.task }}` is the agent an `agent.run` step with `keep_agent: true` left running. `with.instructions` says what the summary must keep; only Claude Code takes them. An agent still in its turn is compacted when the turn ends, which is the usual case right after an `agent.run` step, since the agent completes that step from inside its turn. The step then waits for the compaction to finish. Outputs: `tokens_before` and `tokens_after`, when the tool reports them. The step fails at once when the task has no agent running, when Legato cannot compact the agent's tool or shows the agent only in its terminal, or when a Codex or Chimera agent is given instructions. It fails when the compaction fails, and when 15 minutes after the step started the agent is still working or Legato has seen no end to the compaction (for example because Legato restarted meanwhile); **Retry step** compacts again.

  ```yaml
  - id: compact
    title: Compact before the review
    type: agent.compact
    with:
      task: ${{ workflow.task }}
      instructions: Keep the plan, the decisions and the failing tests
  ```
- `worktree.remove`: validate and remove the shared task's worktree through Yggdrasil, then clear task metadata.
- `file.remove`: idempotently remove one regular file beneath an explicit working directory. Absolute paths, traversal, symlink escapes, and directories are rejected.
- `chimera.session.remove`: remove the exact Chimera session retained by the selected task, then clear its resume and native binding data. The action is idempotent when no session exists, refuses a running agent or a parent session with children, and returns `removed` and `metadata_cleared` booleans. Use an explicit `agent.stop` step first when the workflow should stop an active agent.
- `task.context`: render the selected task's full Markdown context, including structured fields, effective provider/local description, and task notes, as the `content` output.
- `task.working_dir.resolve`: choose a selected task's linked worktree when present, otherwise use the required `with.fallback` repository path. Outputs are `path` and `source` (`worktree` or `repository`).
- `task.note`: add Markdown to a selected task. Pass a resolved `task` input plus either inline `body` or a managed/file `body_path`. Notes are stored locally in Legato and shown in task detail, including for provider-backed tasks; they are not published to Jira. On a task that is away on a worker, the note is kept here and the step still succeeds, but its outputs say so (`away_on`, `warning`): the worker's agent will not see it. `task` may also be a task ID as text, such as another step's output. With `optional: true`, a step whose task is empty adds no note and succeeds, with `skipped: true` in its outputs; without it, an empty task fails the step.
- `task.metadata.set`: atomically set one or more dotted local metadata paths on a task. Provider sync preserves this metadata. A task that is away on a worker is read-only, so the step fails with "away on <worker>".
- `task.move`: move the required structured `with.task` to the required non-empty, exact case-sensitive configured column named by `with.column`. A real move appends the task to the end of that column and uses normal transitions, events, and column-entry triggers; use trigger conditions to avoid authoring loops. Moving to the current column is an idempotent local no-op with no new transition or event. With a ticket provider such as Jira configured, Legato may still queue the mapped Jira transition, so a remote push that failed earlier can recover. A successful step means the move is saved locally and queued, not that Jira has accepted it.
- `github.pr.review_passes`: run a [review loop](/legato/using/review-passes/#review-any-pull-request) on an open pull request (`with.repo`, `with.pr_number`): passes posted as GitHub reviews from the reviewer App until a clean one approves it. The step waits on the loop rather than on an agent, and outputs `outcome`, `passes`, `findings`, `reviews`, `summary_path` and `linked_task`. It is the one step that publishes to an outside service. The `review-pr-passes` example uses it. `with.max_passes` and `with.reviewer` (`tool` or `tool:tier`) override this Legato's review-pass settings for the loop; empty, the setting holds. `with.scope` is `stack` (the default: every open pull request of the stack the named one is in, in one loop) or `pr` (that pull request alone).
- `github.pr.wait`: wait until a pull request (`with.repo`, `with.pr_number`) merges. Legato reads the pull request from GitHub when the step starts, so one that merged already goes straight on, then after each refresh of a card's pull requests and every 2 minutes while the board or `legato serve` runs. With `until: merged` (the default), a pull request closed without merging fails the step. With `until: closed`, a merge or a close completes it. Outputs: `state` (`merged` or `closed`), `merged`, `merged_at` and `url`. The step needs Legato's GitHub access (see [GitHub](/legato/integrations/github/)). A pull request that Legato cannot read when the step starts fails the step, and **Retry step** reads it again. A read that fails later, while the step waits, is tried again at the next check.

  While it waits, the step asks you to merge. [Attention](/legato/using/attention/) and the needs-you rail show **Review and merge PR #n** on the card, with **Open PR** and **Resume agent…**, and the run counts toward the Workflows badge. When the pull request conflicts with its base or its CI fails, the item says so (**PR #n conflicts with main**, **CI failing**) and comes back if you had dismissed it. The item goes away when the pull request merges or closes. `with.task` names the card (a task input, or a task ID); without it, the card is the run's task input. `with.notify: false` turns the item off.

  **Resume agent…** picks a macro from those for tasks and for pull requests, your favourites first and the rest behind **Show all**, and starts the card's own agent with it, such as **Rebase and repush** when the pull request conflicts. The agent resumes its conversation in its clone. If that clone is gone, the agent starts a new conversation in a fresh clone of the pull request's branch, and a note on the task says so. While the card's agent runs, the action is **Send a macro…**: the macro becomes the agent's next message. While the agent waits for you (a permission prompt or a question), the item offers only **Open agent**. The run goes on waiting for the merge. An agent that **Resume agent…** started is stopped when the pull request merges or closes; one that you started yourself is left running. The card stays in its column and its ticket is not changed.

  ```yaml
  - id: merged
    title: Wait for the merge
    type: github.pr.wait
    with:
      repo: acme/widget
      pr_number: ${{ steps.implement.outputs.pr_number }}
      task: ${{ workflow.inputs.task }}
  ```
- `burnlite.task.close`: close the Burnlite ticket a task was imported from (`with.task`), as `completed` (the default) or `not_planned` (`with.reason`), with a one-line `with.summary`. With `optional: true`, a task that is not a Burnlite ticket is skipped (`skipped: true`) instead of failing the step. Outputs: `key`, `closed` and `skipped`. Legato must be connected to Burnlite with write access; see [Burnlite](/legato/integrations/burnlite/#close-a-ticket-from-a-workflow).
- `repo.ensure`: prepare what a job works in. With an empty `with.repo` it does nothing and outputs `working_dir` as given in `with.working_dir`. With a repository (`owner/name`) it makes Legato's cache of it ready (cloning or fetching with the `github` credential, so a repository Legato cannot read fails here) and outputs `repo`, `ref` and `branch` for the agent step; giving both `repo` and `working_dir` is refused. `agent.run` with `with.repo` (and `with.ref`, `with.branch`) starts its agent in a fresh clone made for the agent's execution, on its own branch.
- `agent.interview`, `learning.packet.prepare` and `learning.proposals.submit`: the steps of [Legato Learn](/legato/using/learn/), for your own workflows too. See [Learn steps](#learn-steps) below.

Task inputs expose `${{ workflow.inputs.task.metadata.<path> }}`. Metadata is arbitrary JSON, local to Legato, and separate from provider-owned `remote_meta`. `${{ workflow.inputs.task.description }}` is effective Markdown: local tasks receive their description unchanged, while provider-backed tasks receive provider Markdown followed by editable local content under `## Legato details`.

Definitions may set `run_title` to distinguish individual runs while keeping `title` as the reusable workflow label:

```yaml
title: Review pull request
run_title: Review PR #${{ workflow.inputs.pr_number }}
```

`run_title` supports declared `workflow.inputs` references, including expanded task fields and metadata, but not step outputs or `${{ workflow.task }}`. The intrinsic workflow task does not exist until after the run title resolves, so using it here is rejected as circular. The title resolves after typed input and task/repository expansion and must produce a non-empty string. The resolved value is stored on the run and used for its Workflows execution task. Without `run_title`, a run is named by the definition's `title`, with ` · <card>` added when the workflow takes a `task` input: the card's ticket key, such as `LEG-603`, or its ID for a local card. An `agent.run` step's `with.name` never renames the run.

Set `after_start` to control web navigation after a manual launch: `workflow` (the default) opens the run, `stay` closes the dialog without changing pages, and `agent` opens Agents focused on the selected task. `agent` requires exactly one required task input. This is presentation-only; it does not change workflow execution or automatic trigger behavior.

### Learn steps

The steps of [Legato Learn](/legato/using/learn/) work in any workflow, so you can use part of it: a reflection with no curation, say, or a curation on a column of your own. The `learn` template uses all three, with two `agent.run` steps (the curator and the critic) between the packet and the submit; read it for a full example (`legato workflow add learn`).

- `agent.interview`: ask a task's running agent (`with.task`, a task or a task ID) what it learned, with Legato's reflection prompt or your own `with.prompt`. The agent answers with `legato learn reflection submit`, and each observation becomes evidence on the task. The step waits for that answer. It fails when the task has no agent running; it never starts or stops one. Outputs: `available` and `request_id`. When the question cannot be delivered to the agent, the step succeeds at once with `available: false` and the `reason`, and there is nothing new to curate.
- `learning.packet.prepare`: gather the task's evidence and the learnings it may touch into a bounded packet for a curator (`with.task`; `with.limits` with `max_items` and `max_bytes`; optional `with.project` with `key` and `label`, found from the evidence otherwise). Outputs: `generation_id`, `packet_digest` and `packet` (JSON), which a curator's prompt quotes.
- `learning.proposals.submit`: check a curator's operations (`with.operations`) against the packet (`with.generation`, the packet step's `generation_id`) and keep them as candidates. It accepts nothing for you. Outputs: `status`, `replayed` and `summary`.

A workflow whose steps include any of these is a Learn run: its own task is marked `legato.learning_workflow`, and the `learn` template's trigger skips such tasks, so a run never starts another. Use the same condition in a trigger of your own.

A reflection alone, kept as evidence for a later `legato learn curate`:

```yaml
schema_version: 1
name: reflect
title: Keep a reflection
inputs:
  task: {type: task, required: true}
on:
  task.entered_column:
    column: Done
    enabled: false
    inputs: {task: "${{ event.task }}"}
    conditions:
      field: task.metadata.legato.learning_workflow
      not_equals: true
steps:
  - id: interview
    title: Ask what it learned
    type: agent.interview
    with:
      task: ${{ workflow.inputs.task }}
```

### Writing workflows in your own editor

Legato publishes a JSON Schema of version 1 workflow files at <https://cpave3.github.io/legato/schemas/workflow-v1.schema.json>. A running Legato serves the same schema at `GET /api/workflow-schema`. An editor with the YAML language server, such as VS Code with the Red Hat YAML extension, completes keys, checks values and shows each field's description when the file starts with:

```yaml
# yaml-language-server: $schema=https://cpave3.github.io/legato/schemas/workflow-v1.schema.json
schema_version: 1
```

The schema knows each step type's `with` fields. It cannot check `${{ }}` references or the rules that depend on earlier steps, so run `legato workflow validate <file>` as well. A `with` key that a step's type doesn't read is flagged by the schema and is a warning from `legato workflow validate`. Legato ignores such keys for now, and a later release will refuse them, so remove any you find, such as a misspelt `promt:`.

Workflow definitions do not support `notify_on_complete`. Successful runs remain in workflow history and do not create Attention items; Attention is reserved for manual gates, waiting agents, and failures. Remove this field from older workflow YAML because schema parsing is strict.

### Runs and workflows in the web UI

**Flows** opens on **Runs**, with the number of runs that need you on its tab. Runs in progress come first, those that need you at the top, then finished runs, newest first. Each run says its state (**Running**, **Needs you**, **Done**, **Failed** or **Cancelled**), the task it is for, what it is doing in a sentence, and a bar with one segment per step that shows how far through it is and where it stopped.

A run's page says the same at the top, and under it what you can do now:

- an agent at work: **Open agent**, and **Show on agents graph**, which opens the [agents graph](/legato/using/agents/#the-graph) on that agent with this run's graph open
- an agent that went idle or is waiting for an answer: **Open agent** and **Prompt to continue**
- a manual step: what it will do (such as "Moves LEG-42 to Ready") and **Run step**
- a step that waits for the agent to end: **Open agent**, **Show on agents graph**, and **End now** to run the step without waiting
- a failed step: its error and **Retry step**
- a finished run: the files it produced, with **Preview** and **Download**, and the values its agents reported
- a finished run whose agent step finished with no output: **No output**, and what became of the agent. An agent step's conversation is not kept: when the step completes, Legato removes its agent, and anything it wrote in chat goes with it, unless `keep_agent` kept it. The run's row in **Runs** says so quietly too, after how long it took, and `legato workflow show` gives it as `output_warning` on the step and the run.

Above the graph, **Needs you** gathers everything the run waits on you for, drawn as in the [needs-you rail](/legato/using/attention/), each with its action:

- a step that waits for you: **Start**, or **End now** for one that waits for its agent to end
- an agent that went idle or is waiting: **Open agent** and **Prompt to continue**
- a permission request or question from one of the run's agents: its choices (**Allow once**…) and **Deny…**; the agent's step shows **Needs you** while it is open
- what the run's agents posted for you, with its actions
- a review loop waiting on its author, or stuck: **Run another pass**
- a failed step: **Retry**

Click a row to centre the graph on its step and select it. The list is hidden when nothing waits on you, and keeps up as approvals and items come and go.

On a wide screen the steps are drawn as the run's **workflow graph**, which updates as the run moves while its nodes stay in place. Each step says its state in a word with an icon and a colour (**Waiting**, **Running**, **Needs you**, **Failed**, **Done** or **Skipped**), how long it has run, its attempts folded into a count (**×3**) and the agent tool. The review loop shows its pass (**Pass 2 of 5**). The trigger that started the run is marked **Started this run**, and the others are dimmed. Select a step to see, beside the graph, its inputs and outputs, every attempt with its times, error, agent tool and tier, folder and **Open agent**, its files, a review loop's passes with a link to each review, and the step's YAML. The graph is the run's own copy of the workflow: when the file has changed since, the page says **This workflow has changed since this run**, or **This workflow's file is gone**. On a phone the steps are listed one to a line with how long each took; open one to see its inputs, outputs and agent. **Graph** above the list shows the graph in its place, drawn top to bottom, and a step's details open in a sheet from the bottom of the screen; close it with ✕ or a tap outside. **Cancel run** stops a run in progress.

The **Workflows** tab lists each workflow with its steps (✋ marks one that waits for you or for its agent to end), what it takes, when it starts on its own (a column trigger), and how its last run went. The values of secret inputs are never shown. **Run** opens the form for its inputs. Flows remembers the tab you used last.

### A workflow's page

Select a workflow's title on the **Workflows** tab to open its page. It shows the workflow's title, description and file, and its **workflow graph**: the workflow drawn as nodes, built from its YAML. The graph is read-only: **Edit** opens the workflow's file in the editor (see [Create and edit in the web UI](#create-and-edit-in-the-web-ui)).

- **Run by hand** is the node every workflow starts from when you run it yourself.
- Each trigger the workflow declares is a node too, leading into the first step: when it fires (**Card enters Triage**, **Every weekday at 09:00**, **Changes requested on a PR**), whether it is **on here** or **paused here**, and its loop stop (**3/day**).
- Each step is a node with an icon, its kind (**Agent**, **Send to agent**, **Note**, **Move card**, **Review in passes**…), its title and the one value that says what it does: the prompt's first line or the macro, the column, the repository or pull request.
- Arrows lead from one step to the step that runs after it.
- A **Review in passes** step is drawn as a box holding its loop: **Review pass**, then **Author fixes**, and an arrow back round to the next pass, until a pass approves.
- A **gate** is a step that does not start by itself. It carries a mark: **you** (✋) for a step that waits for you to start it, **agent ends** (⏳) for one that waits for the agent before it to end.
- Select a step to see which steps feed it: dashed arrows, labelled with the outputs used, lead from each step whose outputs it uses, and from it to each step that uses its own. They are hidden while nothing is selected, so the graph stays readable.

Drag to pan, scroll or use the buttons at the bottom left to zoom, and the frame button to fit the whole graph. The overview at the bottom right shows where you are in a long workflow. Click a node, or move to it with Tab and press Enter, to select it. On a phone the graph runs top to bottom, and what the side panel shows opens in a sheet from the bottom of the screen.

Select a step to see its own YAML in the panel beside the graph, with **Copy** and the file it is in, to find and reuse its exact definition. The values of secret inputs are hidden there too.

Select a trigger to see its settings in the panel beside the graph: whether it asks before each run, its loop stop, what its file says, and the trigger as its YAML declares it. Its switch turns it on or pauses it on this Legato, as on the **Workflows** tab; a schedule also lists its next due times and its missed runs.

**Run** opens the form for its inputs, and **Recent runs** lists that workflow's last ten runs. A file that can't be parsed shows its error instead of a graph.

### Create and edit in the web UI

**New workflow** on the **Workflows** tab starts from one of four places: a template, **Blank** (nothing yet: add the first step), **Paste YAML** (it says what you pasted is, such as "Starts when a card enters Review · asks for 2 inputs · 3 steps", and marks its problems), or **Copy one of yours**. Each opens the editor, unsaved. Pick the folder it goes in (yours, a repository's checkout or the project's) and press **Create**. **Edit** on a workflow's page opens its file in the same editor, as the file says it, comments and all. **Save** writes exactly what you typed.

The editor has two sides, **Visual** and **YAML**, and you can switch between them at any time without losing work: both edit the same file. The YAML is coloured in your theme's own syntax colours (Settings › Appearance), so keys, strings and comments read on a dark theme as well as a light one.

- **Visual** shows the workflow as an outline, with the part you choose open beside it.
  - The outline reads from top to bottom: **When** (running by hand, which every workflow can, then its triggers, a paused one marked), **Asks for** (its inputs), **Then** (its steps in order) and **About** (its name, title, description, run title, and what the web UI opens after a run starts).
  - Choose a row to edit it on the right. A step has three tabs: **Setup** (its ID, how it starts, and the fields of its type), **Outputs** (the outputs it declares) and **YAML** (that step's own YAML). A trigger and an input have **Setup** and **YAML**. A change in the YAML tab changes only that part of the file, and is written once it is valid YAML.
  - The row you have open is in the page's address (such as `#steps/review`), so a reload or Back keeps it.
  - **Add trigger**, **Add input** and **Add step** add one and open it. A new trigger starts paused. Renaming an input changes every `${{ workflow.inputs.<name> }}` that reads it, and the triggers' mappings of it.
    - A step's **Setup** shows the fields that matter most first. The rest are folded under **More options**, whose heading names those that are set. Switches read as sentences, the agent tool is a choice of yours, and long text such as a prompt has a **Larger** editor. **How it starts** (right after, when you start it, or when the agent ends) and the step's ID, which later steps use to read its outputs, are at the foot. **Outputs** lists what later steps can read: what the step's type gives, then the outputs it declares.
    - Each step has its ID, title and how it starts, then the fields of its type: text, a prompt, switches, numbers, choices, your macros, agent tools and tiers, board columns, and the workflow's task or a task input. The fields that matter most come first, each named in plain words (such as **What it should do** or **Works in**) with its name in the file beside it. **Prompt** or **Macro** is a choice. **Insert** beside a field, or typing `${{` in it, lists the values it can read: this run's inputs and task, what started the run, and the outputs of each earlier step, by its title. The one you choose goes in where your cursor is. A value of the wrong type for the field is dimmed, saying why. Under the field, each `${{ }}` it holds is shown, in red if Legato can't read it there. A number, switch or choice field has **Use a value from…** instead.
    - Each step's row says in one line what it does, such as its prompt, its macro or the column it moves a card to. The line before it says how it starts: **then** (right after the step before), **waits for you** (you start it), or **when Review's agent ends**.
    - The **+** on that line adds a step there; **Add step** adds one at the end. Type to find a step type: they are grouped as Agents, Code, Board and Cleanup, and Enter adds the first match. The new step opens with its first field ready to fill in. A new step's ID is made from its title, and follows the title until you save.
    - A step's **⋮** menu duplicates, moves or deletes it, and on a computer you can drag a row to move it. A move that would make a step read the outputs of one after it is refused, with the reference named. Deleting a step that others read asks first.
    - Renaming the ID of a step that others read asks whether to change their references too.
    - A field the step's type doesn't know is kept, and named, for you to edit in YAML.
    - Each trigger in **When** reads as a sentence, such as "A card enters **Review** · priority is **high**" or "Every **weekday** at **09:00**", with **on** or **paused** beside it as this Legato runs it. You can always run a workflow by hand too.
    - A column-entry trigger offers to add a task input filled from the card that entered the column; that input shows as "from the card" under **Asks for**.
    - After you save a workflow whose trigger is paused, the editor says **Saved · paused** and offers **Turn on**, which asks first, as it will start runs by itself. A new workflow with a paused trigger stays open in the editor after **Create** for this.
    - A trigger maps the workflow's inputs from what it gives, says whether it starts runs by itself in the file, whether it asks you first, and its runs a day at most. A column-entry trigger also takes its column, handed-off cards and conditions; a schedule is **Every** or **Cron**, with its next three runs.
  - A part of the file the forms can't show, such as a step written with a YAML anchor, is marked **Edited as YAML** and opens in its YAML tab. The rest stays as forms. Only a file whose YAML doesn't parse opens in YAML alone.
  - A change touches only what it changes: the rest of the file, comments included, stays as you wrote it.
  - Problems Legato finds show on their fields, and as a count beside each row and in the header.
  - Keys: **j** and **k** move through the rows, **Enter** opens one, **y** switches between Visual and YAML, and Ctrl+S (⌘S on a Mac) saves.
  - On a phone, the outline is the screen and Save is at the bottom. Tapping a row opens it full screen; Back returns to the outline.
- **Undo** and **Redo** go back and forth through your changes on both sides. In YAML, Ctrl+Z (⌘Z on a Mac) and Ctrl+Shift+Z (⌘⇧Z) do the same, so they also undo a change made in Visual. A pause in typing starts a new step. A change made in Visual leaves your place in the YAML where it was.
- The side you used last is remembered on this device.

**Map**, above the outline, shows the workflow as a graph instead, read-only: choosing a step or a trigger in it opens that row. While the file has an error, the map is of the last version Legato could read, and says so.

In YAML, the outline stays beside the text. Choose a row to go to its part of the file; as you move through the file, the row you are in is marked. On a phone it is a **Jump to** menu.

If the YAML stops parsing, Visual keeps the outline as it last was, greyed out, and says on which line Legato can't read the YAML; **Show** takes you there. The forms come back once it parses.

Pasting YAML on the forms (not into a field) does what you'd expect: a whole workflow asks whether to replace this one, and steps are added after the step you have open.

The YAML side knows the workflow format, from the same schema Legato checks files with:

- As you type, it offers the keys that fit where you are: the top-level fields, the trigger kinds under `on:`, the step types (with what each does), and each step type's `with` fields. It also offers the values of fields that take one of a few, such as `catch_up` or `scope`, and `true` and `false`.
- Hover over a key to read what it is for.
- Inside `${{ }}` it offers what can be read at that place:
  - in a step's `with`: the workflow's inputs, a task input's fields (`.title`, `.metadata.`…), `workflow.task`, the outputs that earlier steps declare, and the step's own file outputs;
  - in `run_title`: the inputs alone;
  - in a file output's `path`: the inputs, the task, and the outputs of earlier steps;
  - in a trigger's `inputs`: what that trigger gives, such as `schedule.time`, `event.pr_number` or `task.title`.

  Hover over an expression to see what it reads. One that Legato would refuse, such as a later step's output, is marked as you type.
- Problems are marked on their lines and listed beside the editor, as you type and again when Legato checks the file, which also checks the `${{ }}` references and each step's rules. A `with` key that the step's type doesn't read is a warning: Legato ignores it for now and a later release will refuse it.
- **Save** stays off while the file has an error.

When you save, these rules apply:

- If the file changed on disk since you opened it, the save is refused. Choose **Reload** to see the file as it is now, or **Keep my edits** to save yours over it.
- A save that would switch on a trigger, so that it starts runs by itself, asks first: **Save and switch on** goes ahead. A trigger you paused on this Legato with its switch doesn't ask.
- A workflow's `name` can't change once it is made. Make a new workflow to give it another name. A new workflow can't take a name another workflow has, a built-in's included.
- Runs that have started keep the definition they started with. Their page says that the workflow has changed since.

If you leave the editor with unsaved changes, Legato asks first when it can and keeps them in this browser either way: the next time you open the workflow it offers them back.

**Delete** on the editor removes the file. The workflow's runs are kept, and this Legato forgets its trigger switches. A built-in workflow, such as `legato-job`, has no file: **Copy to my workflows** on its page writes it to your workflows folder as a file of the same name, which then replaces the built-in and can be edited. A file in `~/.legato/workflows` can be edited, but new workflows are never put there. A file that is a link to somewhere outside its folder is read-only.

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

On a [worker](/legato/running/instances/), a column trigger skips cards handed off from a main: they are the main's, and the main's own workflows run when the task comes home. Set `handed_off: true` on the trigger to run it on those cards too.

In the web UI's editor, the builder's **Triggers** section edits this trigger as a form: see [Create and edit in the web UI](#create-and-edit-in-the-web-ui).

### On a schedule

A `schedule` trigger runs a workflow at due times: every day, every weekday, or once a week at a time, or as a cron expression says.

```yaml
on:
  schedule:
    every: weekdays        # daily | weekdays | weekly
    at: "09:00"
    # on: monday           # weekly only
    # cron: "0 9 * * 1-5"  # instead of every/at/on
    timezone: Australia/Sydney   # optional; your machine's zone by default
    inputs:
      since: ${{ schedule.previous_time }}
```

Give either `every` and `at` (and `on` for `weekly`), or `cron`: five fields, minute, hour, day of the month, month and weekday, each `*`, a number, a range `a-b`, a list, or a step `*/15`; months and weekdays may be names. Times are in `timezone`, or your machine's zone. When the clocks go forward and a time does not exist that day, the run comes at the next minute that does; when they go back and a time comes twice, it runs once.

A run's inputs may read `${{ schedule.time }}`, its due time, and `${{ schedule.previous_time }}`, the due time before it, so a run can cover "since last time". Each due time makes one run, however many Legatos (the TUI and `legato serve`, say) share the database. The **Workflows** tab shows each schedule's next three runs and what its last one did.

In the web UI's editor, the builder's **Triggers** section edits this trigger as a form: see [Create and edit in the web UI](#create-and-edit-in-the-web-ui).

#### Missed runs

When Legato starts, the due times that passed while no Legato was running (since the last one it recorded) are recorded as **missed**, and the Workflows tab lists them with why. `catch_up` says what then happens: `once`, the default, makes one late run for all of them, whose `${{ schedule.previous_time }}` is the due time before the first missed one, so it covers them all; `skip` makes none. A restart does not record or run them again. A scheduled run that cannot start reaches [Attention](/legato/using/attention/) as a failed trigger.

### When changes are requested

A `pr.changes_requested` trigger starts a workflow when someone requests changes on a pull request one of your tasks owns. Every new changes-requested review fires: a second reviewer's, or the same reviewer's again. Your own reviews count, so you can request changes yourself to start it; reviews by your Legato's own GitHub Apps never do (name their bot logins in `github.app_logins`; Legato also learns them from the reviews it posts). Legato sees reviews when it refreshes the task's pull request, so it costs no extra GitHub calls, and the run can start up to a refresh interval after the review.

```yaml
on:
  pr.changes_requested:
    inputs:
      task: ${{ event.task }}
      repo: ${{ event.repo }}
      pr_number: ${{ event.pr_number }}
```

With no inputs to map, write `pr.changes_requested: {}`. The inputs may also read `${{ event.pr_url }}` and `${{ event.reviewer }}`. The [loop stop](#the-loop-stop) counts runs by the task.

In the web UI's editor, the builder's **Triggers** section edits this trigger as a form: see [Create and edit in the web UI](#create-and-edit-in-the-web-ui).

### Pausing a trigger

Every trigger is on unless it says `enabled: false`, which makes it start paused: a shared workflow then runs nothing until someone switches it on.

```yaml
on:
  task.entered_column:
    column: Triage
    enabled: false
```

On the **Workflows** tab, each trigger has a switch that says **on here** or **paused here**. It turns the trigger on or off in this Legato only and never changes the file; another Legato that reads the same file keeps its own switch. A trigger you have not switched follows its file. A paused trigger makes no requests while it is paused, and moves made then are not caught up later.

### The loop stop

A trigger starts at most three runs of its workflow for the same item in a day: the same task for a column trigger, the same pull request, the same due time of a schedule. A fourth is held instead, and [Attention](/legato/using/attention/) says it **stopped to prevent a loop**, with **Run anyway** and **Pause trigger** (which pauses it here, as the switch does). A busy trigger can allow more with `max_runs_per_day`:

```yaml
on:
  task.entered_column:
    column: Triage
    max_runs_per_day: 10
```

Set `confirm: true` on a column trigger to require explicit approval after its conditions match and inputs resolve. Existing triggers without it continue to start automatically. Confirmable requests remain recoverable in Attention: closing the immediate prompt means **Not now** and makes no state change, while **Skip** terminates the request. Conditions are re-evaluated when the user confirms, so a session that closes while the prompt is pending causes the trigger to skip without creating a workflow run.

Automatic triggers run for explicit local moves, including local moves of provider-backed tasks, and for a task [handed off to a worker](/legato/using/handoff/) that comes home into another column than it left (its Return or Recall is a move like any other); pull-sync status changes do not trigger workflows. Trigger requests are durable and idempotent. Missing required inputs leave the card in its destination column and create a recoverable Attention item; for confirmable triggers, supplying those inputs advances to confirmation instead of starting immediately. The workflow can always be started manually as a separate run.

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

The execution task is where the run's agents work, and `${{ workflow.task }}` and `LEGATO_TASK_ID` name it. When a workflow takes a `task` input, the task you chose is the run's subject, recorded beside the run; the run's agents still work on the execution task, so an agent already running on your task is not replaced. The run shows on that task's detail. An agent working on the execution task (a step's agent, or a review loop's reviewer) shows "for <card>" under its name in the agents rail, and the run's Attention items say which card they are for.

A run's agent can send its questions to [the Reeve](/legato/using/reeve/) instead of you (the Ticket to PR macro does when its `ask_reeve` input is on). While it waits for the answer, it shows as **Waiting on the Reeve** on its card and in the agents list, not as your turn. If the Reeve has not answered 15 minutes after it was told of the question, or none is running to answer, it moves to **Needs you** and says how long it has waited, such as "Waiting on the Reeve for 20 min".

Fixed steps run one after another until a step hands work to an agent. The run moves on when that agent reports that it completed or failed its step.

A few steps wait on Legato itself rather than on an agent or on you: their status is **waiting** until the part of Legato that owns them finishes them, with their outputs, or fails them. Such a step is tied to no agent, so no agent exiting fails it, and it is still waiting after Legato restarts. Cancelling the run stops it.

## Schema version 2

Schema version 2 is arriving in parts. Today a version 2 file is a version 1 file with two differences:

- A step says how it starts with `start: automatic` (the default) or `start: manual`. `trigger:` is a version 1 key, and a version 2 file refuses it. A version 2 file has no `agent_ended`.
- A later step can read any output its executor's catalog lists, such as `${{ steps.prep.outputs.branch }}` after `repo.ensure`, without the step declaring it. This also holds in version 1 files.

Legato reads a version 1 file into the same model: `trigger: manual` is `start: manual`, and `trigger: agent_ended` still waits for the workflow task's agent to end. The editor, the builder, the published schema and the bundled templates still write version 1.

## Agent completion

Workflow agents receive these environment variables:

```text
LEGATO_TASK_ID
LEGATO_WORKFLOW_RUN_ID
LEGATO_WORKFLOW_STEP_ID
LEGATO_WORKFLOW_ATTEMPT_ID
```

`LEGATO_TASK_ID` is the generated workflow execution task ID and is also printed in the assignment completion contract.

The step's brief ends with a completion contract: the run, step and attempt, the outputs the step declares, and where its result must go. What the agent writes in its conversation is not kept once Legato removes it, so the result belongs in the declared outputs. When a step declares no outputs, the contract says that nothing it writes in the conversation is kept, so that it does not report a result as "delivered in chat". `legato workflow validate` and the editor warn of an `agent.run` step that declares no outputs and has no `keep_agent`. It is a warning, not an error: a step whose work is its side effects (a commit, a note, a moved card) needs no output.

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

An agent that exits, including one whose pane is back at a shell, fails its unfinished step with **agent exited before step completion**. Use **Retry step** to try again. An agent that never connects to Legato (its agent tool started, but the session's bridge to Legato did not come up within 30 seconds, so it could not be given its prompt) fails its step too, with **the agent never connected to Legato**, the reason and the name of its terminal, instead of waiting forever. Its prompt is dropped with the step. Exit has not succeeded: only `legato workflow step complete` completes a step. Missing or invalid outputs leave the step incomplete or failed. Learn's interview follows the same rule through `legato learn reflection submit`; see [Legato Learn](/legato/using/learn/). The command only saves the outputs. The Legato that runs the board or the web UI then removes the step's agent (unless `keep_agent` is set) and only then starts the next step, so two `agent.run` steps in a row work. That happens at once, or within 30 seconds if Legato missed the message. A step completed while no Legato ran moves on when one starts. If a completion is interrupted and the agent runs the command again, a file that has not changed is accepted again, and a file that changed is refused.

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

While a review step waits on its [review loop](/legato/using/review-passes/#review-any-pull-request), the run's page says where the loop stands (**Reviewing · pass 2**, **Author fixing · 3 blocking**, **Waiting for author · 3 blocking**, **Stuck:** and why, **Approved**), links each review posted, and offers **Run another pass** while it waits for the author or is stuck. A stuck loop is in Attention with **Run another pass** and **End**.

A loop waiting for its author needs you only when the author will not move it alone. That means a person author, an agent author that waits for your answer, or an agent author that has stopped or ended with findings still unanswered for longer than `review_passes.quiet_time`. While an agent author works on the findings (**Author fixing**), the wait is the agent's: the step shows as running, and nothing is in Attention or under Needs you. When the author does need you, Attention shows **Run another pass**, plus **Message the author** for an agent author.

While a `github.pr.wait` step waits, the run's page says **Waiting for PR #n to merge**, with the pull request's branch, its CI, and whether it conflicts. It offers **Open PR**, **Open agent** while the card's agent runs, and **Resume agent…** (**Send a macro…** while the agent runs). **Resume agent…** is on the page of any running run with a card whose agent is not waiting for you, unless a step's own agent is at work: then that step offers **Prompt to continue** and **Retry**. A resumed agent's approvals and items show under the run's Needs you.

The **Runs** list, a run's page and graph, and the **Workflows** tab update by themselves within a second or so of a change, without a reload: a step starting, finishing or failing, a run that a trigger or a schedule started, a review pass, and a trigger switched on or off in another tab or on another device. When a laptop wakes or a phone comes back to the page, everything open is read again. When the agent of an unfinished `agent.run` step stops to wait for you, goes idle, the step is marked as needing attention: the Workflows item in the navigation shows a red count, the run's card says why it stalled, and the run page offers **Prompt to continue** while the agent is still running. Legato never prompts the agent by itself, so it cannot start the same work twice.

## Limits

Schema version 1 runs steps one after another. It has no branching graphs of steps, no parallel steps, no schedules, no shell plugins, no approval gates beyond manual steps, and no steps that publish to outside services except `github.pr.review_passes`, which posts its review passes to GitHub as the reviewer App.

Internals (the HTTP API, how runs and artifacts are stored, how the web UI is kept up to date) are in [docs/ai/workflows.md](https://github.com/cpave3/legato/blob/main/docs/ai/workflows.md) on GitHub.
