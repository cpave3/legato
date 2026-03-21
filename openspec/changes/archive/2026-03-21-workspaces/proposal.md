## Why

Users manage both personal and work tasks in Legato but currently see everything on one board. There's no way to separate contexts, leading to clutter and cognitive overhead when switching between work modes. Workspaces let users partition their board into logical groups (e.g. "work", "personal") and switch between them instantly.

## What Changes

- Add a `workspaces` table in SQLite (id, name, color, sort_order) to define available workspaces
- Add a `workspace_id` foreign key on the `tasks` table (nullable — unassigned tasks belong to no workspace)
- Seed default workspaces from config or setup wizard
- Filter board view by active workspace — only tasks matching the workspace are shown
- Provide keyboard shortcut to cycle/switch workspaces from the board
- "All" view shows tasks from every workspace, tagged with their workspace name
- Unassigned tasks (workspace_id = NULL) appear in their own view and in "All"
- Task creation overlay defaults new tasks to the active workspace
- Workspace indicator in the status bar showing current filter

## Capabilities

### New Capabilities
- `workspace-filtering`: Workspace table, task foreign key, store CRUD, board filtering, switching UI, and config integration

### Modified Capabilities
- `kanban-board`: Board must filter cards by active workspace and show workspace tags in "all" view
- `local-task-management`: Task creation and editing must support workspace assignment
- `status-bar`: Must display current workspace indicator

## Impact

- **Database**: New migration adding `workspaces` table and `workspace_id` column on `tasks`
- **Store**: New Workspace type and CRUD; Task struct gains `WorkspaceID`; card queries filter by workspace
- **Service**: BoardService gains workspace-aware card listing; CreateTask accepts workspace
- **Config**: Optional `workspaces` section for pre-seeding workspace names/colors
- **TUI Board**: Workspace filter state, switching keybinding, workspace tags on cards in "all" view
- **TUI Status Bar**: Workspace indicator
- **TUI Create Overlay**: Default workspace assignment, workspace picker in creation form
- **CLI**: `legato task update` may need `--workspace` flag
