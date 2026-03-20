## Context

Legato currently spawns AI agents in tmux sessions via `AgentService` and tracks them in the `agent_sessions` SQLite table. The TUI polls tmux output at 200ms intervals for display, but there's no feedback channel from the agent back to Legato — task status changes require manual user action.

Claude Code supports a hooks system that fires shell commands on lifecycle events (session start/stop, tool use, task completion). By injecting environment variables into Legato-spawned tmux sessions and installing Claude Code hooks that call back to Legato's CLI, we can close the feedback loop: the agent tells Legato what it's doing, and the board updates in real-time.

This is designed as the first AI tool integration, with an abstract adapter layer so future integrations (Aider, Cursor, Copilot Workspace, etc.) follow the same pattern.

## Goals / Non-Goals

**Goals:**
- Automatic task status updates when Claude Code reaches lifecycle milestones (started working, completed, failed)
- Real-time board updates in any running Legato TUI instance when a hook fires
- Abstract adapter interface so Claude Code is one of N possible integrations
- Simple `legato hooks install` command that configures Claude Code hooks in the project
- Environment variable injection into Legato-spawned tmux sessions so hooks know context
- CLI subcommands for hook scripts (and humans) to push updates to Legato

**Non-Goals:**
- Bidirectional communication (Legato telling Claude Code what to do) — this is agent → Legato only
- Modifying Claude Code's behavior via hooks (blocking, input rewriting) — we only observe
- Supporting hooks outside of Legato-spawned tmux sessions (users can set this up manually, but we don't auto-configure it)
- GUI/web dashboard — TUI only for now
- Hook scripts that require `jq` or other non-standard dependencies — pure shell/Go

## Decisions

### 1. IPC via Unix Domain Socket (not HTTP, not file watching)

The running TUI needs to learn about updates from CLI invocations triggered by hooks. Options considered:

- **HTTP server** (`internal/server/` already exists): Requires binding a port, port conflicts in multi-instance, firewall concerns. The existing server stub uses `BoardService` interface but isn't started by default.
- **File watching** (write to a file, TUI polls): Simple but adds latency, no atomic signaling, cleanup burden.
- **Unix domain socket**: No port conflicts, no network exposure, naturally scoped to user, fast. Socket path can be deterministic from the DB path (e.g., alongside the SQLite file).

**Decision**: Unix domain socket at `$XDG_RUNTIME_DIR/legato/legato.sock` (fallback: `/tmp/legato-$UID/legato.sock`). The TUI starts a listener on launch; CLI commands connect as clients. Protocol is newline-delimited JSON messages.

### 2. CLI Subcommands via Cobra-style Dispatch (not separate binaries)

The `legato` binary needs to serve double duty: TUI mode (default, no args) and CLI mode (subcommands like `legato task update`).

**Decision**: Add subcommand dispatch to `cmd/legato/main.go`. When `os.Args[1]` matches a known subcommand, run that instead of the TUI. Keep it minimal — no heavy framework, just a switch on the first arg. Subcommands:
- `legato task update <task-id> --status <status>` — update task status/column
- `legato task note <task-id> <message>` — append a note/log entry (future use)
- `legato hooks install [--tool claude-code]` — install hooks for a given AI tool
- `legato hooks uninstall [--tool claude-code]` — remove hooks

### 3. Abstract Adapter Interface in Service Layer

The adapter interface lives in `internal/service/` and defines what an AI tool integration provides:

```go
type AIToolAdapter interface {
    Name() string                          // "claude-code", "aider", etc.
    InstallHooks(projectDir string) error  // configure the tool's hook system
    UninstallHooks(projectDir string) error
    EnvVars(taskID, socketPath string) map[string]string  // env vars to inject
}
```

Hook script generation is adapter-specific (Claude Code needs specific JSON parsing from stdin). The adapter owns its hook scripts.

**Why not in engine/**: Adapters need to understand task concepts (task ID, status values) which are service-level concerns. But they don't import the store directly — they generate scripts that call the CLI.

### 4. Environment Variable Injection via tmux `send-keys` or `set-environment`

Tmux supports `set-environment` which makes env vars available to new processes in the session. This is cleaner than `send-keys "export FOO=bar"`.

**Decision**: Use `tmux set-environment -t <session> KEY VALUE` after spawning. Variables injected:
- `LEGATO_TASK_ID` — the task ID this session is working on
- `LEGATO_SOCKET` — path to the Unix domain socket for IPC
- `LEGATO_PROJECT_DIR` — project root, for hook script context

### 5. Claude Code Hook Events to Map

Based on the hooks documentation, the relevant events for task tracking:

| Claude Code Event | Legato Action | Rationale |
|---|---|---|
| `Stop` | Update task status to reflect work completed | Agent finished a response turn |
| `TaskCompleted` | Move task to "Done" column | Explicit task completion signal |
| `PostToolUse` (matcher: `Bash\|Edit\|Write`) | Log activity (future) | Track what the agent is doing |
| `SubagentStop` | Log activity (future) | Subagent finished work |

For v0, we map only `Stop` and `TaskCompleted`. The hook script checks `$LEGATO_TASK_ID` — if unset, exits silently (not a Legato session).

### 6. Hook Scripts Are Embedded Go Templates (not external files)

Hook scripts need to be generated with the correct `legato` binary path and socket path. Rather than shipping separate `.sh` files, embed them as Go templates in the adapter package.

**Decision**: `internal/engine/hooks/claude_code.go` contains `embed`-ed or template-literal hook scripts. `legato hooks install` renders them to `.claude/hooks/legato-*.sh` and updates `.claude/settings.json`.

### 7. Socket Protocol: Newline-Delimited JSON

Messages from CLI → TUI over the socket:

```json
{"type":"task_update","task_id":"abc123","status":"done"}
{"type":"task_note","task_id":"abc123","message":"Finished implementing feature"}
```

The TUI socket listener deserializes and publishes to the event bus (`EventCardUpdated` / new event types as needed). The board reactively updates via existing Bubbletea message flow.

## Risks / Trade-offs

- **Socket lifecycle**: If Legato crashes, the socket file is orphaned → Mitigation: check for stale sockets on startup (try connect, remove if refused), use `defer os.Remove(socketPath)`.
- **Multiple Legato instances**: Two TUIs on the same DB would race on the socket path → Mitigation: include a short random suffix or PID in socket name; CLI broadcasts to all discovered sockets. Or: single-instance lock on the socket path.
- **Claude Code not installed**: `legato hooks install` must fail gracefully with a clear message if `.claude/` doesn't exist or Claude Code isn't detected.
- **Hook script breaks on Legato upgrade**: If the CLI interface changes, installed hooks break → Mitigation: version the hook scripts, `legato hooks install` always regenerates.
- **tmux `set-environment` only affects new processes**: If Claude Code is already running when env vars are set, it won't see them → Mitigation: set env vars before spawning the shell in the tmux session (Spawn already creates a fresh session, so the initial shell inherits).
- **Security**: Socket is user-scoped (filesystem permissions on `$XDG_RUNTIME_DIR`). Hook scripts run as the user. No elevation concerns.
