## Context

Local tasks currently have two limitations: (1) the create overlay treats space as a regular key event but the input handler only accepts `KeyRunes` with single runes — space works but titles feel limited without a description field, and (2) there's no way to set or edit a description after creation. Remote/synced tickets get descriptions from Jira, but local tasks always have empty `description`/`description_md` fields.

The store already has `description` and `description_md` columns on the `tasks` table, and `UpdateTask` persists both. No schema migration is needed.

## Goals / Non-Goals

**Goals:**
- Allow natural multi-word titles in the create overlay
- Add an optional description field to the create overlay
- Enable editing descriptions of local tasks from the detail view via `$EDITOR`
- Add an `editor` config field as an override

**Non-Goals:**
- Inline TUI text editing for descriptions (too complex for multi-line; delegate to terminal editor)
- Editing titles after creation (separate future change)
- Editing descriptions of remote/synced tickets (would need push-back to provider)

## Decisions

### 1. Create overlay: add description input with tab-cycling focus

The create overlay currently has one text input (title) and uses `tab` to cycle columns, `ctrl+p` for priority. Adding a description field means we need a focus model.

**Decision**: Add a `focusField` enum (`focusTitle`, `focusDescription`) to the create overlay. `tab` cycles: title → column → description → title. When focused on description, typing appends to description. The description input shows a multi-line area with a visible cursor.

**Alternative considered**: Using Bubble's `textarea` component. Rejected because it adds a dependency and the description here is just initial text — real editing happens in `$EDITOR`.

### 2. Detail view: `e` key opens `$EDITOR` via `tea.ExecProcess`

**Decision**: Press `e` in the detail view when viewing a local task (provider == nil). Write current `description_md` to a temp file, open `$EDITOR` (falling back to `$VISUAL`, then `vi`). On exit, read the temp file, call `BoardService.UpdateTaskDescription(ctx, id, content)`. The service sets both `description` (plain text, same content) and `description_md` (same content — local tasks use markdown as source of truth). Re-render the detail view.

This mirrors the agent attach pattern: `tea.ExecProcess` suspends bubbletea, runs the editor, resumes on exit.

**Alternative considered**: In-TUI textarea editing. Rejected — terminal editors are the right tool for multi-line markdown editing, and the pattern already exists for tmux attach.

### 3. Editor resolution order

**Decision**: Config `editor` field → `$VISUAL` → `$EDITOR` → `vi`. This follows Unix convention where `$VISUAL` is for full-screen editors and `$EDITOR` is for line editors, with `vi` as the POSIX fallback.

The resolution lives in the config package as `ResolveEditor()` so it's testable and reusable.

### 4. Service layer: `UpdateTaskDescription` method

**Decision**: Add `UpdateTaskDescription(ctx, id, description string) error` to `BoardService`. It loads the task, verifies it's a local task (provider == nil), sets `description` and `description_md`, calls `store.UpdateTask`, and publishes `EventCardsRefreshed`.

**Alternative considered**: Reusing `UpdateTask` directly from TUI. Rejected — the service layer should own validation (local-only check) and event publishing.

### 5. Create overlay: `CreateTask` signature change

**Decision**: Change `BoardService.CreateTask` from `(ctx, title, column, priority)` to `(ctx, title, description, column, priority)`. The `CreateTaskMsg` gains a `Description` field. Empty description is the common case and works fine.

This is a breaking interface change but all implementors are in-tree (production + test fakes).

## Risks / Trade-offs

- **Editor process crashes**: If the editor exits non-zero, discard changes and show feedback message. The temp file is cleaned up in a deferred call. → Low risk, standard pattern.
- **Large descriptions**: No size limit enforced. The viewport in the detail view already handles scrolling. → Acceptable.
- **Interface change blast radius**: `CreateTask` signature changes, requiring updates to all `BoardService` implementations (production + 4 test fakes). → Manageable, all in-tree.
