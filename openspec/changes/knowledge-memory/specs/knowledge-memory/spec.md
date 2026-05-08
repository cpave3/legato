## ADDED Requirements

### Requirement: Notes data model

The system SHALL persist notes in a `notes` table with full markdown bodies and link them to other notes and tasks via dedicated tables.

#### Scenario: Notes schema

- **WHEN** the database is migrated
- **THEN** a `notes` table SHALL exist with columns `id` (INTEGER PK), `slug` (TEXT UNIQUE, kebab-case), `title` (TEXT), `body_md` (TEXT), `body_html` (TEXT, rendered cache), `created_at` (DATETIME), `updated_at` (DATETIME)
- **AND** `note_tags` (note_id, tag) and `note_links` (source_note_id, target_kind ['note'|'task'], target_id) tables SHALL exist with appropriate indexes
- **AND** an FTS5 virtual table `notes_fts(title, body)` SHALL exist mirroring `notes`

#### Scenario: Slug uniqueness

- **WHEN** a note is created with a title that produces a slug colliding with an existing note
- **THEN** the system SHALL append `-2`, `-3`, etc. until unique

### Requirement: Markdown-on-disk persistence

Each note SHALL be persisted as a markdown file at `<project>/.legato/memory/<slug>.md` with frontmatter (title, created_at, tags) followed by the body. The SQLite row is the index; the markdown file is the source of truth on disk.

#### Scenario: Create note writes file

- **WHEN** `NotesService.CreateNote(title, body, tags)` is called
- **THEN** the system SHALL insert a `notes` row AND write `<project>/.legato/memory/<slug>.md` containing YAML frontmatter and the body
- **AND** if the file write fails, the database transaction SHALL roll back

#### Scenario: Update note rewrites file

- **WHEN** a note is updated
- **THEN** both the database row and the on-disk file SHALL be updated atomically (file written first to a temp path, then renamed)

#### Scenario: Delete note removes file

- **WHEN** a note is deleted
- **THEN** the database row SHALL be removed AND the on-disk file SHALL be deleted

#### Scenario: Disk reconciliation on startup

- **WHEN** legato starts and `.legato/memory/` contains markdown files
- **THEN** the system SHALL reconcile DB ↔ disk: files newer than DB rows reload into the DB; files missing from DB are imported; DB rows missing files are flagged but not deleted (await user resolution)

### Requirement: Wikilink parsing

Note bodies SHALL support `[[note-title]]` syntax for note-to-note links and `[[task:ABC-123]]` syntax for note-to-task links. Links SHALL be parsed at write time and persisted in `note_links`.

#### Scenario: Parse note wikilink

- **WHEN** a note body contains `[[architecture-overview]]`
- **THEN** a `note_links` row SHALL be inserted with `target_kind='note'` and `target_id` resolving to the slug `architecture-overview`

#### Scenario: Parse task wikilink

- **WHEN** a note body contains `[[task:abc12345]]`
- **THEN** a `note_links` row SHALL be inserted with `target_kind='task'` and `target_id='abc12345'`

#### Scenario: Unresolved wikilink

- **WHEN** a note body contains `[[no-such-note]]` and no note with that slug exists
- **THEN** the link SHALL still be persisted with `target_id` set to the unresolved slug; `find_backlinks` queries on the eventual note will resolve them when it is created

### Requirement: Backlinks query

The system SHALL provide a `Backlinks(targetKind, targetID) []NoteRef` operation returning notes that link to the given target.

#### Scenario: Backlinks for a note

- **WHEN** `Backlinks("note", "architecture-overview")` is called
- **THEN** it SHALL return all notes whose `note_links.target_id` matches and `target_kind='note'`, ordered by `updated_at DESC`

#### Scenario: Backlinks for a task

- **WHEN** `Backlinks("task", "abc12345")` is called
- **THEN** it SHALL return all notes that reference the task

#### Scenario: No backlinks

- **WHEN** the target has no incoming links
- **THEN** the call SHALL return an empty slice (not an error)

### Requirement: Full-text search

The system SHALL provide a `Search(query string) []NoteRef` operation backed by SQLite FTS5 over note titles and bodies.

#### Scenario: Match in title

- **WHEN** `Search("auth")` is called and a note titled "Authentication overview" exists
- **THEN** it SHALL be returned with a higher rank than notes that only mention "auth" in the body

#### Scenario: Match in body

- **WHEN** `Search("rate limit")` is called
- **THEN** notes whose body contains the phrase SHALL be returned ordered by FTS rank

#### Scenario: Empty query

- **WHEN** `Search("")` is called
- **THEN** the call SHALL return all notes ordered by `updated_at DESC` (no FTS applied)

### Requirement: MEMORY.md index generation

The system SHALL maintain `<project>/.legato/memory/MEMORY.md` as an auto-generated index listing all notes with one line per note.

#### Scenario: Index update on note change

- **WHEN** a note is created, updated, or deleted
- **THEN** `MEMORY.md` SHALL be regenerated containing one line per note: `- [Title](slug.md) — first sentence of body`

#### Scenario: Index format

- **WHEN** `MEMORY.md` is generated
- **THEN** it SHALL begin with a `# Memory Index` header and an `<!-- AUTO-GENERATED -->` marker line so users do not edit it manually

#### Scenario: User-edited MEMORY.md

- **WHEN** the auto-generated marker is missing from an existing `MEMORY.md`
- **THEN** the system SHALL not overwrite the file and SHALL log a warning

### Requirement: Project memory directory

The default location for the memory directory SHALL be `<project>/.legato/memory/` (relative to the legato working directory) and SHALL be configurable via `memory.dir`.

#### Scenario: Default location

- **WHEN** legato runs in `~/code/myproject/`
- **THEN** notes SHALL be written to `~/code/myproject/.legato/memory/`

#### Scenario: Configured location

- **WHEN** `memory.dir: ~/notes/myproject` is in config
- **THEN** notes SHALL be written to that path (with `~` expanded)

#### Scenario: Directory creation

- **WHEN** the memory directory does not exist on first note write
- **THEN** the system SHALL create it (and `.legato/`) with mode 0755
