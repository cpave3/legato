## Context

The agent view currently renders as a full-width header bar (showing selected agent metadata) with the terminal capture below. Users cycle between agents with j/k, but can only see one agent's status at a time. The original spec called for a sidebar layout, but a header bar was implemented instead because dynamically changing the terminal panel width caused tmux capture-pane output to reflow.

The core insight for this change: if the sidebar is **always present** and the tmux session is **spawned at the terminal panel width**, the capture output width never changes.

## Goals / Non-Goals

**Goals:**
- Persistent sidebar showing all agent sessions with activity state indicators
- Constant terminal panel width — tmux sessions spawned to match panel width
- Visual language consistent with board card indicators (working=green, waiting=blue, idle=dim)
- Users can see at a glance which agents need attention without cycling through them

**Non-Goals:**
- Resizable sidebar (fixed width calculation is sufficient)
- Sidebar in board view (agent sidebar is only in the agent view)
- Scrollable terminal output (keep existing behavior: show bottom of buffer)
- Changing the tmux Capture API to pass width — capture-pane returns whatever the pane width is; we control width at spawn time

## Decisions

### 1. Fixed sidebar width: 30 characters

The sidebar needs to fit: status icon (2), task ID (up to 12), activity label (up to 9), padding (4), border (1). That's ~28 chars. Use 30 for a clean number with breathing room.

**Why not percentage-based?** The existing spec says "220 chars or 30%". 220 chars is absurdly wide for a sidebar. 30% of a typical 120-col terminal is 36 — workable but wastes horizontal space the terminal panel could use. A fixed 30 chars is predictable and keeps the terminal panel maximally useful.

**Alternative considered:** Configurable width in config.yaml. Rejected — adds complexity for minimal benefit.

### 2. Spawn tmux sessions at (totalWidth - sidebarWidth) columns

When `SpawnAgent` is called, pass the desired pane width. The tmux `Spawn` method will use `tmux new-session -x <width> -y <height>` to set the initial pane geometry. This means capture-pane output is always the right width for the terminal panel.

**Trade-off:** If the user resizes their terminal, the tmux pane width becomes stale. However, capture-pane still returns at the original width, which gets truncated or padded in the render. This is acceptable — the user can detach/reattach or kill/respawn to get a fresh size. A future enhancement could send `tmux resize-pane` on `WindowSizeMsg`.

### 3. Sidebar renders agent entries as compact rows

Each sidebar entry:
```
 ● REX-1238 WORKING
 ◆ abc12def waiting
 ◌ REX-999  idle
```

- Line 1: status icon + task ID + activity state
- Selected entry gets highlighted background + left border accent
- No second line (command, elapsed time) — keep it compact. These details are in the terminal panel header.

**Why drop command/elapsed from sidebar?** At 30 chars, two-line entries halve the visible session count. The user cares about "which agents need me" — that's answered by the activity indicator alone. Details are visible in the terminal header when selected.

### 4. Terminal panel keeps the header bar

The existing header bar (task ID, command, elapsed time, keybindings) moves to be the terminal panel header rather than full-width. This preserves all existing metadata display without needing to cram it into the sidebar.

### 5. Layout composition in View()

```
┌──────────┬────────────────────────────────────────────────┐
│ Sidebar  │ Terminal Header                                │
│ ● REX-12 │ ● REX-1238 · shell · 5m 23s                   │
│ ◆ abc123 ├────────────────────────────────────────────────┤
│ ◌ REX-99 │                                                │
│          │  $ claude --task "implement feature"            │
│          │  Working on it...                               │
│          │  $ _                                            │
│          │                                                │
│          │                                                │
│ j/k ↵ X  │                                                │
└──────────┴────────────────────────────────────────────────┘
```

Use `lipgloss.JoinHorizontal` with the sidebar and terminal panel as two blocks. Sidebar has a right border. Keybinding hints go at the bottom of the sidebar.

### 6. Width parameter threading

The width needs to flow: `app.go` → `agents.Model` → `AgentService.SpawnAgent()` → `tmux.Manager.Spawn()`.

- `agents.Model` already receives `width` via `WindowSizeMsg`
- `agents.Model.terminalWidth()` = `m.width - sidebarWidth`
- `SpawnAgentMsg` includes `Width` and `Height` fields
- `AgentService.SpawnAgent` passes width/height to `tmux.Manager.Spawn`
- `tmux.Manager.Spawn` adds `-x <width> -y <height>` to the `tmux new-session` command

## Risks / Trade-offs

**[Terminal resize mismatch]** → Users can detach/reattach to reset. Future: send `tmux resize-pane` on `WindowSizeMsg`.

**[30-char sidebar too narrow for long task IDs]** → Truncate with ellipsis. 8-char local IDs fit easily. Jira keys like `PROJECT-1234` (13 chars) get truncated to `PROJECT-12…` — still identifiable.

**[Existing tests break]** → `model.go` tests check model state not rendered output, so layout changes won't break them. New tests needed for sidebar rendering and width calculation.
