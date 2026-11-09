---
title: CLI commands
description: Every legato subcommand, its flags, its JSON output and its exit codes.
sidebar:
  order: 20
---

The `legato` command starts the TUI and the web server, and gives you and your scripts a command line for tasks, agents, plans, reviews, workflows and credentials. Agents that Legato launches use the same commands to report what they do.

Commands take the form `legato <noun> <verb> [arguments] [flags]`. Every level answers `--help` (or `-h`) with its own usage and examples, without loading your configuration:

```bash
legato --help
legato task --help
legato workflow step complete --help
```

`legato help` prints a short Markdown primer written for AI agents.

## Output, JSON and exit codes

Results go to standard output. Errors and other messages go to standard error.

Most commands take `--json`. With it, a command writes one JSON value. On success it goes to standard output:

```json
{"ok": true, "command": "task.create", "data": { ... }}
```

On failure it goes to standard error:

```json
{"ok": false, "command": "task.create", "error": {"code": "missing_argument", "message": "...", "details": { ... }}}
```

`legato --json <command> ...` and `legato --format json <command> ...` are the same as adding `--json` at the end. The `workflow` and `learn` commands print plain JSON on success, without the envelope.

Every command exits with one of these codes:

| Code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | Unexpected internal failure |
| `2` | Usage error or invalid input |
| `3` | Not found |
| `4` | Refused by a provider or by a policy, such as a credential grant |
| `5` | Configuration or a needed environment variable is missing |
| `6` | An external dependency failed |

Legato rejects an unknown or repeated flag before it reads your configuration or database.

### The task ID from the environment

Inside an agent session, Legato sets `LEGATO_TASK_ID`. Commands that act on one task use it when you leave the task ID out: `task show`, `notes`, `update`, `description`, `note`, `link`, `unlink`, `task github ...`, `task worktree ...`, `agent state` and `agent status`. A task ID you pass always wins. `task create` never reads it.

## Running Legato

- `legato`: start the TUI. It also starts the web server when your configuration turns it on. See [On your workstation](/legato/running/workstation/).
- `legato serve [--port <port>]`: run the web server alone, without the TUI. It listens on `web.port` (3080 by default); `--port` overrides it. See [Web server](/legato/running/web-server/).
- `legato --hot-reload` and `legato --hot-reload serve [--port <port>]`: run the TUI or the web server under a supervisor that restarts it when the `legato` binary is rebuilt. `--hot-reload` must come first, and works with these two commands only. See [Build from source](/legato/contributing/build/).
- `legato restart [--json]`: restart every Legato process of this instance (the TUI and `legato serve`) in place. Agents keep running. See [Updates](/legato/running/updates/#restarting-legato-yourself).
- `legato version [--json]`: print this binary's version, `vX.Y.Z` for a release or `dev` for a build from source. See [Updates](/legato/running/updates/).
- `legato pair [--host <host|ip>] [--port <port>] [--json]`: pair a phone with the web UI. When Legato's web server runs on this machine, it shows a QR code and a phrase to type in the Legato app, and waits until a phone pairs (exit `0`) or the phrase expires (exit `4`). With `--json` it prints the phrase and does not wait. See [Phones and other devices](/legato/running/devices/).
- `legato pair --main [--host <host|ip>] [--port <port>] [--json]`: on a worker, pair a main instance. It prints this Legato's address and a phrase to enter in the main's Settings › Handoff › Workers › **Add a worker**, and waits until the main pairs. It needs the web server to be running. See [Main and worker instances](/legato/running/instances/).
- `legato auth token [--json]`: print the web UI's access token.
- `legato auth regenerate [--json]`: replace the access token. Every paired device must pair again. See [Web UI access](/legato/security/web-auth/).
- `legato device list [--json]`: list the phones that sign approval decisions.
- `legato device reset [--json]`: remove every device, for when you lose the only one. Run it on the machine that runs Legato.
- `legato device attestation <device-id> [--json]`: print the attestation chain a phone enrolled with, as PEM.

```bash
legato serve --port 3080
legato pair --host 192.168.1.20
```

## Tasks

- `legato task list [--status <column>] [--workspace <name|unassigned>] [--limit <count>] [--json]`: list active tasks in board order.
- `legato task create <title> [--description <text> | --description-file <path|->] [--status <column>] [--priority <priority>] [--workspace <name|unassigned>] [--json]`: create a local task. `--description-file -` reads Markdown from standard input. `--workspace none` or `unassigned` overrides a configured default workspace.
- `legato task show [<task-id>] [--format description|full|json] [--json]`: print a task. `full` and `json` include the task's notes and its pull requests in stack order; `json` also lists the branches it tracks (`tracked_branches`).
- `legato task update [<task-id>] [--status <column>] [--title <title>] [--description <text> | --description-file <path|->] [--workspace <name>] [--json]`: change a task. `--status` moves it to another column; `--workspace none` clears its workspace. You cannot change the title of a task that comes from Jira. A description you set is Legato's own text, kept beside the ticket's description; Jira does not change.
- `legato task description [<task-id>] (--description <text> | --description-file <path|->) [--json]`: replace a task's Legato-owned description. The same as `task update --description`.
- `legato task note [<task-id>] <message> [--json]`: add a timestamped line to the end of a local task's description. Tasks from Jira are refused.
- `legato task notes [<task-id>] [--json]`: list a task's notes, such as the reports a workflow wrote.
- `legato task archive list [--json]`: list archived tasks.
- `legato task archive restore <task-id> [--json]`: bring back an archived task, into the first column.

Status names must match a column of your board. Task IDs are opaque; copy them exactly.

```bash
legato task create "Implement login" --workspace Personal
legato task list --workspace Personal --status Backlog --json
legato task update --status Doing
```

### Code context and pull requests

These commands tell Legato which repository and branch a task works on, so it can find the task's pull request. See [GitHub](/legato/integrations/github/).

- `legato task link [<task-id>] [--branch <branch>] [--repo <owner/repo>] [--sha <commit>] [--json]`: record a task's branch, repository or commit. Hooks and scripts use it.
- `legato task unlink [<task-id>] [--json]`: forget a task's repository context and pull request.
- `legato task github context [<task-id>] --repo <owner/repo> [--branch <branch>] [--sha <commit>] [--base-branch <branch>] [--json]`: set the repository context by hand. It takes precedence over what Legato finds itself.
- `legato task github refresh [<task-id>] [--json]`: fetch the linked pull request again.
- `legato task github rediscover [<task-id>] [--json]`: forget the linked pull request and look for it again, keeping the repository context.
- `legato task worktree set [<task-id>] [--path <path>] [--primary-dir <path>] [--branch <branch>] [--base-branch <branch>] [--json]`: record the worktree a task works in.
- `legato task worktree clear [<task-id>] [--json]`: forget it.

### Workspaces and columns

- `legato workspace list [--format text|json] [--json]`: list your workspaces, with the exact names `--workspace` takes.
- `legato columns sync [--json]`: update the board's columns from `config.yaml`.

## Agents

- `legato agent peers [--task <task-id>] [--execution-id <id>] [--directory <path>] [--json]`: list the other Legato agents working in the same Git worktree.
- `legato agent message <task-or-execution-id> <text> [--json]`: send a message to a running agent.
- `legato agent broadcast <text> [--task <task-id>] [--execution-id <id>] [--directory <path>] [--json]`: send a message to every other agent in the same Git worktree.
- `legato agent state [<task-id>] --activity working|waiting|background|error|"" [--execution-id <id>] [--json]`: report what an agent is doing. The hooks Legato installs call it; an empty activity clears it.
- `legato agent summary [--exclude <task-id>] [--json]`: count the agents that are working, waiting, in error and idle, formatted for a tmux status line.
- `legato agent status [<task-id>] --format tmux [--json]`: a tmux status line for one task. A swarm task shows its progress; any other task shows the summary. See [tmux status line](/legato/integrations/tmux-status/).
- `legato attention list [--producer <name>] [--category <category>] [--urgency <urgency>] [--json]`: list what waits for you in Attention. See [Attention](/legato/using/attention/).

```bash
legato agent peers --json
legato agent broadcast "Working on internal/service/agent.go"
```

Peer messages are hints between agents. They do not lock files. See [Agents](/legato/using/agents/).

## Hooks

- `legato hooks install --tool <tool>[,<tool>...] [--json]`: install the hooks that tell Legato what an agent tool is doing. The tools are `claude-code`, `codex`, `chimera`, `staccato` and `yggdrasil`. `--tool` is required.
- `legato hooks uninstall --tool <tool>[,<tool>...] [--json]`: remove them.

```bash
legato hooks install --tool claude-code,codex,chimera
```

See [Claude Code](/legato/agent-tools/claude-code/), [Codex](/legato/agent-tools/codex/), [Chimera](/legato/agent-tools/chimera/), [Staccato](/legato/integrations/staccato/) and [Yggdrasil](/legato/integrations/yggdrasil/).

## Plans

An agent uses these commands to propose a plan and act on your feedback. `--task` and `--name` choose the plan; inside a session they default to `LEGATO_TASK_ID` and `LEGATO_PLAN_NAME`. See [Plans](/legato/using/plans/).

- `legato plan validate <markdown-file>`: check a plan's Markdown, its diagrams and the image files it names, without a task or a database. Exit `2` means the plan has errors.
- `legato plan submit <bundle-dir> [--task <id>] [--name <name>] [--json]`: submit the `plan.md` and `plan.json` in a folder as a new revision.
- `legato plan show [--task <id>] [--name <name>] [--json]`: print the current revision with its comments and questions.
- `legato plan feedback [--task <id>] [--name <name>] [--json]`: print the same, for reading your feedback.
- `legato plan status [--task <id>] [--name <name>] [--json]`: print the plan's state and revision.
- `legato plan answer <thread-id> (<markdown> | --file <path|->) [--task <id>] [--name <name>] [--json]`: answer a question on the plan.
- `legato plan claim --task <plan-task-id> [--name <name>] [--json]`: take over a plan from another agent session.
- `legato plan inbox [--json]`: print and acknowledge the plan and review feedback waiting for this agent.
- `legato plan complete [--task <id>] [--name <name>] [--json]`: mark the plan done after the work is built.
- `legato plan withdraw [--task <id>] [--name <name>] [--json]`: withdraw a plan that is not approved.

```bash
legato plan validate .legato/plans/search/plan.md
legato plan submit .legato/plans/search --name search
```

## Reviews

An agent uses these commands to build a review tour of its changes and answer your questions. `--task` and `--name` choose the review; inside a session they default to `LEGATO_TASK_ID` and `LEGATO_REVIEW_NAME`. See [Reviews](/legato/using/reviews/).

- `legato review chapter <title> [<narration> | --narration-file <path|->] --include <path>:<hunk>... [--risk low|medium|high|unsure] [--order <n>]`: add a chapter that groups hunks of the diff.
- `legato review chapter show <chapter-id>`: print a chapter and its diff.
- `legato review chapter edit <chapter-id> [--title <text>] [--narration <text> | --narration-file <path|->] [--risk <risk>] [--order <n>] [--include <path>:<hunk>...] [--clear-narration] [--clear-risk] [--clear-order]`: change a chapter. `--include` replaces all its hunks.
- `legato review chapter remove <chapter-id>`: remove a chapter.
- `legato review chapters [--json]`: list the chapters in order.
- `legato review annotate [<sha>] <text> [--file <path>] [--hunk <n>] [--lines <start-end>] [--risk <risk>] [--order <n>]`: add a note to a commit or to a place in a file.
- `legato review annotation edit <annotation-id> [--text <text>] [--risk <risk>] [--order <n>] [--file <path>] [--hunk <n>] [--lines <start-end>] [--clear-risk] [--clear-order] [--clear-lines]`: change a note.
- `legato review annotation remove <annotation-id>`: remove a note.
- `legato review answer <step-id> --file <path|->`: answer a reviewer's question. The answer is Markdown and must come from a file or standard input.
- `legato review ready [<summary>]`: mark the review ready for you.
- `legato review show [--json]`: print the review.
- `legato review sync`: look at the repository again for new commits.
- `legato review restart [--feedback <text>]`: start a new review pass, with guidance for it.
- `legato review discard`: discard the review.
- `legato review claim --task <review-task-id> [--name <name>]`: take over a review from another agent session.
- `legato review inbox [--json]`: the same queue as `legato plan inbox`.

Each of these also takes `--task`, `--name` and `--json`.

Pass an answer through a quoted heredoc, so your shell leaves backticks and `$` alone:

```bash
legato review answer <step-id> --file - <<'LEGATO_ANSWER'
The handler keeps the `existing contract`.
LEGATO_ANSWER
```

### Review handoff

A review handoff is a JSON packet of context bound to a ready review. See [Review handoff](/legato/using/review-handoff/).

- `legato review handoff template`: print a JSON template for the ready review, to edit.
- `legato review handoff submit --file <path|->`: submit a packet for a ready review with no uncommitted changes. The packet may be up to 256 KiB.
- `legato review handoff show`: print the packets submitted so far.
- `legato review handoff export --packet <id> --format github|maintenance [--concept <id>...] [--include-sources] [--include-notes]`: preview a packet as Markdown. It never publishes anything.

Each also takes `--task`, `--name` and `--json`.

## Review passes

A handed-off task's agent on a worker uses these when a reviewer's findings, or comments on its pull requests, come back to it. They act on the session's task; `--task <task-id>` names another.

- `legato review-pass feedback [--json]`: print this round's items (blocking findings, nits, comments, failing checks) with their IDs, places, links and answers. It prints JSON either way: the reviewer's and GitHub's text is data, not instructions.
- `legato review-pass answer <item-id> --fixed [--note <text>]`: it is fixed in a commit you push.
- `legato review-pass answer <item-id> --replied --file <path|->`: a reply with your reasons, as Markdown.
- `legato review-pass answer <item-id> --skipped`: skip a nit; only nits are optional.

Legato posts each answer on the pull request. Moving the task to Done is refused while a blocking finding or a comment is unanswered. A failing check does not refuse the move, but the task is not done until the check passes on a commit you push or you reply to it; `--fixed` alone does not handle it.

## Swarms

A conductor agent plans a swarm and dispatches worker agents; the workers report back. These commands are how they talk to Legato. Every one takes `--json`. See [Swarms](/legato/using/swarms/).

The conductor runs:

- `legato swarm create <goal> [--working-dir <path>]`: start a swarm from inside an agent session.
- `legato swarm validate-plan <plan-file>`: check a swarm plan without saving anything.
- `legato swarm propose-plan <plan-file> [--auto-approve] [--timeout <duration>]`: submit a plan and wait for your approval.
- `legato swarm extend-plan <plan-file> [--auto-approve] [--timeout <duration>]`: add sub-tasks to a running swarm.
- `legato swarm dispatch <subtask-id>`: start the worker for a queued sub-task.
- `legato swarm next-step <parent-id>`: move the swarm on to its next step.
- `legato swarm message <subtask-id> <text> [--urgent]`: send a message to one worker.
- `legato swarm broadcast <parent-id> <text> [--urgent]`: send a message to every worker.
- `legato swarm close <subtask-id>`: accept a worker's sub-task as done.
- `legato swarm finish <parent-id> <summary>`: end every worker and add the summary to the task.
- `legato swarm cancel <parent-id>`: stop the whole swarm and remove its sub-tasks.

Workers run:

- `legato swarm progress <subtask-id> <text>`: report progress to the conductor.
- `legato swarm question <subtask-id> <text>`: ask the conductor a question.
- `legato swarm built <subtask-id>`: say the sub-task is built.

Anyone can run:

- `legato swarm status <parent-id>`: print the swarm's state as JSON.
- `legato swarm inbox <parent-id>`: print and acknowledge the swarm's unread events.

## Workflows

See [Workflows](/legato/using/workflows/).

- `legato workflow list [--json]`: list the workflows you can run.
- `legato workflow validate <name-or-file> [--json]`: check a workflow definition.
- `legato workflow run <name> [--input <key>=<value>...] [--json]`: start a run.
- `legato workflow runs [--status <status>] [--limit <count>] [--json]`: list runs.
- `legato workflow show <run-id> [--json]`: print a run and its steps.
- `legato workflow trigger <run-id> <step-id> [--force] [--json]`: start a manual step.
- `legato workflow retry <run-id> <step-id> [--force] [--json]`: run a step again.
- `legato workflow cancel <run-id> [--json]`: cancel a run.

An agent working on a workflow step ends it with one of these. The run, step and attempt come from the environment Legato gives the agent:

- `legato workflow step complete [--output <key>=<value>...] [--artifact <key>=<path>...] [--json]`
- `legato workflow step fail <reason> [--json]`

```bash
legato workflow run deploy --input environment=staging --json
legato workflow step complete --output result=value --artifact review=review.md --json
```

## Learning

Legato Learn keeps what agents learn, for you to accept and give to later agents. Commands that change a learning take `--revision`, the revision you last read, so two edits cannot overwrite each other. See [Legato Learn](/legato/using/learn/).

- `legato learn record [<claim>] [--claim <text>] [--kind <kind>] [--category <category>] [--tags <tag,...>] [--scope user|project] [--project <key>] [--rationale <text>] [--task <task-id>] ...`: record a candidate learning. `--help` lists the evidence flags.
- `legato learn show <learning-id>`: print a learning and its evidence.
- `legato learn list [--scope user|project] [--status <status>] [--project <key>] [--category <category>] [--tags <tag,...>] [--limit <count>]`: list learnings.
- `legato learn search <query> [--scope ...] [--status <status>|all] [--project <key>] [--all-projects] [--category ...] [--tags ...] [--min-score 0..1] [--limit <count>]`: search learnings by words and by meaning. It searches the current repository and your own learnings unless you say otherwise.
- `legato learn context [--task <task-id>] [--limit <count>]`: print the learnings that fit a task.
- `legato learn curate --task <task-id>`: start the workflow that reflects on a task and proposes learnings.
- `legato learn consolidate --scope user|project [--project <key>]`: start the workflow that merges and tidies learnings.
- `legato learn accept <learning-id> --revision <n> [--claim <text>] [--scope ...] [--project <key>]`: accept a candidate, changing its wording or scope if you like.
- `legato learn edit <learning-id> --revision <n> [--claim ...] [--rationale ...] [--scope ...] [--project ...] [--category ...] [--tags ...]`: change a learning.
- `legato learn reject <learning-id> --revision <n>`: reject a candidate.
- `legato learn enable <learning-id> --revision <n>` and `legato learn disable <learning-id> --revision <n>`: give an accepted learning to future agents, or stop.
- `legato learn incorporate <learning-id> --revision <n> --destination <destination> [--target <path>] [--action <action>]`: propose a place for a learning to live, such as a document in the repository.
- `legato learn incorporation submit <incorporation-id> --revision <n> [--file <path|->]`: submit where it was put.
- `legato learn verify <incorporation-id> --revision <n> [--repo-root <path>] [--confirm]`: check that it is there.
- `legato learn drift [--repo-root <path>]`: check that incorporated learnings are still where they were put.

Every `learn` command takes `--json`.

## Resources

Resources are named files and folders you can give to agents. See [Resources](/legato/agent-tools/resources/).

- `legato resource list [--json]`: list resources, from your configuration and saved ones.
- `legato resource add <name> <path>... [--write] [--json]`: save a resource, or add paths to a saved one. Paths are absolute or start with `~/`. `--write` lets agents change them.
- `legato resource remove <name> [--json]`: remove a saved resource. A resource from `config.yaml` stays.

```bash
legato resource add "Obsidian vault" ~/Documents/Vault --write
```

## Secrets and credentials

See [Set up credentials](/legato/security/credentials-setup/) and [Credential broker](/legato/security/credential-broker/).

- `legato secret add <name> [--type <type>] [--from <path>] [--json]`: store a secret in Legato's own encrypted store, or replace one of the same name. It reads the value from standard input unless you pass `--from`. The types are `value` (the default), `userpass`, `keypair`, `oauth-client` and `oauth-refresh`. A credential names it as `legato://<name>`.
- `legato secret list [--json]`: list stored secrets. It never prints their values.
- `legato secret remove <name> [--json]`: remove a secret.
- `legato credential list [--json]`: list the configured credentials and their rules.
- `legato credential request <name> --operation <operation>... [--resource <resource>...] [--reason <text>] [--blocking] [--json]`: from an agent session, ask you for the use of a credential. It waits up to a minute for your answer, then reports that the request is pending.
- `legato credential get <name> [--operation <operation>] [--resource <resource>...] [--json]`: print a credential's value, when this session may use it. Exit `4` means it may not.
- `legato credential exec <name> --env <VARIABLE> [--operation <operation>] [--resource <resource>...] -- <command>`: run one command with the credential's value in one environment variable, kept out of the agent's output.
- `legato audit [--credential <name>] [--task <task-id>] [--execution <execution-id>] [--limit <count>] [--json]`: read the credential audit log, newest first. It shows 50 rows unless you pass `--limit`.
- `legato audit verify [--json]`: check that no row of the audit log was changed or removed.

```bash
legato secret add elevenlabs-key < key.txt
legato credential exec elevenlabs --env ELEVENLABS_API_KEY -- libretto render story.yaml
```

## Integrations

These commands connect Legato to Argus and Burnlite. They run only on the machine that runs Legato. See [Argus](/legato/integrations/argus/) and [Burnlite](/legato/integrations/burnlite/).

- `legato integration token create <name> --scope <scopes> [--json]`: make a token for an integration and print it once. The scopes are `owner:read`, `delegate` and `attention`, separated by commas.
- `legato integration token list [--json]`: list integration tokens, never the tokens themselves.
- `legato integration token revoke <name> [--json]`: stop a token working at once.
- `legato integration connect argus [<url>] [--no-browser] [--json]`: connect Legato to Argus, so agents' replies reach it. It asks for your consent in the browser. The address defaults to `integrations.argus.url`.
- `legato integration status argus [--json]`: say whether Legato is connected and how many replies wait to be sent.
- `legato integration disconnect argus [--json]`: forget the connection.
- `legato integration connect burnlite [<url>] [--no-browser] [--json]`: connect Legato to Burnlite, so you can import its tasks and they stay up to date. It asks for your consent in the browser, to read your tasks only. The address defaults to `burnlite.url`. You can also connect from Settings › Integrations in the web UI.
- `legato integration status burnlite [--json]` and `legato integration disconnect burnlite [--json]`: the same for Burnlite. Its tasks stop syncing when you disconnect.

```bash
legato integration token create argus --scope owner:read,delegate,attention
legato integration connect argus https://argus.lan
legato integration connect burnlite https://burnlite.example.com
```

## Clones

- `legato clone setup [--base <ref>] [--primary <path>]`: run the setup commands of the clone in the current folder, from its `.legato.toml`, else from the `post_create` hooks of its `.yggdrasil.toml`. Output goes to the terminal and to `.git/legato-setup.log`. Legato runs it for you in a clone it has just made for an agent; run it yourself to set up again. See [Configuration](/legato/reference/configuration/).

## Commands Legato runs for you

Legato, its hooks and the agent tools it launches run these. You do not need to run them.

- `legato agent bridge`: serves a Claude Code session to Legato's native panel. The Legato mod inside Claude Code starts it.
- `legato agent codex -- <codex arguments>`: starts Codex with its bridge. Legato types it into the agent's terminal in place of `codex`.
- `legato agent codex-bridge`: serves a Codex session to Legato's native panel. `legato agent codex` starts it.
- `legato agent stopped`: turns Claude Code's stop hook into an agent activity.
- `legato agent permission-resolved`: turns Chimera's permission answers into an agent activity.
- `legato agent allow-read`: lets Claude Code read an image you pasted for the session.
- `legato agent session-created <task-id> <session-id>`: records a session an integration created.
- `legato mcp serve`: gives an agent Legato's tools over the Model Context Protocol. See [Legato's MCP tools](/legato/agent-tools/mcp-tools/).
- `legato credential git <credential> get|store|erase`: git's credential helper in an agent session with a GitHub credential.
- `legato credential gh-token -- <gh arguments>`: gives Legato's `gh` wrapper a token for the repository a `gh` command acts on.
