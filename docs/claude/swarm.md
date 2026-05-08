## Swarm Conductor

Swarm orchestration in legato is conductor-driven: pressing `S` on a card spawns a single LLM agent (the **conductor**) that explores the codebase, drafts a plan, asks the user to approve it, then dispatches workers and supervises them. Workers are ephemeral focused agents; the conductor is the only "thinking" agent that holds the global picture.

### Lifecycle

```
  user presses S          conductor      user           conductor      workers       conductor
  ────────────────►   ── proposes ──►   approves   ── dispatches ──►   work    ── supervises ──►   finish
  (working dir)         a plan          (or edit)     N workers       + report      + closes
```

1. **Start.** User presses `S` on a parent card → swarm-init overlay → user supplies working dir → `SwarmService.StartSwarm` spawns the conductor (a tmux session named `legato-<parent-id>`) with the conductor role prompt.

2. **Plan.** The conductor explores the codebase, writes a YAML plan to disk, calls `legato swarm propose-plan <file>`. That CLI call broadcasts `plan_proposed` IPC and blocks awaiting a `plan_verdict`. A running TUI surfaces the plan-approval overlay.

3. **Approve / edit / reject.**
   - `y` approves → CLI returns `{"status":"approved","plan_path":"..."}` → conductor proceeds to dispatch.
   - `e` opens `$EDITOR` on the YAML → save → re-validate → `y` approves the edited plan.
   - `n` rejects with notes → notes flow back via send-keys to the conductor pane → conductor revises and re-submits.
   - `esc` dismisses without verdict (the conductor's CLI stays blocked; the user can re-trigger by re-running propose-plan).

4. **Dispatch.** Conductor calls `legato swarm dispatch <subtask-id>` per plan entry. Each worker spawns in its own tmux session (`legato-<subtask-id>`) with role-prompt-file and brief-file written to `<working-dir>/.legato/agents/<task-id>/`.

5. **Workers report.** Workers call `legato swarm progress|question|built` to relay status. Each call delivers a `[swarm event] ...` line to the conductor's pane via `tmux send-keys`. The conductor receives these as new conversational turns.

6. **Conductor supervises.** Reads progress, sends follow-ups (`legato swarm message`), inspects worker output when they signal `built`, then `legato swarm close <subtask-id>` to ratify.

7. **Finish.** When the goal is met, conductor runs `legato swarm finish <parent-id> "<summary>"`. All worker sessions are killed and the summary is appended to the parent task description. **The conductor session is left alive** so the user can still query it for confirmation or follow-up questions. Dismiss the conductor manually via the agents view (`K`) when done.

### Sub-task lifecycle

`queued → dispatched → in_progress → reporting → done` (or `→ cancelled` from any prior state).

- `queued` — exists in the plan, not yet spawned.
- `dispatched` — worker tmux session has been created; status is `dispatched_at = now`.
- `in_progress` — worker has called `swarm progress` for the first time (or first emitted output).
- `reporting` — worker has called `swarm built`; awaiting conductor confirmation.
- `done` — conductor called `swarm close`. `completed_at` set.
- `cancelled` — worker died unexpectedly, OR conductor closed it before completion, OR the swarm finished with the worker still mid-flight.

### File layout per agent

When the agent service spawns a swarm participant, it writes:

```
<working_dir>/.legato/agents/<task-id>/
  role-prompt.md     # the conductor's or worker's role system prompt
  brief.md           # the per-worker initial brief (or conductor's parent-task framing)
```

Env vars on the spawned tmux session:
- `LEGATO_TASK_ID`, `LEGATO_AGENT_ROLE`, `LEGATO_PARENT_TASK_ID`, `LEGATO_SUBTASK_ID`
- `LEGATO_ROLE_PROMPT_FILE`, `LEGATO_BRIEF_FILE`
- `LEGATO_SOCKET` (existing)

The launch command (e.g. `claude --append-system-prompt "$(cat $LEGATO_ROLE_PROMPT_FILE)"`) substitutes the prompt content at shell-expansion time, sidestepping any quoting/escaping concerns. The brief is delivered as a separate kickoff send-keys: `Read your brief at $LEGATO_BRIEF_FILE and begin work.`

Plans are persisted to `<working_dir>/.legato/plans/<parent-id>-<unix-ts>.yaml` and retained as a record.

### IPC: send-keys is the message bus

For inter-agent communication, legato relies on `tmux send-keys`:

- `legato swarm message <subtask-id> "<text>"` → `tmux send-keys -t legato-<subtask-id> "<text>" Enter`. The text appears as the worker's next user turn.
- Worker → conductor: `legato swarm progress` formats a `[swarm event]` line and sends-keys it into the conductor's pane.
- Multi-line or quote-laden payloads are base64-encoded and wrapped: `[swarm event b64:<encoded>]`. Receiving agents are instructed by their role prompt to decode `b64:` envelopes before processing.

Plan approval uses request/reply IPC: the CLI opens a temporary listening socket, sends `plan_proposed` with `reply_socket: <path>`, and blocks until `plan_verdict` arrives on that socket. See `internal/engine/ipc/ipc.go::BroadcastRequest`.

### Packages

- `internal/engine/swarm/` — `MatchScope`/`ScopeOverlaps`/`ValidateScope` (file-ownership glob detection); `Plan`/`PlanSubtask`/`PlanHeader` types + `ParsePlan`/`LoadPlan`/`ValidatePlan`/`Plan.WriteTo`.
- `internal/engine/store/swarm.go` — Subtask CRUD with new columns (`agent_kind`, `prompt`, `dispatched_at`); `SetSubtaskDispatched` helper. Migration `014_swarm_v1.sql` rewrites v0 status enum values and adds the new columns plus `tasks.swarm_working_dir`.
- `internal/engine/hooks/prompts/` — embedded `conductor.md` and `worker.md`. Free-form role labels fall back to `worker.md`. Override per role/adapter via `cfg.swarm.prompts.<role>.<adapter>`.
- `internal/engine/tmux/` — `SendKeysLine` and `SendKeysMultiline` (with base64 wrapping).
- `internal/service/swarm.go` — `SwarmService` with conductor methods (`StartSwarm`, `ApplyApprovedPlan`, `Dispatch`, `Message`, `Broadcast`, `Close`, `Finish`) plus worker methods (`Progress`, `Question`, `Built`) plus `HandleAgentDied` and `StartEventLoop`. Per-worker progress debouncer (1s window) collapses chatty workers; `built`/`question`/`died` events bypass the debounce.
- `internal/service/swarm_messages.go` — formatters for every `[swarm event]` line.
- `internal/service/agent.go` — `LaunchCommandAdapter` interface + per-agent file write + post-spawn auto-launch via `SendKeysLine` + brief kickoff. `KillAgent` publishes `EventAgentDied`. `LastSpawnConflicts()` exposes advisory scope warnings.
- `internal/cli/swarm.go` + `cmd/legato/main.go::runSwarmCmd` — CLI verb handlers.
- `internal/tui/overlay/swarm_init.go` — collects working dir, validates, emits `SwarmStartMsg`.
- `internal/tui/overlay/plan_approval.go` — renders the proposed plan, handles `y/e/n/esc`, sends `plan_verdict` IPC back to the conductor's reply socket.

### CLI surface

Conductor verbs:
```
legato swarm propose-plan <plan-file> [--auto-approve] [--timeout 5m]
legato swarm dispatch <subtask-id>
legato swarm message <subtask-id> "<text>"
legato swarm broadcast <parent-id> "<text>"
legato swarm close <subtask-id>
legato swarm finish <parent-id> "<summary>"
```

Worker verbs:
```
legato swarm progress <subtask-id> "<text>"
legato swarm question <subtask-id> "<text>"
legato swarm built <subtask-id>
```

Read-only:
```
legato swarm status <parent-id>      # JSON snapshot to stdout
```

### Configuration

```yaml
swarm:
  max_concurrent_agents: 4        # cap on live workers per swarm
  max_subtasks_per_plan: 10       # plan size cap
  default_agent: claude-code      # AI tool when plan entry omits `agent`
  strict_scope: false             # when true, scope overlap hard-blocks dispatch
  require_user_close: false       # reserved (no-op currently)
  brief_kickoff_delay_ms: 250     # pause between launch and "read your brief" send-keys
  prompts:                        # per-role per-adapter overrides
    backend:
      claude-code: |
        Custom backend prompt for Claude Code...

adapters:
  claude-code:
    launch_args: []               # extra flags appended to `claude` invocation
  chimera:
    launch_args: ["--sandbox"]    # extra flags appended to `chimera` invocation
    modes:                        # OPTIONAL: per-role mode mapping (you create the mode files)
      conductor: legato-orchestrator
      worker: legato-worker

workspaces:
  - name: rex-app
    color: "#4A9EEF"
    path: /home/me/Projects/rex   # used to pre-fill swarm-init overlay
```

### Configuring per AI tool

The swarm picks an AI tool at three layers, in priority order:

1. **Per-sub-task override** (most specific): the conductor's plan YAML can set `agent:` per sub-task. The conductor decides which tool fits each sub-task.
   ```yaml
   subtasks:
     - title: "Backend API"
       agent: chimera           # this worker uses Chimera
       role: backend
     - title: "Frontend"
       agent: claude-code       # this worker uses Claude Code
       role: frontend
   ```

2. **Swarm-wide default** (`swarm.default_agent`): used when a plan entry omits `agent`, *and* used for the conductor itself. So this is what determines which tool conducts your swarm.
   ```yaml
   swarm:
     default_agent: chimera    # conductor + workers (without explicit agent) use Chimera
   ```

3. **Built-in fallback**: if `swarm.default_agent` is unset, legato uses the first registered adapter (currently Claude Code).

#### Adapter-specific launch flags

Each adapter accepts a `launch_args` list under `adapters.<name>` that gets appended to the auto-launch command. Use this to set sandboxing modes, permission flags, or any other CLI option you want applied uniformly across all swarm participants using that adapter.

**Claude Code** — typical flags:
```yaml
adapters:
  claude-code:
    launch_args:
      - "--dangerously-skip-permissions"   # if you want unattended execution
      # - "--model"
      # - "claude-sonnet-4-6"              # pin a specific model
```

The full launch command becomes:
```
claude --append-system-prompt "$(cat $LEGATO_ROLE_PROMPT_FILE)" --dangerously-skip-permissions
```

**Chimera** — typical flags:
```yaml
adapters:
  chimera:
    launch_args:
      - "--sandbox"
```

The full launch command for a worker becomes:
```
chimera --prompt "$(cat $LEGATO_ROLE_PROMPT_FILE)" --sandbox
```

The `--prompt` flag carries the role content (`worker.md` or `conductor.md`) and Chimera treats it as the agent's first user turn — so chimera self-starts on launch, no separate kickoff send-keys needed. (Claude Code, by contrast, uses `--append-system-prompt` which is *system context*; legato delivers the kickoff send-keys separately for claude.)

##### Per-role Chimera modes (optional, opt-in)

Chimera supports user-defined modes — markdown files at `~/.chimera/modes/<name>.md` that shape Chimera's runtime behavior (tool restrictions, persona shaping, color, etc.). Modes are *additive*: they layer on top of `--prompt`. The prompt still tells the agent what to do (the role content); the mode just lets you tweak chimera's runtime stance.

Legato can inject `--mode <name>` per swarm role if you opt in. **Legato does NOT ship default mode files** — you create the mode files yourself, then map roles to them in config:

```yaml
adapters:
  chimera:
    launch_args: ["--sandbox"]
    modes:
      conductor: legato-orchestrator     # name of a file at ~/.chimera/modes/legato-orchestrator.md
      worker: legato-worker              # name of a file at ~/.chimera/modes/legato-worker.md
      backend: my-backend-mode           # role-specific override (optional)
```

With the above, the launch becomes:
```
chimera --prompt "$(cat $LEGATO_ROLE_PROMPT_FILE)" --mode legato-worker --sandbox
```

Resolution rules:
- Exact role match wins (`backend: my-backend-mode` for a `backend` worker).
- Non-conductor roles with no exact match fall back to `worker`.
- No match found → no `--mode` flag is passed; chimera uses its own default mode.
- If `modes` is unset entirely → no `--mode` flag is passed (same as above).
- If `--mode` already appears in `launch_args` → user override wins; auto-injection skipped.

To sketch the legato mode files yourself, look at the existing chimera modes at `~/.chimera/modes/*.md` for the YAML frontmatter format. The mode body should describe *how* the agent should behave (tools available, persona, constraints) — not duplicate the role content from `worker.md`/`conductor.md`, which still arrives via `--prompt`.

Args are shell-quoted automatically — use them as you would on the CLI; no escaping required for spaces or special characters in individual args.

#### Per-role prompts (any adapter)

If you want to customize what role looks like when running on a specific adapter, override the system prompt under `swarm.prompts.<role>.<adapter>`:

```yaml
swarm:
  prompts:
    conductor:
      chimera: |
        You are the swarm conductor. (Chimera-specific guidance...)
    backend:
      claude-code: |
        You are a backend specialist for Claude Code. (Tone and emphases that
        play well with Claude Code's behavior...)
```

When unset, the embedded `conductor.md` / `worker.md` defaults apply.

#### Mixing tools in one swarm

Set a default and override per sub-task. This is useful if some sub-tasks benefit from Chimera's sandbox while others need Claude Code's tool ecosystem:

```yaml
# config.yaml
swarm:
  default_agent: claude-code   # conductor uses Claude Code
adapters:
  claude-code:
    launch_args: ["--dangerously-skip-permissions"]
  chimera:
    launch_args: ["--sandbox"]
    # If you've created mode files at ~/.chimera/modes/legato-{orchestrator,worker}.md,
    # opt them in here. Skip this block to let Chimera use its own default mode.
    modes:
      conductor: legato-orchestrator
      worker: legato-worker
```

Then in the conductor's plan:
```yaml
subtasks:
  - title: "Risky migration script"
    agent: chimera           # explicitly use Chimera (sandboxed)
    role: migrations
  - title: "API refactor"
    # no agent: → falls back to default (claude-code)
    role: backend
```

### Web UI parity

The web PWA (`docs/claude/web-ui.md`) has full parity for the user-driven swarm verbs: starting a swarm from the agents view, approving/rejecting/dismissing plan proposals via a modal, messaging individual workers, closing workers, and finishing a swarm. Plan proposals arrive over WebSocket (`plan_proposed`) and verdicts travel back on the same socket (`plan_verdict`). A per-parent event log shows unacked swarm events with an explicit drain action. See `docs/claude/web-ui.md` § Swarm controls for details.

### Risks / known limitations

- **Send-keys is best-effort.** If a message arrives mid-turn, tmux queues at the prompt; the agent processes it next. Latency is bounded by turn duration. No exactly-once guarantees.
- **Recursive sub-tasks not supported in v1.** Workers cannot spawn sub-workers; only the conductor delegates.
- **Plan dismissal blocks the conductor.** If the user `esc`s without rendering a verdict, the conductor's `propose-plan` call stays blocked. The user can either re-trigger by running propose-plan from the conductor pane, or rely on the optional `--timeout` flag.
- **Working dir is per-swarm.** No fallback to current directory; explicit input required at swarm-init.
