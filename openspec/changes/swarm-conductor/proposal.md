## Why

The `swarm-orchestration` change shipped infrastructure (sub-task table, scope detection, role tagging, basic CLI) but the result is not a swarm — it's a parallel-spawn convenience that requires the user to manually decompose work, manually launch each AI tool inside each tmux pane, and manually prompt every agent. Once spawned, the agents have no way to coordinate, no shared understanding of the task, and no orchestrator that can adapt the plan as work unfolds. Without an active coordinator that owns delegation and replanning, "swarm" is the wrong word for what we built.

The right model — which this change introduces — is a **conductor agent** that explores the codebase, proposes a plan to the user, dispatches workers with focused briefs, observes their reports, sends them follow-up instructions when needed, and declares the swarm complete when the parent task's goal is met. Workers are ephemeral and focused; the conductor is the only "thinking" agent that holds the global picture. The IPC between conductor and workers is `tmux send-keys` — each delivered message becomes a new conversational turn for the receiving agent, which is exactly the semantic Claude Code, Chimera, and similar tools already implement.

## What Changes

- **BREAKING**: Replace the `swarm-orchestration` design (rigid `coordinator|builder|scout|reviewer` roles, manual decomposition overlay, auto-promote-on-death lifecycle) with a conductor-driven flow. The previous change is non-prod and is removed wholesale; reusable pieces (sub-task table, scope detection, agent role columns, store CRUD, env var injection) are kept.
- **New conductor agent role**: a long-running agent spawned per swarm with a system prompt that frames it as a project manager. It explores the working directory, drafts a plan, submits it for human approval, then dispatches and supervises workers.
- **New plan approval flow**: when the conductor calls `legato swarm propose-plan <file>`, legato surfaces the plan in a TUI overlay where the user can approve, reject with notes (sent back to the conductor for revision), or open `$EDITOR` to edit the YAML before approval. The CLI call blocks until a verdict is rendered.
- **New send-keys-based message bus**: `legato swarm message <subtask-id> "<text>"` and `legato swarm broadcast <parent-id> "<text>"` deliver text into the receiver's tmux pane as a new conversational turn. The conductor wakes up the same way — when a worker reports progress, finishes, or dies, legato types the notification into the conductor's pane.
- **New worker-facing CLI**: `legato swarm progress`, `legato swarm question`, `legato swarm built` — workers report back through legato; the conductor receives these as send-keysed messages.
- **New conductor-facing CLI**: `legato swarm propose-plan`, `legato swarm dispatch`, `legato swarm message`, `legato swarm broadcast`, `legato swarm close`, `legato swarm finish`.
- **Auto-launch becomes mandatory** for swarm participants: the spawning flow runs the AI tool's launch command via `tmux send-keys` immediately after creating the session. Adapters own the launch command construction. Without this, send-keys-as-IPC doesn't work.
- **Working-directory becomes a per-swarm input**: the user supplies a working directory when invoking a swarm; agents spawn there and scope globs are interpreted relative to it. Removes the broken "spawns in `~`" behavior of v0.
- **Per-sub-task agent picker**: each plan entry can specify the AI tool (e.g. `claude-code`, `chimera`); the default comes from `cfg.Swarm.DefaultAgent`. Lets the user mix tools within a single swarm.
- **No predefined role taxonomy**: the conductor names roles freely per plan ("frontend", "API rewrite", "migration"). The role becomes a label and a prompt-injection target, not an enum.
- **Recursive sub-tasks deferred**: a worker spawning sub-workers is conceptually clean but adds depth-management complexity. v1 forbids it; the conductor is the only delegator. Future change can add it.
- **Lifecycle simplifies**: sub-task transitions are `queued → dispatched → in_progress → reporting → done` (or `cancelled`). The auto-promote-to-`review`-on-death behavior from v0 is removed; the conductor decides what happens when a worker finishes or dies.
- **Kill paths publish events**: explicit kills via `KillAgent` publish `EventAgentDied` so the conductor (and the board) get notified. Board subscribes to `EventSwarmChanged` so badges update without manual refresh.

## Capabilities

### New Capabilities

- `swarm-conductor`: the conductor agent — its role prompt, exploration step, plan-then-dispatch lifecycle, replanning loop, completion semantics. The single brain of a swarm.
- `swarm-coordination`: the send-keys-based message bus, worker progress reporting, conductor wake-ups on worker events, broadcast and per-worker messaging.
- `swarm-lifecycle`: sub-task data model and state machine (`queued → dispatched → in_progress → reporting → done|cancelled`), parent task swarm metadata, working directory per swarm, per-sub-task agent picker.
- `swarm-plan-approval`: TUI overlay that surfaces conductor-proposed plans for human review (approve / edit-in-`$EDITOR` / reject with notes), CLI verb that blocks until verdict.
- `agent-auto-launch`: adapter-driven launch command construction and post-spawn `send-keys` execution, so spawned tmux sessions actually run the AI tool instead of dropping to a shell. Required for swarm participants and useful for single-task agents too.
- `file-ownership`: file-scope globs as a per-sub-task hint, scope-overlap detection at dispatch time, optional sequencing when scopes conflict. Reuses the v0 `internal/engine/swarm/` package.

### Modified Capabilities

- `agent-session`: workers and conductors carry `role`, `parent_task_id`, `subtask_id` (already present from v0); `SpawnAgent` now invokes the adapter-supplied launch command via `send-keys` after session creation.
- `ai-tool-adapter`: adapters gain `LaunchCommand(env, prompt)` for constructing the AI-tool launch line, in addition to the existing `RoleSystemPrompt` capability. The launch command becomes the actual delivery mechanism for prompt injection.
- `legato-cli`: replaces v0 swarm verbs (`decompose`, `built`, `review`, `assign`, `status`) with a conductor-facing set (`propose-plan`, `dispatch`, `message`, `broadcast`, `close`, `finish`) and a worker-facing set (`progress`, `question`, `built`). `status` is retained as the read-only snapshot.

## Impact

- **Removed**: the v0 swarm decomposition overlay (`internal/tui/overlay/swarm.go`), the `Decompose`/`MarkBuilt`/`Review`/`AssignNext` methods on `SwarmService`, the `coordinator|builder|scout|reviewer` role enum check, the auto-promote-on-builder-death behavior in `HandleAgentDied`, the `legato swarm decompose|review|assign|built` CLI subcommand handlers (the worker-side `built` returns under a worker namespace).
- **Reused**: `swarm_subtasks` table (status enum updated — migration), scope detection package, store CRUD helpers, `agent_sessions` role columns (reused as-is), env var injection (`LEGATO_AGENT_ROLE` etc.), IPC `swarm_changed` message type, embedded role prompts (kept as default conductor/worker prompts).
- **New code**:
  - `internal/service/swarm/conductor.go` — conductor lifecycle helpers, plan approval gate, send-keys delivery wrappers.
  - `internal/engine/swarm/plan.go` — plan format (YAML), parser, validator.
  - `internal/tui/overlay/plan_approval.go` — plan review overlay.
  - `internal/cli/swarm/*.go` — split conductor and worker CLI verb handlers.
- **Modified code**:
  - `internal/service/agent.go` — `SpawnAgent` calls adapter `LaunchCommand` and runs it via `tmux send-keys` after session creation. `KillAgent` publishes `EventAgentDied`.
  - `internal/engine/hooks/{claude_code,chimera}.go` — implement `LaunchCommand`. Embedded prompt files repurposed: rename to `conductor.md` and `worker.md`, drop the role-specific files (the conductor decides per-worker prompt content per plan).
  - `internal/engine/store/migrations/` — new migration: rename status values, add `working_dir`, `agent_kind`, `prompt` columns to `swarm_subtasks`; add `parent_dispatched_at` for ordering.
  - `internal/tui/board/` — subscribe to `EventSwarmChanged` so badges refresh live; replace v0 `s` keybinding (decompose) with `S` (start swarm) which spawns the conductor and opens the plan-approval overlay when the conductor proposes.
  - `internal/tui/detail/` — remove v0 swarm graph navigation (`J/K`, `a`/`r`); replace with read-only swarm state view since approve/reject is no longer a per-sub-task TUI action (the conductor owns review semantics).
  - `cmd/legato/main.go` — register conductor adapter; reshape `runSwarmCmd` to dispatch conductor and worker subcommands.
- **Config**: extend `cfg.Swarm` with `DefaultAgent string`, retain `MaxConcurrentAgents`, retain `Prompts` (now keyed by role-as-label rather than fixed enum).
- **No new third-party deps**.
- **Breaking for users on v0 swarm features**: anyone who used the v0 decomposition overlay or `legato swarm decompose` will find those removed. Since v0 is non-prod and never reached an actual usable state (the user-reported issues are exactly why v0 is being scrapped), this is acceptable.
- **The previous `swarm-orchestration` openspec change should be archived as superseded** — its specs never landed in `openspec/specs/`, so there is no canonical-spec rollback to perform.
