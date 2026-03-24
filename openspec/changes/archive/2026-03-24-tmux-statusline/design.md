## Context

Legato spawns tmux sessions for agent work and already applies custom tmux options via `set-option` at spawn time (configured in `agents.tmux_options`). When attached to one of these sessions, the operator has no visibility into other agents' states — they must detach and return to the TUI's agent view to check. The tmux status bar is unused real estate that can display this information.

## Goals / Non-Goals

**Goals:**
- Show a live summary of agent session counts (working, waiting, idle) in the tmux status bar of legato-spawned sessions
- New `legato agent summary` CLI subcommand for machine/human-readable output
- Automatic status line injection at spawn time — no manual tmux config needed

**Non-Goals:**
- Replacing the TUI agent view — this is a glanceable summary, not a full dashboard
- Custom styling/theming of the tmux status line beyond sensible defaults
- Showing per-agent detail in the status line (just counts)
- Modifying the user's global tmux config

## Decisions

### 1. CLI subcommand for data, tmux `#()` for display

The tmux status line will use `#(legato agent summary)` shell expansion to call the legato binary. This is tmux's native mechanism for dynamic status content — tmux runs the command periodically (controlled by `status-interval`).

**Alternative considered:** A long-running daemon or socket-based approach. Rejected because tmux's built-in `#()` polling is simpler, well-understood, and avoids new infrastructure. The query hits local SQLite — sub-millisecond.

### 2. Output format: pre-formatted terminal string

`legato agent summary` outputs a single line with ANSI-colored counts, ready for tmux consumption (tmux supports `#[fg=colour]` style markup in status strings, but `#()` commands can also output raw text that tmux displays as-is).

The command will output tmux-native style markup (e.g., `#[fg=green]2 working #[fg=yellow]1 waiting #[fg=colour8]0 idle`) so colors render correctly in the tmux status bar.

**Alternative considered:** Plain text output with tmux style wrapping done at config time. Rejected because the CLI knows the counts and can conditionally colorize (e.g., yellow for waiting > 0), which would be awkward to express in static tmux format strings.

### 3. Inject via hardcoded tmux options at spawn, not config

Legato will set specific tmux options on spawned sessions:
- `status-right` → `#(legato agent summary)`
- `status-interval` → `5` (refresh every 5 seconds)
- `status-style` → minimal styling to distinguish from default tmux

These are applied via the existing `tmux.SetOption()` mechanism after spawn, same as user-configured `tmux_options`. They are set as session-level options so they don't affect the user's global tmux config.

User-configured `tmux_options` are applied **after** legato's defaults, so the user can override any of these if desired.

**Alternative considered:** Adding these to the config file's `tmux_options` section. Rejected because this should work out-of-the-box without config changes — it's a core feature of legato sessions, not a user preference.

### 4. Summary counts from agent_sessions table

The CLI queries `agent_sessions WHERE status='running'` and aggregates by `activity` column. This reuses the existing store layer — no new tables or queries beyond a simple `GROUP BY`.

Counts: working (activity='working'), waiting (activity='waiting'), idle (activity='' AND status='running').

### 5. Exclude current session from counts

The summary should show "other" sessions, not include the one the operator is currently in. The `LEGATO_TASK_ID` env var is available in the tmux session and can be passed to the CLI to exclude that task from counts.

Format: `legato agent summary --exclude <task-id>`. The tmux status-right will use `#(legato agent summary --exclude $LEGATO_TASK_ID)`.

## Risks / Trade-offs

- **[tmux `#()` overhead]** → The CLI opens SQLite, runs one query, and exits. Benchmarks show this at <10ms. With a 5s interval, overhead is negligible. If somehow slow, user can override `status-interval` via `tmux_options`.
- **[Binary not in PATH]** → `#()` needs the full path to legato. At spawn time, we resolve the binary path via `os.Executable()` and use the absolute path in the status-right string.
- **[User tmux_options conflict]** → If user sets `status-right` in their `tmux_options`, it overwrites legato's. This is intentional — user config wins. Could document how to incorporate legato's summary into a custom status-right.
- **[Env var in #()]** → tmux `#()` runs commands in a shell context where session env vars may or may not be available. We'll use `tmux show-environment` or set the status-right with the task ID baked in at spawn time (since we know it then).
