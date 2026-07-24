## Context

The v0 `swarm-orchestration` change shipped data structures and CLI verbs but produced an unusable product: spawned tmux sessions held shells (no AI tool launched), the decomposition overlay had no description field, agents had no way to coordinate, and the working directory was always wherever legato was launched from. The user's review surfaced four interlocking gaps: (1) work delegation never actually happens — no prompt is injected with the sub-task description, (2) without auto-launch, every "swarm" requires N manual `claude` invocations, (3) without a coordinating brain, parallel agents are not a swarm, and (4) without a working-directory concept, scope globs are meaningless.

Through several rounds of design conversation we converged on a model where the only "thinking" agent is a **conductor** that explores, plans, dispatches, observes, and finishes. Workers are ephemeral focused agents with narrow briefs. The IPC between them is `tmux send-keys` — each delivered message is a new conversational turn for the receiving agent, exactly the semantic that conversational coding agents already implement. Legato is the state store, the message router, and the human-in-the-loop gate for plan approval.

This is non-prod: v0's spec specs never reached `openspec/specs/`, so we are not rolling back published behavior. We can remove v0 wiring freely and only retain pieces that fit the new shape.

Current state being kept:

- `swarm_subtasks` table (status enum updated; new columns added).
- `internal/engine/swarm/scope.go` — `MatchScope`, `ScopeOverlaps`, `ValidateScope` are unchanged; the doublestar dep stays.
- `agent_sessions` columns `role`, `parent_task_id`, `subtask_id` from migration `013_agent_role.sql`.
- Env var injection (`LEGATO_AGENT_ROLE`, `LEGATO_PARENT_TASK_ID`, `LEGATO_SUBTASK_ID`).
- IPC message type `swarm_changed`.
- `RolePromptingAdapter` interface — the prompt-content side stays; only the delivery mechanism changes.
- Embedded prompt files — repurposed for conductor and worker defaults.

Current state being removed:

- `internal/tui/overlay/swarm.go` (decomposition overlay).
- `SwarmService.Decompose`, `MarkBuilt`, `Review`, `AssignNext`, `HandleAgentDied` (all replaced with conductor-driven equivalents).
- `legato swarm decompose|review|assign|built` CLI handlers (worker `built` returns under a different command shape).
- Detail view's `J/K`/`a`/`r` swarm review keybindings.
- The hardcoded `coordinator|builder|scout|reviewer` role enum check in `SwarmService.validRole`.
- The "auto-promote builder to review on death" logic.

## Goals / Non-Goals

**Goals:**

- A user can press `S` on a board card, supply a working directory, and within a few minutes have an agent that has read the relevant code, proposed a plan, and is waiting on human approval to dispatch workers.
- Once the plan is approved, the conductor dispatches workers without further user interaction. Each worker receives its full brief (parent context, sub-task description, scope, completion instructions) at launch — no manual prompting.
- Workers can report progress, ask questions, and signal completion via the legato CLI. The conductor receives these as new conversational turns delivered via `send-keys`.
- The conductor can send follow-up instructions to specific workers or broadcast to the whole swarm.
- When all workers report done, the conductor decides whether the parent goal is met. If yes, it cleans up and reports. If not, it dispatches additional work.
- Working directory is per-swarm. Globs are repo-relative to that directory. Agents spawn there.
- The user can use a mix of AI tools across workers within one swarm.
- The board reflects swarm progress live — badges update as sub-tasks transition without requiring manual refresh.
- Existing single-task spawn (the non-swarm flow) gets auto-launch as a free side-effect, removing the "spawn a shell, manually run `claude`" friction across the product.

**Non-Goals:**

- **Recursive sub-tasks**: workers cannot spawn their own workers in v1. Only the conductor delegates. Future change can lift this.
- **Multi-conductor swarms**: one conductor per swarm. Two conductors collaborating on the same parent is out of scope.
- **Web UI integration**: TUI + CLI only in v1. Web UI follow-up later.
- **Conversational backchannel between workers**: workers don't talk to each other. They report up; the conductor relays if needed.
- **Persistent worker context across tasks**: each worker is a fresh agent for its sub-task. No "the same backend agent that did task A last week."
- **Auto-approval of plans**: plans always go through HITL approval in v1. A future `--auto` mode is plausible once we trust the conductor's plans, but we should not ship without the gate.
- **Send-keys to non-conversational agents**: workers must be conversational AI tools (Claude Code, Chimera, Cursor, etc.). Configuring a one-shot script as a worker is undefined behavior.
- **Per-sub-task reviewer agents**: v0's reviewer auto-spawn is gone. The conductor adjudicates worker output (or asks the user to). If we want a dedicated reviewer pattern, that's a future template the conductor can express in its plan, not a built-in lifecycle stage.

## Decisions

### 1. The conductor is the only delegator. Workers are ephemeral.

Workers boot for a specific sub-task and exit when that sub-task is done (or the conductor closes them). They do not pull new work from a queue. They do not coordinate with siblings. They report up; the conductor decides what to do with the report.

**Rationale**: Pull-based worker pools introduce a fetch-loop in the worker that conversational agents handle poorly (they'd need to know to call `legato swarm next` and then wait on it, and "wait" is uncomfortable for an LLM). Spawn-on-dispatch lets each worker's launch prompt be specific and complete. The lifecycle stays simple: each tmux session is one job.

**Alternative considered**: persistent role-based workers (the "backend expert that owns api/**" model). Rejected because (a) long-lived agent context bloats fast, (b) scope-as-ownership creates unwanted serialization, (c) it conflates "this agent has expertise" with "this agent has the lock on these files," which are different concerns.

### 2. The conductor is itself an agent, not a Go service.

We do not write a Go-side "swarm orchestrator" that programmatically decomposes tasks. The conductor is an LLM-driven agent spawned in its own tmux pane with a system prompt that frames it as a project manager and a CLI reference for delegation.

**Rationale**: Decomposition is a semantic problem (read code, understand domains, draft a plan). LLMs are the right tool. A Go-side orchestrator would be either a thin wrapper around an LLM call (then why hide it?) or a deterministic decomposer (which can't actually do the job). Putting the conductor in the same form factor as a worker also means the user can attach to its pane, see what it's thinking, and intervene directly.

**Alternative considered**: hybrid — Go service drafts a structural plan, LLM fills in details. Rejected because the structural part is the easy part; what makes a plan good is judgement about what's actually involved, which is the LLM's job.

### 3. Send-keys is the IPC.

When the conductor calls `legato swarm message <subtask-id> "<text>"`, legato runs `tmux send-keys -t <session> "<text>" Enter`. The text appears at the worker's prompt and is processed as the next conversational turn. Same direction reverses: workers calling `legato swarm progress` causes legato to type a notification into the conductor's pane.

**Rationale**: Conversational coding agents already implement turn-by-turn input. We don't need to invent a transport. We don't need long-lived processes inside agents to receive messages — the agent's natural idle-after-turn state *is* the receive state. Send-keys is also crash-resilient: if a message fails to deliver, the receiver simply doesn't get the turn; legato logs the failure and the conductor can retry.

**Alternative considered**: a side-channel API (HTTP or unix socket) that agents poll. Rejected — agents would need a polling loop, every conversational tool would need to be modified to support it, and we'd be reinventing what tmux already provides.

**Risk acknowledged in the Risks section**: send-keys arriving mid-turn appends to the next prompt. Latency is "until the current turn finishes." This matches the natural ergonomics of a human interrupting an agent and is acceptable.

### 4. Plans are submitted via CLI, approved via TUI overlay.

The conductor writes its plan to a YAML file in the swarm's working directory and calls `legato swarm propose-plan <file>`. That CLI call blocks. Legato persists the plan, broadcasts an IPC `plan_proposed` message, and surfaces a TUI overlay in any running instance. The user can:

- `y` — approve. The CLI returns success; the conductor proceeds to dispatch.
- `e` — open the YAML in `$EDITOR`, save, return — the edited plan becomes the approved plan.
- `n` — reject with notes. The CLI returns a rejection payload; the notes are send-keysed to the conductor's pane so it can revise.

**Rationale**: YAML is the data; the overlay is the UX. Splitting them lets the conductor produce a structured artifact (which we can persist, diff, replay) while the user gets a friendly review surface. Editing in `$EDITOR` is the escape hatch for "this plan is 80% right but I want to tweak."

**Alternative considered**: free-form approval in the conductor's pane (user types "yes" or "no" while attached). Rejected — it requires the user to be attached to the conductor's tmux pane, doesn't surface plans to other open legato instances, and provides no editable artifact.

### 5. Worker prompt content is conductor-supplied, not template-rendered.

When the conductor's plan declares a sub-task, it includes a `prompt` field — the literal initial-user-message that legato will deliver to the worker after launch. Legato does not template parent description + sub-task description + scope into a fixed format. The conductor is responsible for writing a good brief.

**Rationale**: The conductor knows what context each worker actually needs. A backend worker might need just an API spec and the relevant interface file paths. A docs worker might need a links list. A migration worker might need the production schema. Template-driven prompts produce identical-shaped briefs whether or not the work justifies them. Letting the conductor write the prompt is more flexible and matches the "delegate by goal, not by procedure" instinct from the design conversation.

**Tradeoff**: bad conductor prompts produce bad worker briefs. We mitigate this by giving the conductor's role prompt very explicit guidance on what a good worker brief contains, and by surfacing the worker prompt in the plan-approval overlay so the user can sanity-check it.

**Alternative considered**: legato renders a fixed template (parent + subtask + scope + siblings + completion instructions). Rejected for inflexibility, but we keep this as a fallback — if the conductor leaves `prompt` empty, legato fills in a sensible default template. Prevents accidentally-empty briefs.

### 6. Auto-launch is mandatory for swarm participants and a free upgrade for single-task spawn.

`SpawnAgent` always tries to invoke the adapter's `LaunchCommand` and run it via `send-keys` after session creation. If the adapter doesn't implement `LaunchCommand` (interface assertion fails) or returns an empty command, we fall back to the v0 behavior — drop into a shell with env vars set. Single-task spawn benefits from auto-launch even though it's not part of a swarm.

**Rationale**: The current "spawn a shell, manually run claude" UX is bad outside swarm too. Adapter-driven launch is a small extension that removes friction everywhere.

**Adapter responsibilities**:

- `ClaudeCodeAdapter.LaunchCommand` returns `claude --append-system-prompt "<role prompt>" --print "<initial brief>"` (or equivalent — Claude Code's exact CLI shape determines this).
- `ChimeraAdapter.LaunchCommand` returns `chimera --prompt "<combined>"`.
- `OpenCodeAdapter` (when added) generates a plugin file and launches `opencode`.

The launch command construction reads `LEGATO_ROLE_PROMPT` and the worker's per-task brief from env vars; legato sets these before invoking the adapter so the adapter doesn't need direct access to the plan.

### 7. Working directory is per-swarm input, supplied at swarm initiation.

The user supplies the working directory when starting a swarm (a small overlay before the conductor spawns). The directory is stored on the parent task's swarm metadata and propagated to every spawned agent and to `ScopeOverlaps`. We do not infer it from workspace config or git remote in v1 — the user types it (with autocomplete from recent dirs).

**Rationale**: Inference is a usability win but a correctness risk (wrong dir = wrong work). Explicit input is honest. We can add inference (default to workspace path or current git repo) as a UX layer on top later. For v1, the path is mandatory and the user types it.

**Alternative considered**: store path on workspace config (per-workspace `path: ~/Projects/foo`). Rejected for now because workspaces are a logical organization concept and conflating them with filesystem locations forces every workspace to map 1:1 to a project dir. The next change after this one can add `workspace.path` as a default for the swarm-init overlay's text field.

### 8. Sub-task lifecycle is `queued → dispatched → in_progress → reporting → done | cancelled`.

- `queued` — exists in plan, not yet spawned.
- `dispatched` — agent spawn has been requested; tmux session exists.
- `in_progress` — agent has acknowledged its brief (sent its first progress report or any output we observe).
- `reporting` — worker has called `legato swarm built`; conductor has not yet ratified.
- `done` — conductor has called `legato swarm close <subtask-id>` (which terminates the session).
- `cancelled` — conductor or user explicitly killed the worker before completion.

**Rationale**: v0's `building → review` had `review` mean "needs human approval." That's wrong for the conductor model — the conductor is responsible for review (or for asking the user). `reporting` is purely a state where the worker has signaled done but the conductor hasn't yet confirmed. The conductor calls `close` to ratify.

**Auto-promote-on-death is removed.** When a worker's tmux session dies unexpectedly, legato publishes `EventAgentDied` and the conductor decides what to do — by receiving a send-keysed notification ("worker X has died, exit code Y"). Death is a fact, not a transition.

### 9. Kill paths publish `EventAgentDied`.

`agentService.KillAgent` and `ReconcileSessions` both publish the event. Previously only the reconciliation path did. The conductor's wake-up loop relies on this — explicit kills (user pressed K in the agent view, conductor called `swarm close`) need to flow through the same notification channel as natural deaths.

### 10. Scope is a hint, not a hard upfront declaration.

In v0 `scope_globs` was set at decomposition time and `ScopeOverlaps` ran at spawn to refuse conflicts. In v1 the conductor declares scope per worker in its plan, but conflicts at spawn time become a *signal to the conductor* (delivered via send-keys to the conductor pane: "worker X scope conflicts with active sibling Y") rather than a hard refusal. The conductor decides whether to wait, narrow the scope, or proceed anyway.

**Rationale**: Hard refusal is too blunt. The conductor has more context — maybe the conflict is fine because the two workers will touch different files within the shared directory. Letting the conductor adjudicate is consistent with "conductor owns coordination."

**Tradeoff**: a misbehaving conductor can ignore conflicts and let two workers stomp each other. We keep the *advisory* check and let the conductor see it; if the conductor proceeds, that's its call. The user can also configure `swarm.strict_scope: true` to make conflicts hard-block, for users who'd rather force the conductor to resolve them upfront.

### 11. Plan format: YAML with required and optional fields.

```yaml
swarm:
  parent_task_id: abc12345
  working_dir: /home/user/Projects/myapp
  summary: |
    Brief markdown describing what this swarm will do, surfaced in the
    approval overlay. Required.

subtasks:
  - title: "API auth refactor"
    role: "backend"             # free-form label, used in UI + role prompt selection
    agent: "claude-code"        # optional; defaults to cfg.Swarm.DefaultAgent
    scope: ["api/auth/**"]      # optional; advisory hint
    prompt: |
      You are working on the API auth refactor as part of a larger OAuth2
      migration. The parent task is ABC-123 in the kanban.

      Read api/auth/*.go and the routing in api/router.go to understand
      the current auth middleware. Replace the in-memory token store with
      OAuth2-token-storage in api/auth/oauth.go.

      Do not touch web/, db/, or docs/.

      When done, run: legato swarm built $LEGATO_SUBTASK_ID
```

**Validation**: `parent_task_id` and `working_dir` required, at least one sub-task, each sub-task must have `title` and `prompt` (or omit `prompt` and accept the default template). `scope` is optional and validated for glob syntax. `role` is free-form text; if it matches a key in `cfg.Swarm.Prompts`, that prompt becomes the worker's `LEGATO_ROLE_PROMPT`. Otherwise legato writes the default worker prompt.

### 12. The conductor sleeps between turns; legato wakes it.

The conductor's natural state after dispatching is "I have nothing to do until something changes." It finishes its turn and goes idle. When something changes — a worker reports progress, a worker dies, a worker asks a question — legato `send-keys`es a structured notification into the conductor pane:

```
[swarm event] worker "API auth refactor" (st-3f9a2) reported progress:
  > "Reviewed existing middleware. Plan: extract token store to interface,
  >  add OAuth2 implementation, swap at the router layer."

[swarm event] worker "Migration script" (st-7c1d8) marked itself built.
  Run `legato swarm close st-7c1d8` to terminate, or send a follow-up.

[swarm event] all workers in this swarm are idle (built or queued).
  Decide: dispatch more, ask the user, or call `legato swarm finish`.
```

The conductor's role prompt explicitly tells it to handle these `[swarm event]` messages as the next turn — not as user input it should respond to colloquially, but as state changes it should decide on.

### 13. Single conductor process, single user-facing TUI gate, n workers.

The conductor lives in `legato-<parent-id>` (the parent task's normal agent session — we reuse that slot). Workers live in `legato-<subtask-id>`. The TUI's agent split-view shows them all; the parent slot is implicitly "this is the conductor."

This means starting a swarm on a parent task replaces what would have been a normal single agent for that task with a swarm conductor. The two are mutually exclusive — you can't have both a regular agent and a conductor for the same parent. Pressing `S` on a card that already has a regular agent attached prompts the user to kill the agent and re-spawn as conductor (or cancel).

## Risks / Trade-offs

- **Risk**: Conductor agents that don't follow the plan-then-dispatch flow — they decide to "just do the work themselves" and never spawn workers. → **Mitigation**: The conductor's role prompt is explicit and includes negative examples ("do not write production code yourself; your only output is the plan and CLI invocations"). We accept some failure rate; the user can interrupt and re-launch.

- **Risk**: Send-keys timing — a message arrives while the conductor is mid-turn. → **Mitigation**: Tmux queues the input at the prompt, processed after the current turn finishes. Latency is bounded by turn duration. Documented as expected behavior.

- **Risk**: Send-keys for multi-line content gets garbled (newlines treated as Enter). → **Mitigation**: We base64-encode the payload and have the conductor's prompt include a note that any `[swarm event b64:...]` line should be decoded before processing. Alternative: write the message to a file and send-keys a path reference. The base64 approach is simpler and avoids file lifecycle.

- **Risk**: A worker reports `built` but its work is wrong. The conductor closes it (state → done) without noticing. → **Mitigation**: The conductor's prompt instructs it to either inspect the worker's diff itself before closing or send-keys an inspection request to the user. Both options exist; the conductor's judgement determines which is appropriate. In strict environments, `cfg.Swarm.RequireUserClose: true` adds a HITL gate at every close.

- **Risk**: Plan approval overlay fires when no legato instance is open (CLI-only environment). → **Mitigation**: `legato swarm propose-plan` can also accept `--auto-approve` for headless contexts (useful for scripted swarms). With no auto-approve and no UI, the call blocks indefinitely. Acceptable; we document it.

- **Risk**: The conductor produces an enormous plan (50 sub-tasks). → **Mitigation**: `cfg.Swarm.MaxSubtasksPerPlan` (default 10). Plans exceeding this are rejected at the `propose-plan` validator; the conductor must re-plan smaller. This is also a useful signal that decomposition is too fine.

- **Risk**: Conductor spawn fails silently (e.g., adapter not configured, working dir doesn't exist). → **Mitigation**: The swarm-start overlay validates the working directory exists, the adapter is registered, and the parent task isn't already running an agent. Failures show in-overlay before the conductor is spawned.

- **Risk**: Two conductors are accidentally spawned for the same parent. → **Mitigation**: `parent_task_id` on `agent_sessions` plus a unique constraint or pre-spawn check ensures a parent can have at most one running conductor.

- **Risk**: Workers report progress in tight bursts; the conductor pane fills with `[swarm event]` lines before it processes any of them. → **Mitigation**: legato batches progress events with a 1s debounce per worker — multiple progress reports from the same worker within a window collapse into the latest. `built` and `died` events bypass the debounce.

- **Risk**: Removing v0 swarm code breaks something we didn't notice. → **Mitigation**: full test pass before merge. v0 is non-prod and feature-flagged in practice (no one's depending on it).

- **Trade-off**: The conductor is in the loop for every transition. If the user just wants "spawn 3 builders and let them run," they pay the conductor's tokens for what feels like overhead. → **Counter**: that's the wrong shape for legato — if you don't want coordination, don't use a swarm. The single-agent flow (with auto-launch from this change) handles the simple case fine.

- **Trade-off**: YAML plans add a new format to learn. → **Counter**: the conductor writes them; the user reads/edits. Most users will never write one from scratch. The format is small (~5 fields per sub-task) and we surface a markdown preview in the approval overlay.

- **Trade-off**: Free-form role names mean the user can't visually associate a swarm role with a known concept across swarms. → **Counter**: roles are mostly cosmetic in v1 (label + prompt selection). Cross-swarm consistency is the conductor's responsibility, not the data model's. If we want it later we can introduce optional named templates.

## Migration Plan

1. Apply migration `014_swarm_v1.sql`:
   - Rename `swarm_subtasks.status` enum values: `building → in_progress`, `review → reporting`. `done` and `queued` unchanged. `rejected → cancelled`.
   - Add columns to `swarm_subtasks`: `agent_kind TEXT NOT NULL DEFAULT ''`, `prompt TEXT NOT NULL DEFAULT ''`, `dispatched_at DATETIME NULL`.
   - Add column to `tasks`: `swarm_working_dir TEXT NULL` (parent-only metadata).
2. Existing v0 sub-task rows in any user's local DB get their status values rewritten in-place. Since v0 was unusable, anyone with rows is in development; data loss is acceptable. The migration is permissive (it doesn't fail on unexpected enum values).
3. Drop v0 code:
   - Delete `internal/tui/overlay/swarm.go` (decomposition) and its tests.
   - Delete `Decompose`, `MarkBuilt`, `Review`, `AssignNext`, `HandleAgentDied`, `StartEventLoop` from `SwarmService`. Replace with conductor-driven equivalents.
   - Delete `legato swarm decompose|review|assign|built` (worker-side `built` is reintroduced under `legato swarm built` with new semantics — same verb, different meaning, since the previous one was unused in practice).
   - Delete the role enum check in `validRole`. Replace with free-form role validation (non-empty, matches `[a-z0-9-]+`).
4. Add new code:
   - `internal/engine/swarm/plan.go` — YAML plan parser + validator.
   - `internal/service/swarm/conductor.go` — conductor lifecycle, plan approval gate, dispatch loop.
   - `internal/service/swarm/messages.go` — send-keys delivery wrappers, event formatting.
   - `internal/tui/overlay/swarm_init.go` — overlay collecting working-dir input, spawns the conductor.
   - `internal/tui/overlay/plan_approval.go` — overlay reviewing a proposed plan.
   - `internal/cli/swarm/conductor.go` and `internal/cli/swarm/worker.go` — split CLI handlers.
5. Modify adapters: implement `LaunchCommand` on `ClaudeCodeAdapter` and `ChimeraAdapter`. Repurpose embedded prompts to `conductor.md` and `worker.md` (drop the four role-specific files).
6. Modify `agentService.SpawnAgent` to call `LaunchCommand` after session creation. Modify `KillAgent` to publish `EventAgentDied`.
7. Modify `tui/board` to subscribe to `EventSwarmChanged` for live badge refresh.
8. Update CLAUDE.md docs: remove the swarm sections written for v0; add `docs/ai/swarm.md` v2 covering the conductor model.
9. Archive `swarm-orchestration` change (mark superseded; no spec rollback needed since v0's specs never landed in `openspec/specs/`).

**Rollback**: drop migration `014_swarm_v1.sql` and the v1 columns. Anyone who was using v0 remains broken (they were already broken — that's why we're doing this). Anyone on v1 loses their swarm history. Acceptable for non-prod.

## Open Questions

- **Should worker session names include the role label for easier tmux navigation?** E.g., `legato-st-3f9a2-backend-auth` vs. just `legato-st-3f9a2`. Pro: easier to identify when listing tmux. Con: longer names, less stable IDs. Lean toward keeping IDs short, surface labels in the agent split-view UI.
- **Conductor's role prompt — drafted by us or learned via iteration?** v1 ships an initial draft; it will need iteration based on actual conductor behavior. We should treat the prompt as a config-overridable file from day one.
- **What happens to the parent task's `status` column when a swarm starts?** Move to `Doing`? Leave alone? Lean toward "leave alone" — swarm state is orthogonal to kanban column. Status column reflects user intent; swarm state reflects work.
- **Does the conductor get the same `LEGATO_TASK_ID` env that single agents get for hook activity tracking?** Yes, set it to the parent task ID so existing hook scripts work unchanged. Conductor's "working" / "waiting" activity shows on the parent card.
- **Should `legato swarm finish` archive the parent task automatically?** Lean no — the user might want to keep it open for additional work. Leave archival to the user.
- **Mid-flight plan changes** — if the conductor decides mid-swarm to spawn additional sub-tasks, do they require re-approval? Lean no for additive changes (conductor calls `legato swarm dispatch <inline-subtask>`), yes if the conductor proposes a fundamentally different plan. The CLI shape determines this — `dispatch` accepts inline sub-task definition without going through `propose-plan`. If we want approval, conductor uses `propose-plan` for the diff.
