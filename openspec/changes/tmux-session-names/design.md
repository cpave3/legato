## Context

The agent sidebar renders a two-line entry per session: line 1 is the status icon + task ID, line 2 is the `Command` field. Currently `Command` is hardcoded to `"shell"` when the session is spawned and stored statically in SQLite. Every entry looks identical on line 2, wasting space and providing no useful information.

Tmux exposes per-pane metadata via format variables — `pane_current_command` returns the name of the foreground process in the pane (e.g., `claude`, `bash`, `vim`, `node`). This updates dynamically as the user runs commands.

## Goals / Non-Goals

**Goals:**
- Show the live foreground process name for each agent session in the sidebar
- Query tmux dynamically so the displayed name reflects what's actually running

**Non-Goals:**
- Removing the `command` column from the database (harmless, avoid migration churn)
- Showing full command lines or arguments (just the process name)
- Custom user-editable session labels

## Decisions

### 1. Query `pane_current_command` via `tmux list-panes`

**Choice**: Add a `PaneCommand(sessionName string) (string, error)` method to `TmuxManager` that runs `tmux list-panes -t <session> -F "#{pane_current_command}" -l 1`.

**Why**: `pane_current_command` gives the foreground process name — this is the same value shown in terminal tab titles. It updates automatically when processes start/stop. `-l 1` limits to one pane (we use single-pane sessions).

**Alternatives considered**:
- `window_name`: Often stays as the initial command (e.g., `zsh`) and doesn't update as reliably
- `pane_title`: Requires the running process to set the title via escape sequences — not all programs do this

### 2. Populate `Command` at list time, not spawn time

**Choice**: In `AgentService.ListAgents()`, query tmux for each running session's pane command and override the `Command` field with the live value. Fall back to the stored DB value for dead sessions.

**Why**: This keeps the data fresh without needing a polling/update loop in the service layer. `ListAgents()` is already called on every sidebar render tick (200ms polling), so the cost is one additional tmux call per running session — trivial for the small number of concurrent agents.

**Alternatives considered**:
- Periodic background refresh writing to DB: Over-engineered, adds write contention for something only the TUI needs
- Query in TUI layer directly: Violates the architecture (TUI shouldn't import engine)

### 3. Batch query for all sessions

**Choice**: Add a `PaneCommands() (map[string]string, error)` method that queries all legato-prefixed sessions in one call: `tmux list-panes -a -F "#{session_name} #{pane_current_command}" -f "#{m:legato-*,#{session_name}}"`.

**Why**: One tmux invocation instead of N. The filter flag `-f` scopes to legato sessions. Parse output as space-separated `session_name command` pairs.

## Risks / Trade-offs

- **[Tmux version compatibility]** → The `-f` filter flag requires tmux 2.6+. This is the minimum version we already depend on for other features, so no new risk.
- **[Command flicker]** → The pane command changes rapidly during shell initialization (e.g., `.bashrc` sourcing). At 200ms polling this may show transient process names. Acceptable — the value stabilizes quickly and matches what a terminal tab would show.
- **[Dead session fallback]** → Dead sessions can't be queried via tmux. The service falls back to the stored DB value ("shell"). This is fine — dead sessions are dimmed in the sidebar and the process name is less relevant.
