## Context

Legato today has per-task notes (a CLI subcommand that appends timestamped lines to the task description) and per-task descriptions in markdown. Both are scoped to one task. There is no place to record cross-cutting knowledge — architectural decisions, conventions, gotchas, "how does X work" cheat sheets — that ought to outlive any single ticket.

Externally, agents working on legato-tracked projects today have *no* way to read legato state. Hooks flow from agent → legato (status updates), but the reverse (agent queries legato for what it knows) doesn't exist. This means each new Claude Code / Cursor / OpenCode session starts cold even though legato has accumulated months of context.

This design adds two things at once because they are tightly coupled:
1. A note store with markdown-on-disk persistence and a wikilink graph.
2. An MCP server that exposes both the note store and existing task data to external agents.

The two ship together because the note store without the MCP server is just a notes feature; the MCP server without notes would only expose tasks (which would still be useful, but the cross-product is the killer feature).

## Goals / Non-Goals

**Goals:**
- Local-first markdown notes that live next to the project (`.legato/memory/`).
- Wikilink syntax linking notes to other notes and to tasks.
- FTS search over notes via SQLite FTS5 (no external dep).
- Auto-generated `MEMORY.md` index file users can commit.
- MCP server exposing notes and tasks as tools to external agents.
- Bidirectional knowledge: external agents can both read project memory and append to it as they learn.

**Non-Goals:**
- Embedding-based semantic search. v1 uses FTS5 keyword search. Embeddings are a future change once we know what users actually search for.
- Force-directed graph visualization. The data is graph-shaped (note_links table) but we render via lists in v1; a graph viz is a v2 feature.
- Cross-project notes. The memory directory is per-project. Users with multiple projects keep separate memories.
- Conflict resolution for concurrent edits between editors and the TUI. Last-writer-wins on disk, with a warning if mtime moves under us.
- Encryption at rest. Markdown files are plain on disk — same model as the SQLite DB and config.
- Web UI for notes in v1. Read-only `/api/notes/*` endpoints land here; full editing UI is a follow-up.

## Decisions

### 1. Markdown files are the source of truth on disk; SQLite is the index

When the two disagree, the disk file wins. SQLite is rebuilt from disk on startup if it falls out of sync.

**Rationale**: Users will want to edit notes in their own editor (vim, VS Code, Obsidian) outside legato. The notes are markdown, they live in a directory the user owns, they should behave like markdown files. SQLite gives us fast queries (FTS, backlinks) without breaking the "I can `grep` my notes" promise.

**Alternative**: SQLite as source of truth, disk as export. Rejected — would prevent users from editing notes outside legato, which defeats the "git-committable knowledge" point.

**Implementation**: On note CRUD, write the file *first* (atomic via temp+rename), then update SQLite. On startup, walk the memory directory; for each file, compare mtime to DB `updated_at` and reload if newer.

### 2. Wikilink syntax over Markdown links

Use `[[slug]]` (Obsidian/Roam style) instead of `[text](slug.md)` (vanilla markdown).

**Rationale**: Wikilinks are easier to type, render reliably without text-vs-href divergence, and are now standard in personal-knowledge-management tools. Users coming from Obsidian or Logseq will recognize the syntax. We render them to standard markdown links in `body_html` for compatibility.

**Task wikilinks**: `[[task:abc12345]]` is a deliberate variant — the colon namespaces it. We don't conflate notes and tasks because their lookup semantics differ (task IDs are opaque identifiers; note slugs are kebab-case titles).

### 3. SQLite FTS5 over external search engines

FTS5 is built into modernc.org/sqlite and provides bm25-ranked full-text search.

**Rationale**: Zero new dependencies. Performance is fine for thousands of notes. We can swap to embeddings later behind the same `Search` interface.

**Trade-off**: No semantic search ("show me notes about authentication" misses notes that don't contain the word "authentication"). Acceptable for v1; users can always grep.

### 4. MCP server reuses the existing auth token

The HTTP MCP server uses the same bearer token mechanism as `internal/server/`.

**Rationale**: One token to manage. The MCP server *is* a kind of API server; consistency wins. Users pair an external agent the same way they pair the web UI — via QR or manual token paste.

**Stdio transport bypass**: When the parent process spawns legato as `legato mcp serve`, authentication is implicit. This matches how Claude Code spawns MCP servers today.

### 5. MCP tools wrap services, not stores

`create_note` calls `NotesService.CreateNote`, not the underlying store. Same for tasks — `list_tasks` calls `BoardService.ListCardsByWorkspace`.

**Rationale**: Service layer enforces invariants (validation, event publishing, file-on-disk sync). Bypassing it from MCP would create a parallel write path that drifts.

### 6. Two transports: stdio and HTTP

Stdio for child-process integrations (the canonical MCP shape — Claude Code spawns the server). HTTP for daemon-mode (the legato TUI runs an HTTP MCP server alongside the web server, accessible from external machines).

**Rationale**: Both are first-class MCP transports. Stdio is the reference; HTTP is what makes "external agent on another machine reads my project memory" work. Cost: small — same JSON-RPC handler, just two listeners.

### 7. Note slugs are derived from titles, deduplicated with `-N`

Title "Authentication Overview" → slug `authentication-overview`. On collision: `-2`, `-3`.

**Rationale**: Slugs as filenames need to be filesystem-safe. Deriving from title gives URLs and `MEMORY.md` entries that are readable. Manual override allowed via a `slug:` frontmatter field if users want to rename without breaking links (which we resolve by slug, not title).

### 8. Task notes migrate from descriptions into the memory system

Existing `legato task note` writes timestamped lines into the task description. After this change, the same CLI invocation creates/appends to a synthetic note (`task-<id>-notes`) linked to the task.

**Rationale**: Once we have a notes system, scattering "task notes" inside descriptions is awkward. They are notes; treat them as notes. The CLI surface stays the same — the storage location moves. Migration parses the legacy format on first startup and extracts the timestamped lines.

**Trade-off**: A one-time migration with potential edge cases (free-text descriptions that happen to match the timestamp regex). Mitigation: idempotent marker per task; users can revert by editing the description.

### 9. MEMORY.md is auto-generated; users commit it

The index file lives in `.legato/memory/MEMORY.md` and is regenerated whenever a note changes.

**Rationale**: Mirrors the user-level `/home/cameron/.claude/.../MEMORY.md` pattern from this CLAUDE setup — an index file that's the entry point for any reader (human or AI) into the knowledge graph. Committing it gives reviewers a quick TOC of the project's accumulated context.

**Sentinel**: `<!-- AUTO-GENERATED -->` on the second line. If users delete it, we stop overwriting. We do not silently clobber edits.

## Risks / Trade-offs

- **Risk**: Disk and DB drift if a user edits a note while legato is running. → **Mitigation**: A file watcher (`fsnotify`) on the memory directory triggers reload of changed files. Where unavailable, mtime check on every `GetNote` triggers a reload. Reasonable for v1.
- **Risk**: Wikilink resolution lag — if note A links to note B before B exists, the link is "dangling." → **Mitigation**: Persist dangling links with the unresolved slug; resolve at query time. When B is created, backlink queries on B will find A automatically (via the slug).
- **Risk**: MCP server exposes tasks publicly when run with HTTP transport. → **Mitigation**: Auth token required. Same model as web UI; users already understand the threat surface. Add a clear warning in `legato mcp serve --help` about exposure.
- **Risk**: Task-note migration extracts content that wasn't a note (false positive). → **Mitigation**: Migration is opt-in via config (`memory.migrate_task_notes: true`, default `false` for v1) and idempotent. Users dry-run with `legato note migrate-tasks --dry-run`.
- **Risk**: Adding `mcp-go` introduces a new dep with its own update cadence. → **Mitigation**: The MCP protocol is small and stable enough that we could vendor or hand-write JSON-RPC handlers if the dep becomes a problem. Start with the dep; revisit if it bloats.
- **Trade-off**: SQLite FTS5 is per-database; cross-project search is impossible. Acceptable — search is per-project memory by design.
- **Trade-off**: Markdown frontmatter ties note storage to YAML. We could use HTML comments instead. YAML is conventional in markdown ecosystems (Obsidian, Hugo, Jekyll); we follow the convention.

## Migration Plan

1. Apply migration `011_notes.sql` (creates `notes`, `note_tags`, `note_links`, `notes_fts`).
2. Existing data unaffected. Memory directory created lazily on first note write.
3. Existing `legato task note` invocations begin writing to the new synthetic-note storage. Old descriptions remain unchanged unless `memory.migrate_task_notes: true`.
4. If migrating, first startup with the flag scans tasks for timestamp-formatted lines, extracts them into synthetic notes, and removes from descriptions. Per-task migration marker prevents re-running.
5. Rollback: drop the new tables. Existing tasks unchanged. The `.legato/memory/` directory remains on disk (users can delete manually). Synthetic task-notes are lost — acceptable for an opt-in feature.

## Open Questions

- Should `MEMORY.md` include workspace tags or limit to slug+title? → **Tentative**: slug+title+first-sentence is enough; tags clutter the index. Revisit after dogfooding.
- Should the MCP server expose `agent_sessions` (let external tools see what's running)? → **Defer**. Useful but increases surface; add in a follow-up if asked.
- Do we want a `.legato/memory/.gitignore` to suggest what to commit? → **No**. The point is users commit the directory. We document the convention in the README; no enforcement.
- Should wikilinks support aliases (`[[slug|display text]]`)? → **Yes**, mirror Obsidian. Implement in the parser; rendering uses the alias as link text.
