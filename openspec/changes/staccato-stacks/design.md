## Context

A worker is a Legato server (today: [a server of its own](../../../site/src/content/docs/running/agent-server.md), added to the web UI as a second server) whose agents work in job clones under `<data_dir>/jobs/<execution>/<name>`, usually sandboxed, pushing through the GitHub credential's proxy. The agent guidance tells them to ask for `git.push` on one branch, `legato/<task-id>-<slug>`, before the first push. Staccato (`st`) keeps a stack's graph in `.git/stack/graph.json` and pushes each branch with `git push -u origin <b> --force-with-lease`.

Staccato's change `release-and-graph-transfer` gives `st` releases, `st version`, `st update`, `st graph export`/`import` (a versioned document, format 1, merged per branch on import), and `failed_pushes` from `st sync`. Main/worker handoff is designed but not built (`site/…/running/instances.md`), so the return that would carry a stack to the main instance does not exist yet.

Legato already reads job clones as files and never runs git in them from the host (`gitrepo.UnpushedTips`, `Head`, `StartBranch`): an agent controls its clone's `.git/config`, and settings such as `core.fsmonitor` make git run commands of the agent's choosing, outside its sandbox.

## Goals / Non-Goals

**Goals:**
- An agent can push a whole stack after one approval.
- Agents are told how to stack only where `st` can do the whole job.
- A worker's `st` stays current without anyone logging in.
- The person can get a finished stack into their own checkout today, from the web UI.

**Non-Goals:**
- Carrying the stack with a handoff's return, and importing it on the main instance automatically (the handoff work, which can reuse `task_stacks` and the document builder).
- Installing `st` where it is missing.
- Running `st` or git on the host in an agent's clone.
- Shared-mode Staccato graphs (`refs/staccato/…`); job clones are made from the repository cache, which fetches only branches, so `st` in them is always in local mode.

## Decisions

### Read the stack as files; never run `st graph export` on the host

`internal/engine/stackgraph` reads `.git/stack/graph.json` and the refs through an `os.Root`, as `gitrepo` does, and builds Staccato's format-1 document itself.

- *Alternative: run `st graph export` in the clone.* It is Staccato's own code and format, but `st` runs git, and git honours the clone's `.git/config`: the agent could make Legato run any command outside its sandbox. Environment overrides (`GIT_CONFIG_COUNT`) can switch off some settings (`core.fsmonitor`, `core.hooksPath`) but not all of the ways config runs programs, and a list that must be complete is a list that will be incomplete.
- *Alternative: run it in a fresh bubblewrap sandbox with the clone read-only.* Safe, but only where the sandbox is on and `bwrap` exists, slower, and still a second program to supervise for what is a JSON file and a few refs.
- *Alternative: the agent exports and hands Legato the document.* Needs no reading at all, but depends on the agent remembering, at the right moment, after its last push. It could be added later as a tool; the file reading stays the record.

The price is that Legato writes a format Staccato owns. Two things keep them together: the document is validated by `st graph import`, which refuses anything malformed or of a newer format; and an integration test, run when `st` with `graph export` is installed, builds a stack with `st` and checks that Legato's document equals `st graph export --all`'s apart from `st_version`.

`stackgraph` may not import `gitrepo` (engine packages do not import each other), and need not: it opens the clone's `.git` as an `os.Root` and reads through `Root.FS()`, so it parses loose refs and `packed-refs` itself (a few lines that `gitrepo` also has).

Branch names come from a file the agent wrote, so they are checked against git's branch-name rules before being used as paths; `os.Root` is the second wall.

### Snapshots, not a live view only

A clone is removed by the sweep or the person, and the stack is needed after: the agent may be done for days before the person looks. So Legato records the stack in `clone_stacks` (one row per clone; a task's stacks are those of the clones `job_clone_tasks` lists for it, since one clone can serve several tasks) when a session that worked in the clone ends, before removal, and on each request while the clone exists (so the view is never older than the request). Replaced each time; no history, since the branches on GitHub are the history. The document is not stored: it is made from the recorded branches when asked for, with the same `stackgraph` code.

The session end is found from `EventAgentDied`, whose task's live clones are read; removal through a hook in `RepositoryService.RemoveJobClone` and `SweepJobClones`, before the directory goes.

### A download now, an import later

The web UI offers the document only when every branch is pushed, since `st graph import` fetches each `head_sha` and fails on a commit it cannot get. The person downloads it from whichever server the web UI is talking to (the worker, as a second server) and runs `st graph import` in their checkout. When handoff exists, the return carries the same document and the main instance can offer to run the import in the repository's `primary_dir`, which is a host-side `st` run in the person's own checkout, not an agent's.

### One grant for the stack: a branch pattern scoped to the task

Each `st sync` pushes every branch; with one-branch grants, each new branch fails until the person allows it, one prompt per branch. The broker already takes a pattern as a resource (shown as **Broader scope**), and `Covers` is `doublestar.Match`, whose `*` does not cross `/`: a grant on `…@refs/heads/legato/t1-*` covers `legato/t1-a` and `legato/t1-part-2`, not `legato/t1-a/b` and not `legato/t2-a`. The default rule for `legato/**` (`session`) decides it, so the person is asked once. The one-branch advice stays for work without a stack.

- *Alternative: let the broker widen a one-branch grant to the task's prefix by itself.* Fewer words in the guidance, but a grant broader than what was asked for and shown.

A forced push needs nothing new: `ClassifyRefUpdate` looks at the ref, not at whether the update is a fast-forward, and `--force-with-lease` is checked by git against the advertisement the proxy passes through. Tests pin both.

### Guidance only when `st` can finish the job

Stack guidance without `failed_pushes` would have agents miss refused pushes, and without `graph export` the stack could not come back. Version numbers are not known yet, so the check is the feature: `st graph import --help` exits 0 (Staccato ships export, import and `failed_pushes` in one release). `legato mcp serve` outside a sandbox checks once when it starts. A sandboxed session's instructions are built by the host, which checks for them and for the Codex and Chimera launch prompt (built once at start) and keeps the answer a minute; the `st` the sandbox mounts is the host's.

### `st` updates ride on Legato's updater

`UpdateService` gains a tool updater for `st`: found with `exec.LookPath` on Legato's `PATH` (the agent-server unit's `PATH` starts with `~/.local/bin`), managed only when `st version --json` says `release: true`, checked with `st update --check --json` at each Legato check, installed with `st update`. `st` does its own downloading and verification; Legato only runs it, with a 2-minute limit. It follows the same mode, but `auto` does not wait for agents to be quiet: unlike Legato, nothing holds `st` open, and `st update` replaces the file by rename. Its state is kept beside Legato's, and a failure of one never blocks the other.

- *Alternative: a systemd timer running `st update`.* No code, but a second schedule, no visibility in Settings, and no `ask` mode.
- *Alternative: Legato downloads `st` releases itself with `selfupdate`.* Duplicates what `st update` does, and two updaters for one binary disagree eventually.

## Risks / Trade-offs

- [Legato's document drifts from Staccato's format] → The integration test against `st graph export`; `st graph import` refuses what it cannot read; a new format is a Legato change, called out in the proposal.
- [An agent asks for a broader pattern (`legato/*`)] → The card shows **Broader scope** and the exact pattern; the person decides. The guidance names only the task's own prefix.
- [`graph.json` changes shape in a later `st`] → `stackgraph` reads only `root` and each branch's `name`, `parent`, `base_sha`, `head_sha`, ignores the rest, and reports a graph it cannot read as a problem instead of failing the request.
- [A stack read while `st` is writing `graph.json`] → `st` writes the file whole; a read that fails to parse is reported and retried at the next trigger.
- [The worker's `st` updated while an agent is mid-`st sync`] → The running process keeps the old file; the next command uses the new one.
- [Stack guidance on a host whose `st` lacks the features] → Not given; agents use plain git as today.

## Migration Plan

One migration adds `clone_stacks`. Nothing changes until `st` with graph transfer is installed: no guidance, no tool updater (an older `st` has no `version`), stacks still recorded only if `graph.json` exists. On the worker: install `st` with the `curl` line, after which the updater keeps it. Rollback: the table can stay; the guidance and updater switch off with `st` absent.

## Open Questions

- Should the main instance (before handoff exists) be able to fetch a worker's stack through the multi-server connection and run the import itself, instead of a download? Left for the handoff work.
- Should the broker refuse a `legato/…` pattern that does not start with the asking task's own ID? Not done here; the approval card shows it.
