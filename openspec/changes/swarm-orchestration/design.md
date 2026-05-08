## Context

Legato today spawns a single tmux-backed agent per task. Users running large-scope tickets (e.g. "build a feature touching API + UI + docs") must either babysit one agent for hours or hand-decompose the work and run multiple legato sessions side-by-side. There is no mechanism for two agents to coordinate on a single ticket without merge conflicts.

The infrastructure to do better is already in place: SQLite-backed task state, tmux session lifecycle, IPC broadcast, hook-driven activity tracking, and the `AIToolAdapter` abstraction. What's missing is (1) a sub-task data model under a parent task, (2) a file-ownership scope mechanism, (3) a state machine that connects builder → reviewer transitions, and (4) role-aware spawn that injects different system prompts.

This design draws inspiration from BridgeMind's BridgeSwarm but adapts it to legato's existing primitives — we are not copying their orchestration architecture (which is opaque) but mirroring their public-facing role model.

## Goals / Non-Goals

**Goals:**
- Allow a user to decompose any task (local or remote-tracking) into N sub-tasks with explicit file scopes and roles.
- Prevent merge conflicts by refusing concurrent spawns that touch overlapping files.
- Auto-spawn a reviewer agent when a builder finishes; auto-respawn the builder if rejected.
- Expose a coordination surface (JSON snapshot) so swarm agents can self-orient via the existing CLI.
- Inject role-specific system prompts via the adapter interface so each agent boots with appropriate context.
- Surface the swarm graph in the TUI (board badge, detail view section, agent split-view panel).

**Non-Goals:**
- Automatic decomposition by an LLM. v1 requires the user (or a coordinator agent invoked manually) to specify sub-tasks. We expose the primitives; auto-decomposition is a future change.
- Web UI integration. v1 is TUI + CLI only. The web UI can render swarm state in a follow-up.
- Cross-task swarms (one swarm spanning multiple parent tasks). Sub-tasks have exactly one parent.
- Custom roles beyond `coordinator|builder|scout|reviewer`. We reserve the column type for future expansion but ship with these four.
- Branch isolation per sub-task (git worktrees). All sub-task agents share the working tree; file ownership is the enforcement mechanism. Worktree-per-sub-task is a future option.

## Decisions

### 1. Sub-tasks are a separate table, not a self-join on `tasks`

We introduce `swarm_subtasks` rather than adding `parent_task_id` to `tasks`.

**Rationale**: Sub-tasks have different lifecycles, different fields (`scope_globs`, `role`, `builder_agent_id`, `reviewer_agent_id`), and different display semantics. They are not "tasks that happen to have a parent" — they are work units inside a swarm. Keeping them separate avoids muddying every existing query (`ListTasksByStatus`, `ArchiveDoneCards`, etc.) with `WHERE parent_id IS NULL` filters.

**Alternative**: Self-join on `tasks` with `parent_task_id`. Rejected because nearly every existing query would need updating to exclude sub-tasks from the kanban view.

### 2. File ownership via globs, not exact paths

Sub-task scopes are glob patterns matched against repo-relative paths.

**Rationale**: Globs match the way users think about ownership ("the API folder", "everything under web/"). Exact paths would require listing every file in advance, which is brittle and unhelpful when the agent creates new files. Globs naturally cover both existing and future files.

**Implementation**: Use `github.com/bmatcuk/doublestar/v4` (already commonly used in Go for this; small dep). Alternatively, since all existing legato code uses stdlib globs, we could use `filepath.Match` for simple cases — but `filepath.Match` doesn't support `**`, which we need. Adding doublestar is justified.

**Alternative**: Path prefixes only (no globs). Rejected because users routinely want patterns like `**/*.test.ts` ("all test files everywhere") that prefixes can't express.

### 3. Overlap detection is best-effort, not perfect

`ScopeOverlaps` walks the working directory and tests files against both glob sets. It is O(files × globs) but runs only at spawn time on a typical repo (~10k files), which is fast enough.

**Rationale**: Perfect overlap detection between two glob sets is undecidable in general (e.g., `*.go` vs `**/*.go` overlap depends on whether files exist in subdirs). Walking the file tree at spawn time gives us a concrete answer for the current state. False negatives (overlap exists but no files match yet) are acceptable — the worst case is two agents both creating the same file, which git itself will flag.

**Alternative**: Pure pattern intersection without filesystem. Rejected — too many edge cases, and the file walk is fast in practice.

### 4. Reviewer is a separate agent, not a continuation

When a builder completes, we spawn a *new* tmux session for the reviewer rather than reusing the builder's session.

**Rationale**: The reviewer needs a clean prompt, no builder history, and a different system prompt. Reusing the session would require the builder to "switch hats," which works poorly with terminal-based agents. The cost is one extra tmux session per sub-task, which is cheap. It also gives the user a separate pane to inspect the review independent of the build.

**Alternative**: Single agent, "now switch to review mode." Rejected — confuses context and complicates prompt injection.

### 5. Sub-task completion is signaled, not detected

The builder marks done by either (a) terminating its tmux session or (b) explicitly calling `legato swarm review` (intended for the reviewer, but builders can self-mark via `legato swarm built <subtask-id>`). Detection is not heuristic.

**Rationale**: We considered detecting completion from agent activity hooks (e.g., "no activity for N minutes" → done). Too fragile — many real workflows have lulls. Explicit signaling is honest and predictable.

**Alternative**: Inactivity-based detection. Rejected as unreliable.

### 6. Role-specific prompts are adapter-owned

The system prompt for a "builder" comes from the adapter's `RoleSystemPrompt("builder")`, not from a global config.

**Rationale**: Different agents have different prompt conventions, and the adapter knows its tool's idioms; the orchestration layer should not.

**Per-adapter delivery mechanism** (all first-class — no send-keys fallbacks needed for the supported tools):

- **Claude Code**: launches with `claude --append-system-prompt "$(cat <prompt-file>)"`. System-level guidance, persists for the session.
- **Chimera**: launches with `chimera --prompt "<role-prompt>"`. Treated as the initial prompt for the session — see Risks for the implication this has on prompt content.
- **OpenCode**: the generated `legato.ts` plugin reads `$LEGATO_AGENT_ROLE` at startup and injects the corresponding role prompt via OpenCode's plugin context API. No flag at launch — the plugin owns delivery.

Adapters that don't implement `RoleSystemPrompt` (interface assertion fails) cause the agent service to skip prompt injection. The agent still gets `LEGATO_AGENT_ROLE` and `LEGATO_PARENT_TASK_ID` env vars, so a tool without prompt-injection support is functional but relies more on the agent's own discipline than on hardcoded role guidance.

**Override path**: Users can override via config — `swarm.prompts.<role>.<adapter>` map in `config.yaml`. The adapter checks config first, falls back to a built-in default. This is the same pattern as `agents.tmux_options`.

### 7. Coordination surface is JSON-on-demand, not push

Agents query `legato swarm status <parent-id>` when they need it; we don't push updates into a shared file.

**Rationale**: Pull-on-demand fits the existing CLI/IPC model. Pushing would require a watch/poll mechanism inside each agent, which means a long-lived process per pane. Agents that want freshness can re-run the CLI between turns — it's <10ms.

**Alternative**: Maintain a `.legato/swarm/<parent-id>.json` file kept in sync with the DB. Rejected — adds disk I/O on every state change without a clear consumer that needs file-form. Can be added later if a use case appears.

### 8. Sequencing is automatic but bounded by user intent

When sub-task A is in `building` and sub-task B has a conflicting scope, B stays in `queued` until A leaves `building`. We then auto-spawn B.

**Rationale**: This matches the user's intent (they decomposed knowing the work overlaps) without requiring them to babysit transitions. The alternative — failing decomposition for any overlap — would force users to either narrow scopes (often impractical) or handle sequencing manually.

**Auto-spawn safety**: Auto-spawning requires the user to be running legato (TUI or `legato server`). If neither is running, the queue is processed when legato starts next. We do not auto-spawn from CLI-only operations.

## Risks / Trade-offs

- **Risk**: File-scope checks are best-effort; two agents *can* create overlapping files if neither file exists yet at spawn time. → **Mitigation**: Document this clearly. The most common case (overlapping directories with existing files) is caught. The remaining case is no worse than current legato (one agent, no scope check) and git will surface it on commit.
- **Risk**: Reviewer auto-spawn could overwhelm a user who decomposed a task into 8 builders. → **Mitigation**: Cap concurrent agents per swarm via `swarm.max_concurrent_agents` config (default: 4). When the cap is hit, completed reviewers free a slot. Builders past the cap stay queued even without scope conflicts.
- **Risk**: Different adapter implementations may have wildly different prompt-injection mechanics, making `RoleSystemPrompt` a leaky abstraction. → **Mitigation**: The adapter is responsible for actually injecting the prompt during spawn (via flags, env vars, or plugin generation). The interface returns a string; the adapter chooses how to consume it.
- **Risk**: Initial-prompt vs system-prompt semantics differ across tools. Claude Code's `--append-system-prompt` is persistent guidance ("you are a builder for the duration of this session"); Chimera's `--prompt` is closer to an initial user message that the agent responds to once. The same role prompt content may work as guidance in one tool and as a triggering instruction in another. → **Mitigation**: Role prompt files are written as standing instructions ("Your standing instructions for this session: …") that read sensibly in either context. Adapters MAY further wrap the prompt before delivery — e.g., the Chimera adapter could prepend "Acknowledge these instructions and wait for the user." The interface returns a string; adapters own framing.
- **Risk**: Manual `legato swarm review --reject` could leave orphan reviewer sessions. → **Mitigation**: Reject path explicitly kills the reviewer session before respawning the builder. Approve path also terminates the reviewer.
- **Trade-off**: Storing scope as JSON vs a relational `subtask_scopes` table. JSON keeps queries simple and the data is read as a unit (we never `WHERE scope LIKE …`). The cost is no SQL-level scope queries — fine for v1.
- **Trade-off**: `parent_task_id` on `agent_sessions` is denormalized (also derivable from `subtask_id`). We accept the duplication because (a) most queries want the parent without a join, (b) non-swarm agents have no sub-task. A `subtask_id` column is added on `agent_sessions` too for the swarm case.

## Migration Plan

1. Apply migration `011_swarm.sql` (creates `swarm_subtasks` table) and `012_agent_role.sql` (adds `role`, `parent_task_id`, `subtask_id` columns to `agent_sessions`).
2. Existing rows are unaffected — defaults are empty/NULL, behavior is preserved.
3. No data backfill required.
4. Rollback: drop the new table and columns. Existing tasks and agents are unaffected. Sub-task data is lost on rollback (acceptable — this is opt-in functionality).

## Open Questions

- Should the coordinator role spawn an actual agent in v1, or is "coordinator" just a tag on the user (i.e., the user decomposes manually, no coordinator agent runs)? → **Tentative answer**: v1 ships with `coordinator` reserved but not auto-spawned. Future work: a coordinator agent that runs `legato swarm decompose` from a natural-language goal.
- Do we want git worktree per sub-task in v2? → **Out of scope for v1**, but worth noting that the `scope_globs` model maps cleanly onto worktree paths if we add it later.
- How should the web UI render swarm state? → **Defer to a follow-up change**. The data model is JSON-friendly so any UI can consume it via the existing `/health` or new `/api/swarm/<parent-id>` endpoint.
