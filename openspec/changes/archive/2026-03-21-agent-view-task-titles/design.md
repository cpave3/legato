## Context

The agent sidebar currently renders each entry as a 2-line card: status icon + task ID on line 1, command name on line 2. Users with multiple running agents must remember which task ID maps to which task, adding cognitive overhead. The task title is available in the `tasks` table but is not currently fetched or passed through to the agent view.

## Goals / Non-Goals

**Goals:**
- Show truncated task title in agent sidebar entries
- Keep sidebar entries compact (3 lines max: status+ID, title, command)
- Populate title data in the service layer so the TUI just renders it

**Non-Goals:**
- Changing the terminal panel layout or content
- Adding new database queries — titles come from existing store methods
- Making card height configurable

## Decisions

### 1. Enrich AgentSession at service layer via batch title lookup

Add a `Title` field to `service.AgentSession`. In `ListAgents()`, after fetching sessions from the DB and populating tmux data, do a single pass to look up task titles from the store. This keeps the TUI layer thin — it just renders what it receives.

**Alternative considered**: Look up titles in the TUI layer by calling `BoardService.GetCard()` per agent. Rejected because it mixes concerns (TUI doing data enrichment) and would require passing `BoardService` into the agents model.

### 2. Title on its own line, truncated with ellipsis

The title renders as a third line in the sidebar entry, styled dim/muted (same as command line). Truncated to `cardContentWidth` with `…` suffix when it exceeds the available width. This keeps entries scannable without horizontal overflow.

**Alternative considered**: Title on the same line as the task ID (e.g., `● abc123 — Fix the bug`). Rejected because IDs + titles together often exceed the sidebar width, leading to ugly wrapping.

### 3. Terminal header includes title

The terminal panel header already shows the task ID. We add the title next to it, truncated to fit the header width. This is consistent with the sidebar showing titles.

## Risks / Trade-offs

- **Taller sidebar entries** → fewer agents visible without scrolling. Acceptable trade-off since most users run 1-5 agents, and the title adds significant value.
- **Extra store query in ListAgents** → one `GetTask` call per active agent per poll cycle (200ms). Since agent counts are small (typically <10) and SQLite is local, this is negligible. Could batch into a single query later if needed.
