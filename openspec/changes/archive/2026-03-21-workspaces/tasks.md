## 1. Database & Store Layer

- [x] 1.1 Create migration `008_workspaces.sql`: add `workspaces` table (id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL, color TEXT, sort_order INTEGER NOT NULL DEFAULT 0) and add `workspace_id INTEGER REFERENCES workspaces(id)` nullable column to `tasks` table
- [x] 1.2 Add `Workspace` struct to `store/types.go` (ID int, Name string, Color *string, SortOrder int) and add `WorkspaceID *int` field to `Task` struct
- [x] 1.3 Implement workspace store CRUD: `CreateWorkspace`, `ListWorkspaces`, `GetWorkspace`, `EnsureWorkspace` (upsert by name)
- [x] 1.4 Implement `ListTasksByStatusAndWorkspace(status, workspaceView)` with three filter modes: all (no filter), unassigned (NULL), specific workspace (by ID)
- [x] 1.5 Update `InsertTask` and `UpsertTask` to include `workspace_id` in INSERT/UPDATE statements
- [x] 1.6 Add `UpdateTaskWorkspace(taskID, workspaceID *int)` store method
- [x] 1.7 Write tests for all new store methods using real SQLite in t.TempDir()

## 2. Config Layer

- [x] 2.1 Add `Workspaces []WorkspaceConfig` to `Config` struct in `config/config.go` with `WorkspaceConfig{Name string, Color string}`
- [x] 2.2 Write config parsing tests for workspaces section (present, absent, empty)

## 3. Service Layer

- [x] 3.1 Add `WorkspaceView` type to service layer: `ViewAll`, `ViewUnassigned`, `ViewWorkspace(id int)` — use a struct with a kind enum and optional ID
- [x] 3.2 Add `Workspace` service type (ID, Name, Color) and `ListWorkspaces()` method to BoardService
- [x] 3.3 Update `BoardService.ListCards` to accept a `WorkspaceView` parameter and pass through to store
- [x] 3.4 Update `BoardService.CreateTask` to accept optional `workspaceID *int` parameter
- [x] 3.5 Add `BoardService.UpdateTaskWorkspace(taskID string, workspaceID *int)` — reject remote tasks
- [x] 3.6 Add workspace name to `Card` struct for display in "All" view (populate via JOIN or post-query lookup)
- [x] 3.7 Write service layer tests with real SQLite store

## 4. Workspace Seeding

- [x] 4.1 Add workspace seeding logic in startup (after migration, before TUI): read config workspaces, call `EnsureWorkspace` for each
- [x] 4.2 Write tests for seeding: new workspaces inserted, existing skipped, empty config no-ops

## 5. TUI — Board Workspace Filtering

- [x] 5.1 Add `workspaceView WorkspaceView` and `workspaces []Workspace` fields to board model; default to `ViewAll`
- [x] 5.2 Update board data loading to pass `workspaceView` to `ListCards`
- [x] 5.3 Add `WorkspaceName` and `WorkspaceColor` fields to `CardData`; populate when `ViewAll` is active
- [x] 5.4 Update card rendering to show workspace tag on metadata line when `WorkspaceName` is set (colored by `WorkspaceColor`)
- [x] 5.5 Wire `w` keybinding to open workspace switcher overlay

## 6. TUI — Workspace Switcher Overlay

- [x] 6.1 Create `internal/tui/overlay/workspace.go`: overlay model listing "All", "Unassigned", and each workspace with color indicators
- [x] 6.2 Implement j/k navigation, single-letter shortcuts, enter to select, esc to dismiss
- [x] 6.3 Return a `WorkspaceSelectedMsg` with the chosen `WorkspaceView`; app handles by updating board filter and refreshing data
- [x] 6.4 Add `overlayWorkspace` to the overlay type enum in the app model
- [x] 6.5 Write overlay tests (selection, navigation, dismiss)

## 7. TUI — Status Bar Workspace Indicator

- [x] 7.1 Add workspace indicator to status bar: show workspace name in color after sync state
- [x] 7.2 Add `w` workspace hint to board view key hints
- [x] 7.3 Write status bar rendering tests for workspace indicator

## 8. TUI — Create Overlay Workspace Field

- [x] 8.1 Add workspace field to create overlay (cycle with h/l like column field, between None + workspace list)
- [x] 8.2 Pre-fill workspace field with active workspace (None if All/Unassigned view)
- [x] 8.3 Pass selected workspace to `BoardService.CreateTask`
- [x] 8.4 Write create overlay tests for workspace field cycling and submission

## 9. Wiring & Integration

- [x] 9.1 Wire workspace seeding in `cmd/legato/main.go` after migration
- [x] 9.2 Pass workspace list and initial view through `NewApp` → board model
- [x] 9.3 Handle `WorkspaceSelectedMsg` in app model: update board view, trigger data refresh
- [x] 9.4 End-to-end manual test: create workspaces in config, start app, switch views, create tasks in different workspaces, verify filtering
