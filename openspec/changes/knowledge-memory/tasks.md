## 1. Database & Schema

- [ ] 1.1 Add migration `internal/engine/store/migrations/011_notes.sql` creating `notes`, `note_tags`, `note_links` tables and FTS5 virtual table `notes_fts` per spec
- [ ] 1.2 Add `Note`, `NoteRef`, `NoteLink` structs in `internal/engine/store/notes.go`
- [ ] 1.3 Implement CRUD: `CreateNote`, `GetNote`, `GetNoteBySlug`, `UpdateNote`, `DeleteNote`, `ListNotes(orderBy)`, `SearchNotes(query)`
- [ ] 1.4 Implement link CRUD: `UpsertNoteLinks(noteID, links)`, `Backlinks(targetKind, targetID)`, `OutgoingLinks(noteID)`
- [ ] 1.5 FTS5 triggers to keep `notes_fts` in sync on insert/update/delete

## 2. Wikilink Parser

- [ ] 2.1 Create `internal/engine/memory/wikilink.go` — `ParseWikilinks(body string) []Wikilink` returning `{Kind: note|task, Target: slug, Alias: ""}` for each `[[...]]`
- [ ] 2.2 Support `[[task:abc12345]]` namespaced syntax
- [ ] 2.3 Support `[[slug|alias]]` aliasing
- [ ] 2.4 Render wikilinks to standard markdown `[alias](slug.md)` for `body_html` cache (used by Glamour)
- [ ] 2.5 Unit tests covering all syntax variants, escaping, code fences (don't parse links inside fenced code)

## 3. Markdown-on-Disk Persistence

- [ ] 3.1 Create `internal/engine/memory/disk.go` — `WriteNote(dir, note)`, `ReadNote(path)`, `DeleteNote(dir, slug)`, `ListNoteFiles(dir)`
- [ ] 3.2 Frontmatter format: YAML between `---` markers — `title`, `created_at`, `tags`, optional `slug`
- [ ] 3.3 Atomic writes: temp file + rename
- [ ] 3.4 `Reconcile(dir, store)` — walks directory, loads files newer than DB rows, imports new files, flags missing-on-disk in DB
- [ ] 3.5 Optional `fsnotify` watcher to live-reload on external edits
- [ ] 3.6 Unit tests using `t.TempDir()` for read/write/reconcile

## 4. MEMORY.md Index

- [ ] 4.1 Create `internal/engine/memory/index.go` — `GenerateIndex(notes []Note) string` producing markdown with sentinel
- [ ] 4.2 `WriteIndex(dir)` writes `MEMORY.md` if sentinel present or file absent; logs warning if user-edited
- [ ] 4.3 Hook index regeneration into `NotesService` create/update/delete

## 5. Notes Service

- [ ] 5.1 Create `internal/service/notes.go` with `NotesService` interface + concrete impl
- [ ] 5.2 Methods: `CreateNote(title, body, tags)`, `UpdateNote(id, body)`, `DeleteNote(id)`, `GetNote(slug)`, `Search(query)`, `Backlinks(kind, id)`, `LinkToTask(noteID, taskID)`
- [ ] 5.3 Slug generation from title with collision suffixes
- [ ] 5.4 Wikilink extraction on every write — refresh `note_links` rows in single tx
- [ ] 5.5 Publish `EventNoteCreated`/`EventNoteUpdated`/`EventNoteDeleted` events
- [ ] 5.6 Service tests with real SQLite (`t.TempDir()`) verifying CRUD + linking + FTS

## 6. Task Note Migration

- [ ] 6.1 Detection: regex `^\[\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}\] (.+)$` per description line
- [ ] 6.2 Implement `MigrateTaskNotes(ctx) (migrated int, err error)` extracting matched lines into a synthetic note `task-<id>-notes`, removing them from descriptions
- [ ] 6.3 Idempotency marker: `tasks.notes_migrated` boolean column (migration `012_notes_migration_marker.sql`)
- [ ] 6.4 Config gate: `memory.migrate_task_notes` (bool, default false)
- [ ] 6.5 CLI: `legato note migrate-tasks [--dry-run]`
- [ ] 6.6 Update `legato task note` CLI to write into the synthetic note instead of appending to description
- [ ] 6.7 Unit tests: legacy format → synthetic note + link; mixed content preserved; idempotent re-run

## 7. MCP Server

- [ ] 7.1 Add `github.com/mark3labs/mcp-go` dependency (or equivalent — evaluate at impl time)
- [ ] 7.2 Create `internal/engine/mcp/server.go` with stdio + HTTP transport implementations
- [ ] 7.3 Register note tools: `create_note`, `update_note`, `get_note`, `search_notes`, `list_notes`, `find_backlinks`, `delete_note`
- [ ] 7.4 Register task tools: `list_tasks`, `get_task`, `create_task`, `update_task_status`, `append_task_note`, `link_note_to_task`
- [ ] 7.5 JSON Schema input definitions for each tool
- [ ] 7.6 HTTP transport: bearer token middleware reusing `auth.ReadToken`; respond 401 on invalid token
- [ ] 7.7 Stdio transport: no auth; read newline-delimited JSON-RPC from stdin, write to stdout
- [ ] 7.8 `--read-only` flag disables write tools (returns error from handler)
- [ ] 7.9 Tool handlers wrap services (`NotesService`, `BoardService`), not stores
- [ ] 7.10 Integration tests using `httptest.NewRecorder` for HTTP and pipe-driven for stdio

## 8. CLI Subcommands

- [ ] 8.1 Create `internal/cli/note.go` with `New`, `Edit`, `Search`, `Link`, `MigrateTasks` handlers
- [ ] 8.2 Wire `note` subcommand group in `cmd/legato/main.go`
- [ ] 8.3 `note new [title]` — create note, open `$EDITOR` for body, save
- [ ] 8.4 `note edit <slug>` — open file in `$EDITOR`, reload on save
- [ ] 8.5 `note search <query> [--json]` — text or JSON output
- [ ] 8.6 `note link <slug> --task <id>` and `note link <slug> --note <slug>`
- [ ] 8.7 Create `internal/cli/mcp.go` with `Serve` handler
- [ ] 8.8 Wire `mcp serve [--transport stdio|http] [--port] [--read-only]`
- [ ] 8.9 IPC broadcast `memory_changed` on note CRUD; CLI broadcasts to running TUI instances

## 9. TUI Notes List View

- [ ] 9.1 Create `internal/tui/notes/list.go` — full-screen view with note list, status bar, key hints
- [ ] 9.2 Wire `N` keybinding on board to switch to `viewNotes` enum
- [ ] 9.3 Implement search via `/` with real-time FTS calls via `NotesService.Search`
- [ ] 9.4 `n` opens new-note overlay (title input → `$EDITOR` for body)

## 10. TUI Note Detail View

- [ ] 10.1 Create `internal/tui/notes/detail.go` — Glamour-rendered body, metadata header, backlinks section, outgoing-links section
- [ ] 10.2 `e` opens `$EDITOR` on the note's markdown file via `tea.ExecProcess`; reload DB on close
- [ ] 10.3 Navigation in backlinks/outgoing sections: `j`/`k` highlight, `enter` follows link (note → note view; task → task detail view)

## 11. TUI Wikilink Autocomplete

- [ ] 11.1 Create `internal/tui/notes/autocomplete.go` — popup model with prefix-filtered list of notes + tasks
- [ ] 11.2 Hook into description text inputs (task description editor and note editor) — trigger on `[[`
- [ ] 11.3 Filter as user continues typing; insert wikilink on `enter`; cancel on `esc`

## 12. Server Endpoints (Optional in v1)

- [ ] 12.1 Add `GET /api/notes` (list) and `GET /api/notes/:slug` (detail) to `internal/server/`
- [ ] 12.2 Reuse existing bearer auth middleware
- [ ] 12.3 Read-only in v1 (no POST/PUT)

## 13. Configuration

- [ ] 13.1 Add `memory.dir` (string, default `.legato/memory/`) to config struct
- [ ] 13.2 Add `memory.mcp.enabled` (bool, default false), `memory.mcp.port` (string, default `3081`), `memory.mcp.transport` (`stdio|http`)
- [ ] 13.3 Add `memory.mcp.read_only` (bool, default false)
- [ ] 13.4 Add `memory.migrate_task_notes` (bool, default false)
- [ ] 13.5 Auto-start MCP server alongside TUI when `memory.mcp.enabled: true`
- [ ] 13.6 Status bar indicator `MCP :<port>` when MCP HTTP server is running

## 14. Wiring

- [ ] 14.1 Initialize `NotesService` in `cmd/legato/main.go` after store, before TUI/server
- [ ] 14.2 Reconcile `.legato/memory/` on startup
- [ ] 14.3 Pass `NotesService` to `NewApp` and to MCP server
- [ ] 14.4 Wire IPC `memory_changed` listener on event bus → republish as `EventCardsRefreshed` (notes view subscribes to NotesService events directly)

## 15. Documentation

- [ ] 15.1 Create `docs/ai/memory.md` describing the notes data model, wikilink syntax, MCP tools
- [ ] 15.2 Update `docs/ai/packages.md` with `internal/engine/memory/`, `internal/engine/mcp/`, `internal/service/notes.go`
- [ ] 15.3 Update `docs/ai/database.md` with the new tables
- [ ] 15.4 Update `docs/ai/cli.md` with new subcommands
- [ ] 15.5 Add `@docs/ai/memory.md` reference to project CLAUDE.md
- [ ] 15.6 Update help overlay with new keybindings (`N`, autocomplete, etc.)
