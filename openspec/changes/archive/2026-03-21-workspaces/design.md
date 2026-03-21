## Context

Legato currently shows all tasks on a single board. Users with mixed work/personal tasks have no way to separate contexts. The proposal adds workspaces as a first-class concept backed by a dedicated SQLite table with a foreign key on tasks.

Current data flow: `store.ListTasksByStatus(status)` → `BoardService.ListCards(column)` → `board.Model` renders all cards. Workspace filtering needs to intercept at the store query level and propagate the active workspace through the TUI state.

## Goals / Non-Goals

**Goals:**
- Add a `workspaces` table and `workspace_id` FK on `tasks` for clean relational modeling
- Filter board cards by active workspace at the store query level
- Provide fast keyboard-driven workspace switching (single key from board view)
- Support four view modes: specific workspace, unassigned, and "all" (composite)
- Show workspace tags on cards in "all" view
- Default new tasks to the active workspace

**Non-Goals:**
- Per-workspace column configurations (all workspaces share the same columns)
- Workspace CRUD from the TUI (managed via config/setup only for v1)
- Workspace assignment for synced/remote tasks (only local tasks)
- Workspace-based access control or multi-user support

## Decisions

### 1. Dedicated `workspaces` table with auto-incrementing integer ID

**Choice**: `workspaces(id INTEGER PRIMARY KEY, name TEXT UNIQUE NOT NULL, color TEXT, sort_order INTEGER)` with `tasks.workspace_id INTEGER REFERENCES workspaces(id)` nullable FK.

**Why over string column**: Enforces referential integrity, provides a single place to store workspace metadata (color, sort order), avoids string duplication, and makes rename trivial (update one row). Integer FK is compact and fast for indexing.

**Alternatives considered**:
- Text column on tasks: simpler migration but duplicates names, no place for metadata, rename requires updating all tasks
- JSON config only: no relational integrity, can't query efficiently

### 2. Workspace seeding from config.yaml

**Choice**: Define workspaces in config under a `workspaces` key. On startup, sync config → `workspaces` table (insert missing, don't delete existing). Color is optional.

```yaml
workspaces:
  - name: Work
    color: "#4A9EEF"
  - name: Personal
    color: "#7BC47F"
```

**Why**: Workspaces change rarely and are best declared in config alongside other board settings. The store is the source of truth at runtime; config is the seeding mechanism.

### 3. View modes as an enum, not a workspace ID

**Choice**: Active filter is a `WorkspaceView` enum: `ViewAll`, `ViewUnassigned`, or `ViewWorkspace(id)`. Stored on the board model. This avoids conflating "show everything" with a magic workspace ID.

### 4. Keyboard shortcut: `w` to open workspace switcher overlay

**Choice**: Press `w` on the board to open a small overlay listing workspaces + "All" + "Unassigned". Single-letter shortcuts (like move overlay) for quick selection, or j/k + enter.

**Why over cycling**: With 2+ workspaces plus All and Unassigned, cycling requires too many presses. An overlay gives visual feedback and scales.

### 5. Store-level filtering with optional workspace parameter

**Choice**: Add `ListTasksByStatusAndWorkspace(status, workspaceView)` to the store. The workspace view translates to SQL:
- `ViewAll`: no WHERE clause on workspace_id (return all)
- `ViewUnassigned`: `WHERE workspace_id IS NULL`
- `ViewWorkspace(id)`: `WHERE workspace_id = ?`

BoardService passes the active view through to the store. This keeps filtering at the data layer where it's efficient.

### 6. Workspace tag on cards in "all" view

**Choice**: When `ViewAll` is active, each `CardData` includes a `WorkspaceName` field. Board renders a small colored tag (workspace name in workspace color) on the card's metadata line. When viewing a specific workspace, the tag is omitted to save space.

### 7. Create overlay defaults to active workspace

**Choice**: The create overlay pre-selects the active workspace. If viewing "All", defaults to no workspace. User can cycle workspace in the create form (similar to column cycling with h/l).

## Risks / Trade-offs

- **Migration on existing data**: All existing tasks get `workspace_id = NULL` (unassigned). Users must manually assign tasks to workspaces. → Acceptable for v1; could add a bulk-assign command later.
- **Synced tasks excluded**: Remote tasks (Jira etc.) won't have workspace assignment. They'll appear in "All" and "Unassigned" views. → Could add workspace mapping per provider project in future.
- **Config drift**: If a workspace is removed from config but tasks reference it, the FK still holds. → Don't delete workspaces from DB when removed from config; only add missing ones. Orphaned workspaces remain accessible.
- **No workspace persistence across sessions**: The active workspace resets to "All" on restart. → Could persist last-used workspace in config or a local pref file in future. Acceptable for v1.
