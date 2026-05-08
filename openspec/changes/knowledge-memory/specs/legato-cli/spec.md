## ADDED Requirements

### Requirement: note new subcommand

The system SHALL provide `legato note new [title]` that creates a new note and opens it in `$EDITOR` for body entry.

#### Scenario: Create with title

- **WHEN** `legato note new "Architecture Overview"` is executed
- **THEN** the system SHALL create the note record, write `.legato/memory/architecture-overview.md` with frontmatter and an empty body, and open the file in `$EDITOR`

#### Scenario: Create without title

- **WHEN** `legato note new` is executed without a title argument
- **THEN** the system SHALL prompt for a title via stdin (or fail with code 1 if stdin is not a TTY)

### Requirement: note search subcommand

The system SHALL provide `legato note search <query>` that prints matching notes to stdout.

#### Scenario: Search with results

- **WHEN** `legato note search "rate limit"` is executed and matching notes exist
- **THEN** stdout SHALL contain one line per match in the format `<slug>\t<title>\t<snippet>`

#### Scenario: No results

- **WHEN** the query matches no notes
- **THEN** the command SHALL exit with code 0 and print nothing to stdout

#### Scenario: JSON output

- **WHEN** `--json` flag is passed
- **THEN** stdout SHALL contain a JSON array of matching notes

### Requirement: note edit subcommand

The system SHALL provide `legato note edit <slug>` that opens the note's markdown file in `$EDITOR`.

#### Scenario: Edit existing note

- **WHEN** `legato note edit architecture-overview` is executed and the note exists
- **THEN** the file SHALL be opened in `$EDITOR`; on save, the DB row SHALL be updated from the file content

#### Scenario: Edit nonexistent note

- **WHEN** the slug does not exist
- **THEN** the command SHALL exit with code 1 and an error message

### Requirement: note link subcommand

The system SHALL provide `legato note link <slug> --task <task-id>` that creates a link from a note to a task.

#### Scenario: Link note to task

- **WHEN** `legato note link arch-overview --task abc12345` is executed
- **THEN** a `note_links` row SHALL be inserted (`source_note_id` from slug, `target_kind='task'`, `target_id='abc12345'`) and IPC SHALL broadcast a memory refresh

#### Scenario: Link with note target

- **WHEN** `legato note link arch-overview --note auth-design` is executed
- **THEN** a `note_links` row SHALL be inserted with `target_kind='note'` and `target_id='auth-design'`

### Requirement: mcp serve subcommand

The system SHALL provide `legato mcp serve [--transport stdio|http] [--port <port>] [--read-only]` that starts the MCP server.

#### Scenario: Default stdio

- **WHEN** `legato mcp serve` is executed without flags
- **THEN** the server SHALL run with stdio transport until stdin closes

#### Scenario: HTTP server

- **WHEN** `legato mcp serve --transport http --port 3081` is executed
- **THEN** the server SHALL listen on port 3081 and require bearer auth on each request

#### Scenario: Read-only

- **WHEN** `--read-only` is passed
- **THEN** write tools SHALL return errors but read tools SHALL function
