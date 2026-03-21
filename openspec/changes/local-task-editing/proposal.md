## Why

Local tasks are second-class citizens compared to synced Jira tickets: the create overlay doesn't allow spaces in titles (each keypress is treated as a potential shortcut) and there's no way to set or edit a description. This makes local tasks useful only as one-word placeholders, defeating the purpose of a local-first task board.

## What Changes

- **Fix title input in create overlay**: allow spaces (and any printable character) so titles read naturally (e.g. "Refactor auth middleware")
- **Add description field to create overlay**: optional multi-line body when creating a task
- **Add description editing from detail view**: press `e` in the detail view of a local task to open `$EDITOR` (or `$VISUAL`) with the current description, save on exit
- **Add `editor` config field**: optional override in `config.yaml` for users who want a different editor than their shell default

## Capabilities

### New Capabilities
- `local-task-editing`: Covers improved title input, description on create, and editor-based description editing from the detail view

### Modified Capabilities
- `task-creation-overlay`: Title input must accept spaces; new description field added
- `detail-view`: New `e` keybinding to edit description via external editor (local tasks only)

## Impact

- **TUI overlay** (`internal/tui/overlay/create.go`): input handling changes, new description field
- **TUI detail** (`internal/tui/detail/model.go`): new `e` key handler, `tea.ExecProcess` for editor
- **Service layer** (`internal/service/board.go`): `CreateTask` gains a `description` parameter; new `UpdateTaskDescription` method
- **Service interface** (`internal/service/interfaces.go`): `BoardService.CreateTask` signature change, new method
- **Config** (`config/config.go`): optional `editor` field
- **Store**: no schema changes — `description` and `description_md` columns already exist
