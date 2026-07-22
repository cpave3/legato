## CLI Subcommands

`legato` binary supports subcommand dispatch alongside the default TUI mode:

- `legato` (no args) — launches TUI (existing behavior)
- `legato task show <task-id> [--format description|full|json]` — print task context to stdout for agents/scripts. Defaults to the description-only markdown format used by detail-view copy; `full` includes structured metadata; `json` returns a machine-readable task detail object.
- `legato task update <task-id> --status <status>` — move task to column (case-insensitive status matching)
- `legato task note <task-id> <message>` — append timestamped note to task description
- `legato agent state <task-id> --activity <working|waiting|"">` — update agent activity state on a card
- `legato agent summary [--exclude <task-id>]` — output tmux-formatted agent session counts (working/waiting/idle) for use in tmux status bar `#()` expansion
- `legato agent status <task-id> --format tmux` — output a swarm-aware tmux status-line string for the given task. For swarm participants it shows `x/y done`, the last event kind + age, active sibling count, and a scope-warning icon; for solo agents it falls back to the same output as `agent summary --exclude <task-id>`. Auto-injected into `status-right` by `SpawnAgent` for swarm sessions; solo sessions keep the summary command. The CLI opens a new SQLite connection each call (the in-process `SwarmService.LatestSnapshot` cache is unreachable across process boundaries) so latency is bounded by SQLite open + two aggregate queries
- `legato task link <task-id> [--branch <branch>] [--repo <owner/repo>] [--sha <commit-sha>]` — link a git branch to a task for PR tracking (auto-detects branch if `--branch` omitted, `--repo` enables repo-scoped polling, `--sha` anchors PR discovery to the exact head commit)
- `legato task unlink <task-id>` — remove branch/PR association from a task
- `legato hooks install [--tool claude-code|staccato|chimera|codex]` — install AI tool hooks (claude-code: `.claude/hooks/`, staccato: `~/.config/staccato/hooks/`, chimera: `~/.chimera/hooks/`, codex: `.codex/hooks/`)
- `legato hooks uninstall [--tool claude-code|staccato|chimera|codex]` — remove installed hooks
- `legato auth token` — print the web UI auth token to stdout
- `legato auth regenerate` — generate a new auth token (invalidates all paired devices)
- `legato pair [--port <port>]` — render a QR code in the terminal encoding `legato://pair?url=<serverUrl>&token=<token>` for one-step PWA pairing. Prints raw token below QR as fallback. Uses configured hostname or system hostname, auto-detects TLS scheme

### Collaborative plan verbs

- `legato plan submit <bundle-dir> [--task <id>] [--name <name>]` — validate `plan.md`/`plan.json`, snapshot them, and propose a new immutable revision
- `legato plan show|feedback --json [--task <id>] [--name <name>]` — print the current revision, choices, responses, comments, and Q&A transcript
- `legato plan status --json [--task <id>] [--name <name>]` — print lifecycle status and revision
- `legato plan answer <thread-id> "<markdown>" [--task <id>] [--name <name>]` — answer immediate plan Q&A
- `legato plan withdraw [--task <id>] [--name <name>]` — remove a non-approved proposal

See `docs/plans.md` for the bundle schema and approval lifecycle.

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
- `"working"` — Claude is actively processing (triggered by `UserPromptSubmit` hook)
- `"waiting"` — Claude finished, waiting for user input (triggered by `Stop` hook)
- `""` — no activity / cleared (triggered by `SessionEnd` hook)

**Card indicators**: `AgentState` field on `CardData` drives three visual states on a dedicated agent line: green spinning icon + "RUNNING" with cumulative duration (working), blue diamond + "WAITING" with duration (waiting), dim terminal icon + "IDLE" (agent alive but no activity). Cards with no active agent but duration history show "Xh Ym working · Zm waiting". Rendered in `board/card.go` via `renderAgentLine()`, populated via `board.SetAgentStates()` + `board.SetDurations()` from app.go data loading.

**Hook events mapped**: `UserPromptSubmit` → working, `Stop` → waiting, `SessionEnd` → clear. Scripts generated by `legato hooks install`, written to `.claude/hooks/legato-*.sh` (prompt-submit, stop, session-end), registered in `.claude/settings.json`.

**IPC**: Each TUI instance creates a PID-based Unix domain socket at `$XDG_RUNTIME_DIR/legato/legato-<pid>.sock` (fallback `/tmp/legato-<uid>/legato-<pid>.sock`). Multiple instances coexist — CLI commands `Broadcast()` to all `*.sock` files in the directory. Protocol: newline-delimited JSON. Best-effort — CLI silently skips unreachable sockets. Message types: `task_update`, `task_note`, `agent_state`, `pr_linked`, `plan_proposed`, `plan_verdict`, `swarm_changed`, `review_changed`. Review changes carry explicit `tour_id`, `step_id`, and `kind` fields so CLI-authored answers can refresh the matching web review in real time.

**Adapter registration**: `AdapterRegistry` in service layer. Claude Code adapter in `internal/engine/hooks/claude_code.go`. `AgentServiceOptions` struct passes adapter, socket path, and `TmuxOptions` to `NewAgentService` for env var injection and session configuration on spawn.

**Migration**: `006_agent_activity.sql` adds `activity TEXT NOT NULL DEFAULT ''` column to `agent_sessions` table.

**State duration tracking**: `state_intervals` table records timestamped working/waiting intervals per task. `cli.AgentState()` calls both `store.UpdateAgentActivity()` and `store.RecordStateTransition()`. `ReconcileSessions()` closes orphaned intervals for dead agents. Durations computed at query time via SQL aggregation (including open intervals using `datetime('now')`). `Store.DB()` exposes underlying `*sqlx.DB` for advanced queries in tests.

**Duration formatting**: `board.formatDuration(d)` returns `""` (zero), `"<1m"` (under 60s), `"Xm"` (under 1h), `"Xh Ym"` (1h+). `CardData.WorkingDuration`/`WaitingDuration` populated during `DataLoadedMsg` via `AgentService.GetTaskDurations()` batch query.

## Tmux Status Line

Legato-spawned tmux sessions get a custom status bar showing live context. Solo agents see a summary of other sessions; swarm participants see swarm-local progress. Implemented via tmux `#()` shell expansion.

- **Injection**: `SpawnAgent` sets `status-right`, `status-interval` (5s), `status-style`, `status-left` on the session *before* user `tmux_options` — user config can override
- **Binary path**: Resolved once at startup via `os.Executable()`, passed to `AgentServiceOptions.BinaryPath`, embedded as absolute path in the `status-right` command
- **Solo vs swarm**: when `AgentSpawnOptions.ParentTaskID` is empty, `status-right` invokes `legato agent summary --exclude <taskID>` (working/waiting/idle counts for *other* sessions). When `ParentTaskID` is set, `status-right` invokes `legato agent status <taskID> --format tmux`, which prints swarm-local context (`x/y done`, last event + age, active sibling count, scope-warning icon).
- **Output format**: `legato agent summary` outputs tmux-native style markup (`#[fg=green]2 working #[fg=colour240]· #[fg=yellow]1 waiting #[fg=colour240]· #[fg=colour245]0 idle`). Zero-count working/waiting states omitted; idle always shown
- **Performance**: Opens SQLite, runs single `GROUP BY` aggregate query, exits. Sub-10ms typical execution

## Staccato Integration

`StaccatoAdapter` in `internal/engine/hooks/staccato.go` implements `AIToolAdapter`. Installs a `post-pr-create` hook at `~/.config/staccato/hooks/post-pr-create/legato-pr-link.sh` (staccato uses directory-based hooks, not config files).

**Flow**: Staccato `post-pr-create` fires when browser opens to PR creation page (PR may not exist yet) → hook reads `LEGATO_TASK_ID` (injected by legato's tmux session) + `ST_REPO_PATH` + `ST_BRANCH` → detects owner/repo from git remote + captures head commit SHA via `git rev-parse HEAD` → calls `legato task link $LEGATO_TASK_ID --branch $ST_BRANCH --repo owner/repo --sha $SHA` → IPC broadcast triggers immediate poll → background polling discovers PR when it actually exists.

**Key detail**: staccato's `post-pr-create` fires on PR page open, not on actual PR creation. So the initial link only stores repo+branch+sha (no PR number). The PR tracking service resolves the PR via `gh api repos/{owner}/{repo}/commits/{sha}/pulls` — an exact match on commit identity, immune to reused branch names and fork collisions — falling back to `gh pr list --head <branch> --repo <owner/repo>` filtered by link time (PRs created before the link are rejected as stale). This is why `PRMeta.Repo` is needed — legato may not be running from the same repo directory.

## Chimera Integration

`ChimeraAdapter` in `internal/engine/hooks/chimera.go` implements `AIToolAdapter`. Installs five activity-update hooks under `~/.chimera/hooks/<EventName>/legato-*.sh` (Chimera uses directory-based hooks, not a settings file — drop a script and it runs).

**Event → activity mapping** (per Chimera's documented integration recipe):

| Event              | Script                            | Activity   |
|--------------------|-----------------------------------|------------|
| `UserPromptSubmit` | `legato-prompt-submit.sh`         | `working`  |
| `PostToolUse`      | `legato-post-tool-use.sh`         | `working`  |
| `PermissionRequest`| `legato-permission.sh`            | `waiting`  |
| `Stop`             | `legato-stop.sh`                  | (clear)    |
| `SessionEnd`       | `legato-session-end.sh`           | (clear)    |

**Flow**: Each script gates on `LEGATO_TASK_ID` (injected by legato's tmux session) — outside a Legato-spawned session it's a no-op. Inside, it calls `legato agent state $LEGATO_TASK_ID --activity <state>`, which updates `agent_sessions.activity` and broadcasts IPC like Claude Code's hooks.

**Task context access**: Chimera role prompts include Legato-specific guidance that `legato task show $LEGATO_TASK_ID` fetches the current task description/context, and `--format full` includes structured metadata. In sandboxed Chimera sessions, run that command in host mode, the same as other Legato CLI calls.

**Coexistence with Claude Code**: Both adapters can be installed simultaneously. Hooks fire from different processes inside the same tmux session, and only `LEGATO_TASK_ID` (already injected by the agent service) needs to flow through — `ChimeraAdapter.EnvVars` returns nil. Install with `legato hooks install --tool chimera`.

## Codex Integration

`CodexAdapter` in `internal/engine/hooks/codex.go` implements `AIToolAdapter`. Installs four activity-update hooks via `.codex/hooks.json` and writes shell scripts to `.codex/hooks/legato-*.sh`. The scope is determined by the current working directory: run `legato hooks install --tool codex` from your home directory for global hooks (`~/.codex/`) or from a project directory for project-local hooks (`<repo>/.codex/`).

For swarm auto-launch, Legato injects the conductor/worker role prompt as Codex developer instructions with `codex -c developer_instructions="$(cat $LEGATO_ROLE_PROMPT_FILE)"`, then sends the usual kickoff message telling Codex to read `$LEGATO_BRIEF_FILE`.

**Event → activity mapping** (per Codex's documented hook events):

| Event               | Script                       | Activity  |
|---------------------|------------------------------|-----------|
| `UserPromptSubmit`  | `legato-prompt-submit.sh`    | `working` |
| `PostToolUse`       | `legato-post-tool-use.sh`    | `working` |
| `PermissionRequest` | `legato-permission-request.sh` | `waiting` |
| `Stop`              | `legato-stop.sh`             | (clear)   |

**Flow**: Codex hooks read JSON input on `stdin` and write JSON output on `stdout`. Each Legato hook script is a thin wrapper that gates on `LEGATO_TASK_ID` (injected by legato's tmux session) — outside a Legato-spawned session it's a no-op. Inside, it calls `legato agent state $LEGATO_TASK_ID --activity <state>`, which updates `agent_sessions.activity` and broadcasts IPC like Claude Code's hooks.

**Hook trust**: Codex requires users to review and trust non-managed hooks before they run. After installing, open the Codex CLI and run `/hooks` to inspect, review, and trust the Legato hook entries. Alternatively, pass `--dangerously-bypass-hook-trust` for one-off automation.

**Install**: `legato hooks install --tool codex`
**Uninstall**: `legato hooks uninstall --tool codex` — removes only Legato entries from `hooks.json` and deletes the scripts. Other user hooks in `hooks.json` are preserved.

**Coexistence**: Codex, Claude Code, Chimera, and Staccato adapters can all be installed simultaneously. Each adapter's hooks fire independently; only `LEGATO_TASK_ID` (already injected by the agent service) needs to flow through.
