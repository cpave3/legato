## ADDED Requirements

### Requirement: Stack guidance when st can transfer graphs
The guidance an agent gets (the MCP server's instructions for Claude Code, the launch prompt for Codex and Chimera; swarm conductors and workers get the same) SHALL include a paragraph about stacking with Staccato when the process that builds it finds `st` on its `PATH` and `st graph import --help` exits 0 within 2 seconds. `legato mcp serve` outside a sandbox checks when it starts; a host Legato, which builds the instructions of sandboxed sessions and the launch prompt (once, when it starts), keeps its answer for a minute. The paragraph SHALL tell the agent to name every branch of a stack `legato/<task-id>-<a few words>` with its own task ID, to push with `st sync`, that a non-zero exit of `st sync` means a branch was not pushed and `failed_pushes` (with `--json`) says why, that `pending ap-…` in a reason means the person is being asked and the agent is to carry on or end its turn and run `st sync` again after the `[legato approvals]` message, and to open the stack's pull requests with `st pr`. Without such an `st` the guidance SHALL NOT mention Staccato.

#### Scenario: st with graph transfer
- **WHEN** an agent is launched and `st graph import --help` exits 0
- **THEN** its guidance SHALL include the stack paragraph with its own task ID in the branch names

#### Scenario: st too old
- **WHEN** `st` is on the `PATH` but `st graph import --help` exits non-zero
- **THEN** the guidance SHALL NOT mention Staccato

#### Scenario: st hangs
- **WHEN** `st graph import --help` has not exited after 2 seconds
- **THEN** it SHALL be killed and the guidance SHALL NOT mention Staccato

### Requirement: Reading a stack from a job clone
`internal/engine/stackgraph` SHALL read a clone's Staccato stack without running git, `st` or any other program in it: the graph from `.git/stack/graph.json` (at most 1 MiB), and each branch's tip and its `origin` remote-tracking tip from `refs/heads/<name>` and `refs/remotes/origin/<name>` or `packed-refs`, all opened through an `os.Root` at the clone's `.git` directory, so no path in the graph can lead outside it. The graph is written by the agent and SHALL be treated as untrusted: a branch name SHALL be accepted only when it is a valid git branch name by git's rules (no `..`, no control characters, spaces, `~^:?*[\`, no leading `-` or `/`, no `.lock` component, no `@{`), and a branch with an invalid name SHALL be reported as invalid and left out. Each branch SHALL be read as `pushed` (tip equals its remote-tracking tip), `unpushed` (tip differs or has no remote-tracking tip) or `missing` (in the graph, no such branch). A clone with no `graph.json` has no stack. A clone whose graph is kept in a shared ref (`refs/staccato/…`) instead SHALL be reported as unsupported.

#### Scenario: Stack read as files
- **WHEN** a clone has `.git/stack/graph.json` with `legato/t1-a` on `main` and `legato/t1-b` on `legato/t1-a`, both pushed, one of them in `packed-refs`
- **THEN** the stack SHALL list both, with their parents and tips, as `pushed`

#### Scenario: Path escape in a branch name
- **WHEN** the graph names a branch `../../config`
- **THEN** the branch SHALL be reported as invalid and no file outside the clone's `.git` SHALL be opened

#### Scenario: No program runs
- **WHEN** a clone's `.git/config` sets `core.fsmonitor` to a command
- **THEN** reading its stack SHALL NOT run that command

#### Scenario: Unpushed branch
- **WHEN** `legato/t1-b` has a commit its remote-tracking branch does not
- **THEN** it SHALL be read as `unpushed`

### Requirement: The import document
Legato SHALL build, from a stack it has read, the document `st graph import` reads (Staccato's format 1): `{"format": 1, "kind": "staccato.graph", "st_version": "", "root", "branches": [{"name", "parent", "base_sha", "head_sha"}]}`, branches listed depth first from the root, parents before their children and siblings by name, `head_sha` the branch's tip. A document SHALL be offered only when every branch is `pushed` and none is invalid, so that every commit it names can be fetched by the import.

#### Scenario: Document order
- **WHEN** a stack has `a` on `main`, `c` and `b` on `a`, and `d` on `main`
- **THEN** the document SHALL list `a`, `b`, `c`, `d`

#### Scenario: Not importable
- **WHEN** one branch of a stack is `unpushed`
- **THEN** no document SHALL be offered, and the stack SHALL say which branch is not pushed

### Requirement: Recording a task's stacks
Legato SHALL keep, per job clone, the last stack it read (`clone_stacks`: clone, root, branches with their state, a problem when there is one, and when it was read); a task's stacks are those of the clones it works or worked in, and the document is made from the recorded branches when asked for. It SHALL read and record a clone's stack when an agent session that worked in the clone ends, before the clone is removed (by the person or by the sweep), and when the task's stacks are asked for while the clone exists. A record SHALL outlive its clone. A clone with no stack SHALL leave no record, and a record SHALL be replaced, not added to, on each read.

#### Scenario: Agent ends
- **WHEN** an agent session ends whose job clone has a stack
- **THEN** the task SHALL have a record of that stack

#### Scenario: Clone swept
- **WHEN** the sweep removes a job clone that has a stack
- **THEN** the stack SHALL be recorded before the clone is removed, and the record SHALL remain after

#### Scenario: Read while the clone exists
- **WHEN** the task's stacks are asked for and its clone still exists
- **THEN** the stack SHALL be read again and the answer SHALL be that reading

### Requirement: Stack API
`GET /api/tasks/{id}/stacks` SHALL return `{"stacks": [{"clone_id", "repo", "root", "read_at", "live", "problem", "importable", "branches": [{"name", "parent", "head_sha", "state"}]}]}`, `live` being true when the clone still exists. `GET /api/tasks/{id}/stacks/{clone_id}/document` SHALL return the document as `application/json` with `Content-Disposition: attachment; filename="<task-id>-<repository name>-stack.json"`, and `409` with the reason when the stack is not importable. Both SHALL require the web UI's token and answer `404` for an unknown task or clone.

#### Scenario: Download
- **WHEN** a task's stack is importable and its document is requested
- **THEN** the response SHALL be the document as an attachment named after the task and repository

#### Scenario: Not importable
- **WHEN** the document of a stack with an unpushed branch is requested
- **THEN** the response SHALL be `409` naming the branch

### Requirement: The stack in the web UI
A task's activity SHALL show each of its recorded stacks: the repository, the branches as a tree from the root with each branch's state, when it was read and whether the clone still exists. An importable stack SHALL offer **Download** and the command to run in the person's checkout of the repository, `st graph import <downloaded file>`, with a copy button. A stack that is not importable SHALL say why (the branches not pushed, missing or invalid, or an unsupported shared graph).

#### Scenario: Importable stack
- **WHEN** the person opens a task whose stack has every branch pushed
- **THEN** its activity SHALL show the stack with **Download** and the import command

#### Scenario: Unpushed stack
- **WHEN** a branch of the stack is not pushed
- **THEN** the activity SHALL show the stack, mark that branch, and offer no download
