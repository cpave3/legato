## Why

Legato today stores per-task notes (`legato task note`) but has no way to capture project-level knowledge that transcends a single ticket — architectural decisions, gotchas, conventions, "where does X live" cheat sheets. Users compensate by stuffing this into CLAUDE.md or per-agent system prompts, which doesn't compound across sessions. Worse, external tools (Cursor, Claude Code outside legato, OpenCode) have no way to query legato's knowledge — the data flow today is one-directional (hooks report *to* legato), not bidirectional.

This change adds a local-first knowledge-graph store inside legato, accessible to the user via the TUI/CLI and to agents via an MCP server. Notes link to tasks, link to each other via wikilinks, and stay readable as plain markdown so they're git-committable.

## What Changes

- New `notes` table in SQLite storing markdown notes with titles, bodies, tags, and timestamps. Notes are persisted to `.legato/memory/<slug>.md` files in the project root for git-tracking; SQLite is the index, the markdown files are the source of truth on disk.
- Wikilink syntax (`[[note-title]]` and `[[task:ABC-123]]`) parsed at write time; backlinks computed at read time via SQL.
- Full-text search (SQLite FTS5) over note bodies.
- New MCP server (`legato mcp serve`) exposing both notes and tasks as MCP tools for external coding agents. Tools include: `create_note`, `search_notes`, `get_note`, `find_backlinks`, `list_tasks`, `get_task`, `append_task_note`, `link_note_to_task`.
- TUI: notes view (`N` from board) listing all notes with search; note detail view with Glamour markdown rendering and backlink navigation; `[[wikilink]]` autocomplete in note editor.
- CLI: `legato note new`, `legato note search <query>`, `legato note edit <slug>`, `legato note link <slug> --task <id>`.
- Project-level memory: legato auto-generates a `MEMORY.md` index in `.legato/memory/` listing all notes; commit this directory with the project to share knowledge.

## Capabilities

### New Capabilities

- `knowledge-memory`: notes data model, wikilink parsing, backlink resolution, FTS5 search, markdown-on-disk persistence, project-level memory index.
- `mcp-server`: legato as an MCP server, exposing notes and tasks as tools for external coding agents (stdio + HTTP transports).
- `notes-tui`: notes list overlay, note detail view, wikilink autocomplete in editor.

### Modified Capabilities

- `legato-cli`: `note` subcommand group (`new`, `edit`, `search`, `link`); `mcp serve` subcommand.
- `local-task-management`: task notes (existing `legato task note`) become first-class memory entries — appended to the parent task's note thread, queryable via FTS, linkable from other notes.

## Impact

- **New tables**: `notes` (id, slug, title, body_md, body_html, created_at, updated_at), `note_tags` (note_id, tag), `note_links` (source_note_id, target_kind, target_id) where target_kind is `note|task`. Plus FTS5 virtual table `notes_fts`. Migration `011_notes.sql`.
- **New disk layout**: `.legato/memory/` directory at project root. Markdown files written in lockstep with SQLite. `.legato/memory/MEMORY.md` is an auto-generated index. `.gitignore` is NOT touched — users commit this directory deliberately.
- **New code**:
  - `internal/engine/store/notes.go` — note CRUD, wikilink parsing, backlink queries, FTS indexing.
  - `internal/engine/memory/` — disk-side serialization (markdown read/write), MEMORY.md index generation.
  - `internal/engine/mcp/` — MCP server implementation (JSON-RPC over stdio + HTTP). Uses the same auth-token mechanism as the web server.
  - `internal/service/notes.go` — `NotesService` (CreateNote, UpdateNote, Search, Backlinks, LinkToTask).
  - `internal/tui/notes/` — notes list + detail views.
  - `internal/cli/note.go`, `internal/cli/mcp.go`.
- **Modified**:
  - `cmd/legato/main.go` — wire NotesService, register MCP server boot under `legato mcp serve`.
  - Existing `task note` CLI — appends to a synthetic note for that task ID rather than to the description (BREAKING for the storage location, but the CLI surface is unchanged).
  - `internal/server/` — optional `/api/notes/*` REST endpoints for the web UI (read-only in v1; auth via existing token).
- **New dep**: `github.com/mark3labs/mcp-go` (or equivalent MCP SDK) — small, MIT-licensed, no transitive bloat.
- **Config**: `memory.dir` (default `.legato/memory/`), `memory.mcp.enabled` (bool), `memory.mcp.port` (default `3081`), `memory.mcp.transport` (`stdio|http`, default `stdio`).
- **Compatibility**: Existing task notes auto-migrate on first startup — descriptions matching the task-note format are split into a synthetic note linked to the task.
