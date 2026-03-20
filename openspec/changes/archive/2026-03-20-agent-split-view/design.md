## Context

Legato is a kanban TUI with a 3-layer architecture (engine/service/tui). It currently has two view modes (board, detail) managed by a `viewType` enum in the root App model. The board tracks tickets synced from Jira. Users want to spawn AI coding agents (Claude Code, aider, etc.) on individual cards and manage them from within legato.

The TUI runs in Bubbletea's alt-screen mode. Tmux is widely available and provides session persistence, multiplexing, and programmatic control — making it the natural choice for hosting agent terminals that must survive legato restarts.

## Goals / Non-Goals

**Goals:**
- Spawn a persistent tmux session tied to a kanban card
- Track sessions in SQLite so legato can reconnect after restart
- Embed live terminal output in a split-view TUI panel
- Let users drop into the session to type commands, and escape back to legato
- Support multiple concurrent agent sessions
- Keep the spawn mechanism modular so command presets can be added later

**Non-Goals:**
- Git worktree management (future phase)
- Command presets or agent type configuration (future phase)
- Injecting context/prompts into agents (future phase — mockup shows this but it's out of scope)
- API cost tracking (shown in mockup, out of scope)
- Pause/resume agent functionality (future phase)

## Decisions

### 1. Tmux for session persistence

**Decision:** Use tmux sessions as the persistence mechanism for agent terminals.

**Rationale:** Tmux is ubiquitous on developer machines, provides named sessions that survive parent process death, supports programmatic control via CLI (`tmux new-session`, `tmux capture-pane`, `tmux send-keys`), and can be attached/detached without killing the process.

**Alternatives considered:**
- **Screen**: Less common, worse programmatic API
- **Direct PTY management**: Would require reimplementing persistence, no survival after legato exits
- **Zellij**: Newer, less ubiquitous, API still maturing

### 2. Terminal output via `tmux capture-pane` polling

**Decision:** Poll `tmux capture-pane -t <session> -p` on a timer (e.g., every 200ms) to read terminal output, render it as static text in the TUI panel.

**Rationale:** Bubbletea owns the terminal in alt-screen mode. We cannot literally embed a live PTY inside a Bubbletea view. Polling capture-pane gives us a snapshot of the terminal content that we render as text. This is simpler than PTY forwarding and works within Bubbletea's architecture.

When the user wants to interact (type commands), we suspend Bubbletea's alt-screen via `tea.ExecProcess` to attach to the tmux session directly. This gives full terminal fidelity — colors, cursor, interactive programs all work.

**Alternatives considered:**
- **Raw PTY embedding**: Bubbletea doesn't support nested PTY — would require forking or major workarounds
- **VHS/asciinema-style recording**: Too much overhead, not interactive
- **Always-attached tmux**: Would steal the terminal from Bubbletea

### 3. Escape key: Ctrl+] (configurable)

**Decision:** Default escape key is `Ctrl+]`. Configurable via `agents.escape_key` in config.yaml.

**Rationale:** `Ctrl+]` is the classic telnet escape character, rarely used in terminal applications or shells. When the user attaches to the tmux session via `tea.ExecProcess`, we wrap the attach in a shell script that intercepts `Ctrl+]` and detaches, returning control to Bubbletea.

**Implementation:** The tmux attach is wrapped: we set the tmux `detach-client` key to `Ctrl+]` for this session so pressing it detaches cleanly and returns to legato.

### 4. Agent view as separate mode (viewAgents)

**Decision:** Add `viewAgents` to the existing `viewType` enum. Toggle with `A` key from the board view.

**Rationale:** Keeps the board as the default home screen. Agent view is a dedicated mode with its own model (`internal/tui/agents/`), following the same pattern as board and detail views. The root App routes input and rendering based on `active viewType`.

### 5. Session tracking in SQLite

**Decision:** New `agent_sessions` table in the existing SQLite database, with a new migration.

**Schema:**
```sql
CREATE TABLE agent_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id TEXT NOT NULL,
    tmux_session TEXT NOT NULL UNIQUE,
    command TEXT NOT NULL DEFAULT 'shell',
    status TEXT NOT NULL DEFAULT 'running',
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    ended_at DATETIME,
    FOREIGN KEY (ticket_id) REFERENCES tickets(id)
);
```

**Rationale:** Follows the existing migration pattern. `tmux_session` is the tmux session name (e.g., `legato-REX-1238`). `status` is `running`, `stopped`, or `dead`. On startup, legato queries this table and validates against `tmux list-sessions` to reconcile state.

### 6. Engine layer: `internal/engine/tmux/`

**Decision:** New `internal/engine/tmux/` package for all tmux process management. Exposes a clean Go API; no tmux details leak into service or TUI layers.

**API surface:**
- `New(opts Options) *Manager` — create manager with configurable escape key
- `Spawn(name, workDir string) error` — create a new tmux session
- `Attach(name string) *exec.Cmd` — return an exec.Cmd suitable for `tea.ExecProcess`
- `Capture(name string) (string, error)` — capture current pane content
- `Kill(name string) error` — destroy session
- `ListSessions() ([]string, error)` — list active legato-prefixed sessions
- `IsAlive(name string) (bool, error)` — check if session exists

### 7. Service layer: `AgentService`

**Decision:** New `AgentService` in `internal/service/` that composes tmux.Manager + Store operations. The TUI depends on this service, never on tmux directly.

**Responsibilities:**
- Spawn: create tmux session + insert DB row
- Kill: destroy tmux session + update DB row
- List: query DB + validate against live tmux sessions (mark dead sessions)
- Reconnect: on startup, reconcile DB state with tmux reality
- Capture: proxy to tmux.Manager for terminal output

### 8. Naming convention for tmux sessions

**Decision:** Session names follow pattern `legato-<TICKET_ID>` (e.g., `legato-REX-1238`).

**Rationale:** Prefix prevents collision with user's own tmux sessions. Ticket ID makes sessions discoverable outside legato (`tmux ls | grep legato`).

## Risks / Trade-offs

**[tmux not installed]** → Check for tmux on agent view entry. Show clear error in status bar if missing. Agent features gracefully degrade — board still works.

**[capture-pane polling latency]** → 200ms polling means output is slightly delayed compared to a real terminal. Acceptable for a monitoring view — user attaches for real interaction. Can tune interval later.

**[stale sessions after crash]** → If legato crashes without cleanup, tmux sessions persist (feature, not bug) but DB may be stale. Reconciliation on startup handles this: query `tmux list-sessions`, mark DB sessions as `dead` if tmux session is gone, discover running sessions not in DB.

**[multiple legato instances]** → If user runs two legato instances, both see the same tmux sessions (same SQLite DB). This is fine — tmux sessions are global. DB acts as shared state. No locking needed for v1.

**[Ctrl+] conflict]** → Unlikely but possible in some terminal apps. Configurable escape key mitigates this.

## Open Questions

- Should we limit the number of concurrent agent sessions? (Suggest: no limit for v1, revisit if resource issues arise)
- Should killing an agent from legato also kill the tmux session, or just detach tracking? (Suggest: kill the tmux session — user can always `tmux` directly if they want untracked sessions)
