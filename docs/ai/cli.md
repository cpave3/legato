## CLI

## Agent execution identity

Legato creates a durable execution before spawning every hook-bearing agent and injects `LEGATO_EXECUTION_ID` beside `LEGATO_TASK_ID`. Generated Claude Code, Codex, and Chimera hooks call `legato agent state <task-id> --execution-id <id> --activity <working|waiting|error|"">`. New hooks always provide the execution ID. For compatibility with already-installed legacy hooks, an omitted execution ID updates only the live `agent_sessions.activity` status and notifications; it never infers an execution or writes reporting activity. Provided unknown, terminal, or mismatched execution IDs are rejected. Provider IDs such as `CHIMERA_SESSION_ID` remain validation/readiness metadata, and child Chimera hooks do not mutate parent execution activity.

Terminal outcomes are explicit: user/workflow cancellation → `cancelled`, launch/runtime failure → `failed`, missing session reconciliation → `lost`, and successful workflow callback → `succeeded`.

## Workflows

`legato workflow` manages reusable hybrid workflows:

```bash
legato workflow list --json
legato workflow validate <name-or-file> --json
legato workflow run <name> --input key=value --json
legato workflow runs [--status running] [--limit 20] --json
legato workflow show <run-id> --json
legato workflow trigger <run-id> <step-id> --json
legato workflow retry <run-id> <step-id> --json
legato workflow cancel <run-id> --json
```

Each `agent.run` attempt owns a distinct durable execution; retries create new executions. Task-triggered runs attribute active time to `workflow_runs.related_task_id` while retaining the synthetic workflow task and run/attempt IDs for drill-down. Repository-only runs remain project/workflow activity. Successful callbacks terminalize the execution before deleting the operational agent session.

Agent callbacks use `LEGATO_WORKFLOW_RUN_ID`, `LEGATO_WORKFLOW_STEP_ID`, and `LEGATO_WORKFLOW_ATTEMPT_ID` automatically:

```bash
legato workflow step complete --output result=value --artifact review=review.md --json
legato workflow step fail "reason" --json
```

See `docs/workflows.md` for the schema and lifecycle. Workflow commands currently emit raw JSON for successful structured output; errors use the standard CLI error rendering.

## Legato Learn

`legato learn` records, retrieves, curates, decides, incorporates, verifies, and checks drift for evidence-backed durable learnings. Commands include `record`, `show`, `list`, `search`, `context`, `curate`, `consolidate`, `accept`, `edit`, `reject`, `incorporate`, `incorporation submit`, `verify`, `drift`, and the workflow callback `reflection submit`. Mutation commands use optimistic `--revision`; reflection and incorporation documents can be read from `--file` or stdin. Successful Learn commands emit raw JSON. See [Legato Learn](learning.md) for exact flags, schemas, privacy boundaries, and lifecycle semantics.

## Review handoff

`legato review handoff template` prints the editable nested contract bound to the current ready review. `legato review handoff submit --file path|-` authors a bounded, strict JSON packet for an existing clean ready review. `show [--json]` reads persisted history; `export --packet ID --format github|maintenance` previews selected Markdown without publishing. Selectors use `--task`/`--name` or the corresponding review environment variables. Human notes are not writable through this authoring CLI. See [Review handoff](../handoff.md) for the full runnable schema example, evidence limits, revision conflicts, API, and privacy rules. Handoff is optional and does not change review readiness or approval.

## Subcommands

`legato` binary supports subcommand dispatch alongside the default TUI mode:

- `legato` (no args) — launches TUI (existing behavior)
- `legato --help`, `legato <command> --help`, and deeper registered paths such as `legato workflow step complete --help` — print context-specific CLI help with realistic examples to stdout and exit successfully without loading configuration or the database. Every registered command level includes at least one example; parent help shows up to three examples from each immediate child, with a count and path to child help when more forms are available. `-h` and a nested trailing `help` are aliases.
- `legato help` / `legato learn` — print a Markdown primer designed for AI-agent discovery, including task workflows, Jira edit restrictions, agent-session environment, and links to command-family help.
- `legato task list [--status <status>] [--workspace <name|unassigned>] [--limit <count>] [--json]` — list active tasks in board-column and card order. Text output is tab-separated; JSON returns concise task objects.
- `legato task show [<task-id>] [--format description|full|json] [--json]` — print task context to stdout for agents/scripts. Full Markdown and JSON include durable task notes, including workflow triage reports.
- `legato task notes [<task-id>] [--json]` — retrieve durable task notes as feedable Markdown or structured JSON. Defaults to `LEGATO_TASK_ID` when omitted.
- `legato task create <title> [--description <text>|--description-file <path|->] [--status <status>] [--priority <priority>] [--workspace <name>] [--json]` — create a local task. `--description-file -` reads multiline Markdown from stdin without trimming it.
- `legato task update [<task-id>] [--status <status>] [--title <title>] [--description <text>|--description-file <path|->] [--workspace <name>] [--json]` — atomically update requested fields. Use workspace `none` or `unassigned` to clear an assignment. Titles remain read-only for Jira-backed tasks; description updates replace their Legato-owned Markdown details without changing Jira.
- `legato task description [<task-id>] [--description <text>|--description-file <path|->]` — replace a task's Legato-owned description (convenience alias for `task update --description`); provider content is unchanged
- `legato task note <task-id> <message>` — legacy convenience command that appends timestamped text to a local task description; Jira-backed tasks are rejected. Durable workflow notes are separate records written by the `task.note` workflow executor and read with `task notes` or full/JSON `task show`.
- `legato workspace list [--format text|json]` — list configured workspaces. Text output prints exact names suitable for `--workspace`; JSON includes IDs, names, and colors.
- `legato agent state <task-id> --activity <working|waiting|background|error|"">` — update agent activity state on a card
- `legato agent stopped [<task-id>] [--execution-id <id>] [--working-dir <path>]` — integration command the Claude Code `Stop` hook pipes its JSON payload into. It reports `background` when the payload's `background_tasks` lists work still in flight (a shell, a subagent) that will wake the session, and clears the activity otherwise. A payload it cannot read also clears it, so the hook never fails a turn.
- `legato agent peers [--task <task-id>] [--execution-id <id>] [--directory <path>] [--json]` — list other live Legato agents launched anywhere inside the same Git worktree. Task and execution default to `LEGATO_TASK_ID` and `LEGATO_EXECUTION_ID`; an explicit directory can be used outside an agent session. Separate Git worktrees remain isolated.
- `legato agent message <task-or-execution-id> <text> [--json]` — queue a best-effort direct message to a running agent. Task IDs are stable routing identities; `ex-…` execution IDs identify one specific launch and are also accepted while that execution is live. Chimera appends the message so a busy turn receives it at the next feasible boundary; tmux delivery remains immediate send-keys.
- `legato agent broadcast <text> [--task <task-id>] [--execution-id <id>] [--directory <path>] [--json]` — send a non-urgent advisory message to every other live agent in the same Git worktree. JSON reports `delivered` task IDs and per-target `failed` details; one stale peer does not undo successful deliveries. Direct and broadcast messages open with a `[legato peer <sender-task-id>]` tag (`[legato peer]` when sent from outside an agent session) and end with an agent-to-agent footer naming the sender task ID and showing the reply command. The join notice existing peers receive is Legato's own `[legato coordination]` message and carries neither.
- `legato agent summary [--exclude <task-id>]` — output tmux-formatted agent session counts (working/waiting/error/idle) for use in tmux status bar `#()` expansion
- `legato agent status <task-id> --format tmux` — output a swarm-aware tmux status-line string for the given task. For swarm participants it shows `x/y done`, the last event kind + age, active sibling count, and a scope-warning icon; for solo agents it falls back to the same output as `agent summary --exclude <task-id>`. Auto-injected into `status-right` by `SpawnAgent` for swarm sessions; solo sessions keep the summary command. The CLI opens a new SQLite connection each call (the in-process `SwarmService.LatestSnapshot` cache is unreachable across process boundaries) so latency is bounded by SQLite open + two aggregate queries
- `legato agent bridge [<task-id>] [--execution-id <id>]` — long-running integration command spawned by the Legato mod inside a Claude Code session, not run by hand. It serves the session to Legato in the native agent protocol, binds itself to the task's running agent session, and exits with the Claude Code process. Its stdout carries commands for the mod only; diagnostics go to stderr. `LEGATO_CLAUDE_BRIDGE_TRACE=<file>` records the traffic. See [Claude Code native integration](claude-code-native.md).
- `legato agent codex -- <codex arguments>` — what Legato types into a pane to start a Codex session it can drive natively, in place of `codex <arguments>`. It starts the session's bridge, waits for it, then replaces itself with `codex --remote …` attached to the bridge's app server, so the pane's process is `codex`. When Codex cannot share an app server or the bridge does not come up, it says why on stderr and starts plain `codex <arguments>`. See [Codex native integration](codex-native.md).
- `legato agent codex-bridge [<task-id>] [--execution-id <id>] [-- <app-server arguments>]` — long-running integration command started by `legato agent codex`, not run by hand. It runs a `codex app-server` for the session, serves the session to Legato in the native agent protocol, binds itself to the task's running agent session, prints one `{"type":"ready","url","token"}` line on stdout for the launcher, and exits with the Codex process. Diagnostics go to stderr (`<runtime dir>/codex-bridge/<task-id>.log` when started by the launcher).
- `legato task link <task-id> [--branch <branch>] [--repo <owner/repo>] [--sha <commit-sha>]` — compatibility command for hooks/scripts; records durable task code context and resets cached PR data when the branch changes.
- `legato task github context <task-id> --repo <owner/repo> [--branch <branch>] [--sha <commit>] [--base-branch <branch>]` — set manual repository context with highest precedence.
- `legato task github refresh <task-id>` — refetch the currently linked PR without reconsidering its association.
- `legato task github rediscover <task-id>` — retain repository context, clear cached/pinned PR identity, and immediately rerun conservative discovery.
- `legato task unlink <task-id>` — clear both the PR cache and repository context.
- `legato hooks install --tool <tool[,tool...]>` — install hooks for one or more explicitly selected AI tools; for example `--tool claude-code,codex,chimera` (claude-code: `.claude/hooks/`, staccato: `~/.config/staccato/hooks/`, chimera: `~/.chimera/hooks/`, codex: `.codex/hooks/`, yggdrasil: `.yggdrasil.toml`)
- `legato hooks uninstall --tool <tool[,tool...]>` — remove hooks for one or more explicitly selected tools
- `legato auth token` — print the web UI auth token to stdout
- `legato auth regenerate` — generate a new auth token (invalidates all paired devices)
- `legato pair [--port <port>]` — render a QR code in the terminal encoding `legato://pair?url=<serverUrl>&token=<token>` for one-step PWA pairing. Prints raw token below QR as fallback. Uses configured hostname or system hostname, auto-detects TLS scheme

### Shared command contract

Human-readable text remains the default. Task, workspace, agent, hooks, auth,
and pairing commands accept `--json`; it writes one `{ok, command, data}` value
on success or `{ok, command, error: {code, message, details?}}` on failure.
Global `legato --json <command> ...` is an alias. JSON diagnostics never share
stdout with the result.

Peer messages are coordination hints, not file locks or durable claims. Managed agents receive a startup notice when peers share their worktree and should announce touched files, for example `legato agent broadcast "Working on internal/service/agent.go; message task-123 before editing it."` Set `agents.peer_awareness: false` in config to suppress automatic peer context and join notices. The explicit `peers`, `message`, and `broadcast` commands remain available.

Task-scoped commands resolve identity in this order: explicit task ID, then
`LEGATO_TASK_ID`. This applies to task show/notes/update/description/note/link/unlink,
task github context/refresh/rediscover, task worktree set/clear, and agent state/status. Task creation never consumes
the environment identity.

Stable exit categories are: `0` success, `2` usage or invalid input, `3`
not found, `4` provider/state policy rejection, `5` configuration or missing
environment, `6` external dependency failure, and `1` unexpected internal
failure. Unknown and duplicate flags are rejected before configuration or the
database is loaded.

### Collaborative plan verbs

- `legato plan validate <markdown-file>` — validate Markdown plus `mermaid` and canonical `wireframe` fences without task or database context; prints `Plan is valid.` on success, exits `2` for content errors, and installs pinned Node parsers into the user cache when absent
- `legato plan submit <bundle-dir> [--task <id>] [--name <name>]` — validate `plan.md`/`plan.json`, including plan visuals, snapshot them, and propose a new immutable revision
- `legato plan show|feedback --json [--task <id>] [--name <name>]` — print the current revision, choices, responses, comments, and Q&A transcript
- `legato plan status --json [--task <id>] [--name <name>]` — print lifecycle status and revision
- `legato plan answer <thread-id> "<markdown>" [--task <id>] [--name <name>]` — answer immediate plan Q&A
- `legato plan claim --task <plan-task-id> [--name <name>]` — hand an existing plan to the current agent session; unacknowledged interactions follow the new claimant
- `legato plan inbox [--json]` — print and acknowledge durable plan/review interactions assigned to the current `LEGATO_TASK_ID`
- `legato plan complete [--task <id>] [--name <name>]` — mark successful implementation complete and safely remove the submitted source bundle when approval requested cleanup
- `legato plan withdraw [--task <id>] [--name <name>]` — remove a non-approved proposal

See `docs/plans.md` for the bundle schema and approval lifecycle.

Review tours use the same handoff model: `legato review claim --task
<review-task-id> [--name <name>]` assigns an existing tour to the current agent,
and `legato review inbox [--json]` drains the shared interaction queue. Initial
plan submission records the current agent separately from an explicit `--task`,
so an ephemeral implementation task can receive feedback for a plan attached to
another board task.

Submit review answers from a file or stdin. Positional answer text is not
supported because a shell can expand Markdown before Legato receives it. Use a
quoted heredoc to preserve backticks, dollar signs, quotes, and fenced code:

```sh
legato review answer <step-id> --file - <<'LEGATO_ANSWER'
Markdown with `inline code`, $VARIABLE, and $(command).
LEGATO_ANSWER
```

`--file <path>` reads the same Markdown from a named file. The answer must be
non-blank UTF-8 text. A retry replaces the answer after the latest question on
the step. A later question starts a new transcript turn.

### Swarm verbs

Conductor-only:
- `legato swarm validate-plan <plan-file>` — dry-run validation, prints JSON `{valid:bool, error?:string}`; exits 2 on invalid but no DB writes
- `legato swarm propose-plan <plan-file> [--auto-approve] [--timeout 5m]` — submit a YAML plan for HITL approval. Blocks via IPC `BroadcastRequest` until a TUI replies with `plan_verdict` (approved / rejected / edited), or returns immediately when `--auto-approve` is set
- `legato swarm extend-plan <plan-file> [--auto-approve] [--timeout 5m]` — append a validated plan to an existing swarm. Inherits the swarm's working directory so `working_dir` can be omitted. New sub-tasks receive step indices after the current max. Uses `plan_extension_proposed` IPC instead of `plan_proposed`
- `legato swarm cancel <parent-id>` — terminate a swarm from any state: kills conductor + workers, deletes sub-tasks, clears working dir, removes runtime files and pending plans
- `legato swarm dispatch <subtask-id>` — spawn the worker for a queued sub-task; transitions `queued → dispatched`
- `legato swarm message <subtask-id> "<text>"` — send-keys text into a worker's tmux pane
- `legato swarm broadcast <parent-id> "<text>"` — send the same text to every live worker in the swarm
- `legato swarm close <subtask-id>` — ratify a sub-task; transitions `reporting → done` (or `* → cancelled` from earlier states)
- `legato swarm finish <parent-id> "<summary>"` — kill all worker sessions and append the summary to the parent task description; the conductor session stays alive

Worker-only:
- `legato swarm progress <subtask-id> "<text>"` — emit a debounced progress event into the conductor's pane
- `legato swarm question <subtask-id> "<text>"` — emit an immediate (non-debounced) question to the conductor
- `legato swarm built <subtask-id>` — signal completion; transitions `in_progress → reporting` and bypasses progress debounce

Read-only:
- `legato swarm status <parent-id>` — print a JSON snapshot of the swarm's coordination surface to stdout
- `legato swarm inbox <parent-id>` — drain unacked swarm events from the inbox (FIFO) and print as JSON; events are atomically marked acked

CLI subcommands load only the dependencies they need. Read-only task fetches load config+store+board service; mutating commands also use IPC broadcasts. They do not initialize the TUI, tmux, or sync service.

## AI Tool Integration (Claude Code)

Abstract adapter interface (`service.AIToolAdapter`) for pluggable AI tool integrations. Claude Code is the first implementation. **Hooks do NOT perform status/column transitions** — they only update visual activity state on cards.

**Flow**: Legato spawns tmux session → injects `LEGATO_TASK_ID` env var via `tmux new-session -e` → Claude Code hooks fire on lifecycle events → hook scripts check `LEGATO_TASK_ID` → call `legato agent state` CLI → CLI updates `agent_sessions.activity` in SQLite + broadcasts IPC to all running instances → TUI event bus publishes `EventCardUpdated` → board refreshes card indicators

**Agent activity states** (stored in `agent_sessions.activity` column):
- `"working"` — Claude is actively processing (triggered by the `UserPromptSubmit` and `PostToolUse` hooks)
- `"waiting"` — Claude needs the person (triggered by the `PermissionRequest` hook)
- `"background"` — Claude's turn ended while a shell or subagent it started is still running; the session wakes by itself when that finishes (triggered by the `Stop` hook when its payload lists `background_tasks`). It is a live status only: no reporting span is recorded, no notification is sent and it does not raise Attention, because the agent has neither finished nor asked for anyone.
- `""` — idle / cleared (triggered by the `Stop` hook with nothing in flight, and by `SessionEnd`)

**Card indicators**: `AgentState` field on `CardData` drives three visual states on a dedicated agent line: green spinning icon + "RUNNING" with cumulative duration (working), blue diamond + "WAITING" with duration (waiting), dim terminal icon + "IDLE" (agent alive but no activity). A fourth, cyan terminal icon + "BACKGROUND", marks an agent whose turn ended while a shell or subagent it started still runs; the agents view shows the same label. Cards with no active agent but duration history show "Xh Ym working · Zm waiting". Rendered in `board/card.go` via `renderAgentLine()`, populated via `board.SetAgentStates()` + `board.SetDurations()` from app.go data loading.

**Hook events mapped**: `UserPromptSubmit` and `PostToolUse` → working, `PermissionRequest` → waiting, `Stop` → background or clear (from its payload, through `legato agent stopped`), `SessionEnd` → clear. A background shell finishing reaches the session as a prompt, so `UserPromptSubmit` returns it to working. Installations made before the `Stop` hook read its payload keep clearing on every stop until `legato hooks install --tool claude-code` is run again. Scripts generated by `legato hooks install`, written to `.claude/hooks/legato-*.sh` (prompt-submit, stop, session-end), registered in `.claude/settings.json`.

**IPC**: Each TUI instance creates a PID-based Unix domain socket at `$XDG_RUNTIME_DIR/legato/legato-<pid>.sock` (fallback `/tmp/legato-<uid>/legato-<pid>.sock`). Multiple instances coexist — CLI commands `Broadcast()` to all `*.sock` files in the directory. Protocol: newline-delimited JSON. Best-effort — CLI silently skips unreachable sockets. Message types: `task_update`, `task_note`, `agent_state`, `agents_changed` (a Claude Code bridge bound or unbound its agent's native endpoint), `pr_linked`, `plan_proposed`, `plan_verdict`, `swarm_changed`, `review_changed`. Review changes carry explicit `tour_id`, `step_id`, and `kind` fields so CLI-authored answers can refresh the matching web review in real time.

**Native bridge**: Legato-launched Claude Code sessions also load the Legato mod (unless `adapters.claude-code.native_bridge: false`), which delivers messages, interrupts and permission decisions directly instead of through send-keys and feeds the web native panel. The shell hooks above still own activity state. See [Claude Code native integration](claude-code-native.md).

**Adapter registration**: `AdapterRegistry` in service layer. Claude Code adapter in `internal/engine/hooks/claude_code.go`. `AgentServiceOptions` struct passes adapter, socket path, and `TmuxOptions` to `NewAgentService` for env var injection and session configuration on spawn.

**Migration**: `006_agent_activity.sql` adds `activity TEXT NOT NULL DEFAULT ''` column to `agent_sessions` table.

**State duration tracking**: `state_intervals` table records timestamped working/waiting intervals per task. `cli.AgentState()` calls both `store.UpdateAgentActivity()` and `store.RecordStateTransition()`. `ReconcileSessions()` closes orphaned intervals for dead agents. Durations computed at query time via SQL aggregation (including open intervals using `datetime('now')`). `Store.DB()` exposes underlying `*sqlx.DB` for advanced queries in tests.

**Duration formatting**: `board.formatDuration(d)` returns `""` (zero), `"<1m"` (under 60s), `"Xm"` (under 1h), `"Xh Ym"` (1h+). `CardData.WorkingDuration`/`WaitingDuration` populated during `DataLoadedMsg` via `AgentService.GetTaskDurations()` batch query.

## Tmux Status Line

Legato-spawned tmux sessions get a custom status bar showing live context. Solo agents see a summary of other sessions; swarm participants see swarm-local progress. Implemented via tmux `#()` shell expansion.

- **Injection**: `SpawnAgent` sets `status-right`, `status-interval` (5s), `status-style`, `status-left` on the session *before* user `tmux_options` — user config can override
- **Binary path**: Resolved once at startup via `os.Executable()`, passed to `AgentServiceOptions.BinaryPath`, embedded as absolute path in the `status-right` command
- **Solo vs swarm**: when `AgentSpawnOptions.ParentTaskID` is empty, `status-right` invokes `legato agent summary --exclude <taskID>` (working/waiting/error/idle counts for *other* sessions). When `ParentTaskID` is set, `status-right` invokes `legato agent status <taskID> --format tmux`, which prints swarm-local context (`x/y done`, last event + age, active sibling count, scope-warning icon).
- **Output format**: `legato agent summary` outputs tmux-native style markup (`#[fg=green]2 working #[fg=colour240]· #[fg=yellow]1 waiting #[fg=colour240]· #[fg=colour245]0 idle`). Zero-count working/waiting states omitted; idle always shown
- **Performance**: Opens SQLite, runs single `GROUP BY` aggregate query, exits. Sub-10ms typical execution

## Staccato Integration

`StaccatoAdapter` in `internal/engine/hooks/staccato.go` implements `AIToolAdapter`. Installs a `post-pr-create` hook at `~/.config/staccato/hooks/post-pr-create/legato-pr-link.sh` (staccato uses directory-based hooks, not config files).

**Flow**: Staccato `post-pr-create` fires when browser opens to PR creation page (PR may not exist yet) → hook reads `LEGATO_TASK_ID` (injected by legato's tmux session) + `ST_REPO_PATH` + `ST_BRANCH` → detects owner/repo from git remote + captures head commit SHA via `git rev-parse HEAD` → calls `legato task link $LEGATO_TASK_ID --branch $ST_BRANCH --repo owner/repo --sha $SHA` → IPC broadcast triggers immediate poll → background polling discovers PR when it actually exists.

**Key detail**: the hook is optional. `task link` writes durable `CodeContext` (repository/branch/SHA), and authenticated background synchronization discovers the PR by exact SHA, exact repository+branch, or a unique ticket-key branch match. Worktree and agent launch paths capture the same context without Staccato.

## Chimera Integration

`ChimeraAdapter` in `internal/engine/hooks/chimera.go` implements `AIToolAdapter`. It installs ten managed hooks under `~/.chimera/hooks/<EventName>/legato-*.sh` (Chimera uses directory-based hooks, not a settings file — drop a script and it runs).

**Event → activity mapping** (per Chimera's documented integration recipe):

| Event                | Script                                  | Activity  |
|----------------------|-----------------------------------------|-----------|
| `SessionCreated`     | `legato-session-created.sh`             | capture   |
| `UserPromptSubmit`   | `legato-prompt-submit.sh`               | `working` |
| `PermissionRequest`  | `legato-permission.sh`                  | `waiting` |
| `PermissionResolved` | `legato-permission-resolved.sh`         | payload-dependent |
| `UserInputRequired`  | `legato-user-input-required.sh`         | `waiting` |
| `Stop`               | `legato-stop.sh`                        | (clear)   |
| `OnError`            | `legato-on-error.sh`                    | `error`   |
| `SessionEnd`         | `legato-session-end.sh`                 | (clear)   |
| `Interrupt`          | `legato-interrupt.sh`                   | (clear)   |
| `Timeout`            | `legato-timeout.sh`                     | (clear)   |

**Flow**: `SessionCreated` pipes Chimera's JSON payload into `legato agent session-created`. The CLI stores the session ID only when `session_name` matches `LEGATO_TASK_ID`, which identifies the durable task parent created with `--session-name`; unnamed or differently named `spawn_agent` children are valid no-ops and cannot replace the task's resume ID. Malformed payloads and session-ID mismatches fail without mutating the stored binding. The direct `legato agent session-created <task-id> <session-id>` form remains available for administrative repair. Existing installations must run `legato hooks install --tool chimera` to receive this payload-aware script.

Each activity script gates on `LEGATO_TASK_ID`, `LEGATO_EXECUTION_ID`, and `CHIMERA_SESSION_ID`; outside a Legato-managed execution it exits without reporting activity. Inside one, it calls `legato agent state` with the task, execution, working directory, Chimera session, activity, and event. Scripts propagate that command's exit status so Chimera can warn on failed state publication. The command updates `agent_sessions.activity`, records execution activity, and broadcasts IPC through the shared state path.

`PermissionResolved` pipes Chimera's JSON payload into `legato agent permission-resolved`. The CLI validates the atomic `session_status`/`remaining_pending` pair and maps another outstanding permission to `waiting`, or the final resolution back to `working`. Resolution never clears to idle because allow and deny both resume the turn; terminal hooks own idle/error. `PostToolUse` is intentionally not a managed activity hook because a parallel tool can finish while another permission remains outstanding. Installing updated hooks removes only Legato's obsolete `legato-post-tool-use.sh` script.

**Task context access**: Chimera role prompts include Legato-specific guidance that `legato task show $LEGATO_TASK_ID` fetches the current task description/context, and `--format full` includes structured metadata. In sandboxed Chimera sessions, run that command in host mode, the same as other Legato CLI calls.

**Coexistence with Claude Code**: Both adapters can be installed simultaneously. Hooks fire from different processes inside the same tmux session. `ChimeraAdapter.EnvVars` injects `LEGATO_TASK_ID`; the agent service supplies execution identity and Chimera supplies `CHIMERA_SESSION_ID` when invoking hooks. Install or refresh the managed scripts with `legato hooks install --tool chimera`.

## Codex Integration

`CodexAdapter` in `internal/engine/hooks/codex.go` implements `AIToolAdapter`. Installs four activity-update hooks via `.codex/hooks.json` and writes shell scripts to `.codex/hooks/legato-*.sh`. The scope is determined by the current working directory: run `legato hooks install --tool codex` from your home directory for global hooks (`~/.codex/`) or from a project directory for project-local hooks (`<repo>/.codex/`).

For swarm auto-launch, Legato injects the conductor/worker role prompt as Codex developer instructions with `codex -c developer_instructions="$(cat $LEGATO_ROLE_PROMPT_FILE)"`, then sends the usual kickoff message telling Codex to read `$LEGATO_BRIEF_FILE`.

**Native bridge**: unless `adapters.codex.native_bridge: false`, that command is typed as `legato agent codex -- -c developer_instructions=… <launch_args>`: the session runs against an app server of its own, with a Legato bridge as a second client of it. Messages, interrupts and approvals then go to Codex directly instead of through send-keys, the kickoff waits for the bridge instead of a fixed delay, and the web UI shows the session in the native panel. The shell hooks below still own activity state; they run inside the app server, which inherits the pane's `LEGATO_TASK_ID`. See [Codex native integration](codex-native.md).

**Event → activity mapping** (per Codex's documented hook events):

| Event               | Script                       | Activity  |
|---------------------|------------------------------|-----------|
| `UserPromptSubmit`  | `legato-prompt-submit.sh`    | `working` |
| `PostToolUse`       | `legato-post-tool-use.sh`    | `working` |
| `PermissionRequest` | `legato-permission-request.sh` | `waiting` |
| `Stop`              | `legato-stop.sh`             | (clear)   |

**Flow**: Codex sends each hook's JSON payload on `stdin`. Each Legato hook script drains that payload before gating on `LEGATO_TASK_ID` (injected by legato's tmux session) — outside a Legato-spawned session it's a no-op. Inside, it calls `legato agent state $LEGATO_TASK_ID --activity <state>`, which updates `agent_sessions.activity` and broadcasts IPC like Claude Code's hooks. Legato does not emit a hook decision or other JSON output.

**Hook trust**: Codex requires users to review and trust non-managed hooks before they run. After installing, open the Codex CLI and run `/hooks` to inspect, review, and trust the Legato hook entries. Alternatively, pass `--dangerously-bypass-hook-trust` for one-off automation.

**Install**: `legato hooks install --tool codex`
**Uninstall**: `legato hooks uninstall --tool codex` — removes only Legato entries from `hooks.json` and deletes the scripts. Other user hooks in `hooks.json` are preserved.

**Coexistence**: Codex, Claude Code, Chimera, and Staccato adapters can all be installed simultaneously. Each adapter's hooks fire independently; only `LEGATO_TASK_ID` (already injected by the agent service) needs to flow through.
