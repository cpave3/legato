## Why

An agent on a worker (a Legato server working in fresh clones) should be able to break its work into a stack of small branches with Staccato (`st`), push them all, and open their pull requests; the person should then be able to carry on with that stack on their own machine. Today an agent is told to ask for push access to one branch at a time, so a five-branch stack asks the person five times; `st` is not on a worker unless built there by hand and never updates; and the stack's graph stays in the worker's clone, so the person gets the branches but not the stack. Staccato is adding releases, `st update`, `st graph export`/`import` and push-failure reporting (its change `release-and-graph-transfer`); this is Legato's side.

## What Changes

- **Stack guidance.** When the session's `st` can export and import graphs, agents are told how to stack: branches named `legato/<task-id>-…`, one `request_access` for the pattern `refs/heads/legato/<task-id>-*` before the first push instead of one per branch, `st sync` to push (a non-zero exit with `pending ap-…` in `failed_pushes` means waiting for the person), `st pr` for pull requests, and running `st sync` again after the `[legato approvals]` message.
- **Legato keeps `st` up to date.** The updater also looks after a release build of `st` found on Legato's `PATH`: it checks with `st update --check --json` when it checks itself, and installs with `st update` under the same mode (`auto` installs at once, `ask` offers it in Settings › Updates and Attention, `off` never). A `dev` or `go install` build of `st` is left alone. Nothing installs `st` where it is missing; the agent-server guide gains the `curl` line.
- **A task's stack is recorded.** Legato reads the Staccato graph and branch tips of each of a task's job clones as files, without running git or `st` in the clone, and keeps a snapshot per clone: when the agent's session ends, before a clone is removed, and when the stack is asked for while the clone exists. Each branch is marked pushed or not.
- **The stack in the web UI, with a download.** The task's activity shows the stack (branches, parents, pushed or not) and, when every branch is pushed, a **Download** of the document `st graph import` reads, with the command to run. `GET /api/tasks/{id}/stacks` and `GET /api/tasks/{id}/stacks/{clone}/document`.
- **Push path checked for stacks.** Tests that a restack's forced push (`--force-with-lease`) of `legato/**` branches is classified and allowed like any other push, all branches of one `st sync` against one pattern grant.

Not in this change: the return of a handed-off task carrying the document, and importing it on the main instance. That belongs to the handoff work, which can use what this records.

## Capabilities

### New Capabilities
- `staccato-stacks`: when agents get stack guidance, how a task's stack is read from a job clone and recorded, the API, and the web UI's stack view and download.
- `tool-updates`: the updater keeping `st` current: finding it, checking, installing under the update mode, and reporting it.

### Modified Capabilities
- `github-credentials`: "Branch convention in the agent guidance" — with stack guidance in effect, agents ask once for the task's branch pattern instead of one branch.

## Impact

- New: `internal/engine/stackgraph` (reads `.git/stack/graph.json` and branch refs through `os.Root`, builds the format-1 document), `internal/service/update_tools.go`, `internal/service/task_stacks.go`, `internal/server/task_stacks.go`, a migration for `clone_stacks`, `web/src/components/board/TaskStacks.tsx`, `cmd/legato/stack_support.go`.
- Changed: `internal/cli/mcp_credential_guidance.go` and `mcp.go` (stack guidance), `internal/service/update.go`, `update_attention.go` and `internal/server/updates.go` (tool status), `internal/service/repository.go` (snapshot before a clone is removed), the agent service's end-of-session path, `web/src/components/UpdatesSettings.tsx`, `TaskActivityTabs.tsx`.
- Docs: `docs/ai/credentials.md`, `cli.md` § Staccato Integration, `updates.md`, `web-ui.md`, `database.md`; site pages `integrations/staccato`, `running/agent-server`, `running/updates`, `using/web-ui`.
- Depends on Staccato releasing `st version`, `st update`, `st graph export`/`import` and `failed_pushes`. The recorded document is Staccato's format 1; Legato writes it, so a later format means a change here.
