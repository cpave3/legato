## 1. Service Layer — Add Title to AgentSession

- [ ] 1.1 Add `Title string` field to `service.AgentSession` struct
- [ ] 1.2 Update `ListAgents()` to populate `Title` by looking up each agent's task from the store after fetching sessions
- [ ] 1.3 Add/update tests in `agent_test.go` verifying that `ListAgents()` returns sessions with populated titles (including edge case where task is missing)

## 2. TUI — Sidebar Entry Rendering

- [ ] 2.1 Update `renderSidebarEntry()` in `internal/tui/agents/model.go` to render a third line with the truncated task title (dim style, ellipsis on overflow)
- [ ] 2.2 Skip the title line when `Title` is empty (no blank line for missing titles)
- [ ] 2.3 Update sidebar entry height calculation to account for the new title line

## 3. TUI — Terminal Header

- [ ] 3.1 Update `renderTerminalHeader()` to display the task title after the task ID, truncated to fit the header width
- [ ] 3.2 Fall back to ID-only display when title is empty
