## ADDED Requirements

### Requirement: MCP server lifecycle

The system SHALL expose an MCP (Model Context Protocol) server via `legato mcp serve` that allows external coding agents to read and write legato notes and tasks.

#### Scenario: Stdio transport

- **WHEN** `legato mcp serve` is executed
- **THEN** the server SHALL communicate over stdio (newline-delimited JSON-RPC) and remain alive until stdin closes

#### Scenario: HTTP transport

- **WHEN** `legato mcp serve --transport http --port 3081` is executed
- **THEN** the server SHALL listen on the given port and accept JSON-RPC requests at `POST /mcp`

#### Scenario: Auto-start with TUI

- **WHEN** `memory.mcp.enabled: true` is in config and the TUI starts
- **THEN** the MCP server SHALL start in a background goroutine using the configured transport (HTTP only — stdio is single-client)

### Requirement: MCP authentication

When using HTTP transport, the MCP server SHALL require the same bearer token as the web server.

#### Scenario: Authenticated request

- **WHEN** an HTTP MCP request includes `Authorization: Bearer <token>` matching the server's token
- **THEN** the request SHALL be processed

#### Scenario: Missing or invalid token

- **WHEN** an HTTP MCP request omits the Authorization header or provides an invalid token
- **THEN** the server SHALL respond with HTTP 401

#### Scenario: Stdio transport bypasses auth

- **WHEN** the MCP server is started in stdio mode
- **THEN** no token is required (the parent process implicitly authenticates by spawning legato)

### Requirement: Note tools

The MCP server SHALL expose the following tools related to notes: `create_note`, `update_note`, `get_note`, `search_notes`, `list_notes`, `find_backlinks`, `delete_note`.

#### Scenario: create_note tool

- **WHEN** `create_note` is invoked with `{title, body, tags}`
- **THEN** the server SHALL create a note via `NotesService.CreateNote` and return the new note's slug and id

#### Scenario: search_notes tool

- **WHEN** `search_notes` is invoked with `{query}`
- **THEN** the server SHALL return matching notes with `{slug, title, snippet, updated_at}` for each

#### Scenario: find_backlinks tool

- **WHEN** `find_backlinks` is invoked with `{kind, id}` where kind is `note` or `task`
- **THEN** the server SHALL return all notes linking to the target

#### Scenario: get_note tool

- **WHEN** `get_note` is invoked with `{slug}`
- **THEN** the server SHALL return the full note (title, body_md, tags, links, backlinks)

### Requirement: Task tools

The MCP server SHALL expose the following tools related to tasks: `list_tasks`, `get_task`, `create_task`, `update_task_status`, `append_task_note`, `link_note_to_task`.

#### Scenario: list_tasks tool

- **WHEN** `list_tasks` is invoked with optional `{status, workspace}`
- **THEN** the server SHALL return tasks matching the filter via `BoardService` queries

#### Scenario: get_task tool

- **WHEN** `get_task` is invoked with `{id}`
- **THEN** the server SHALL return the task including description, status, workspace, pr_meta, and remote_meta

#### Scenario: append_task_note tool

- **WHEN** `append_task_note` is invoked with `{task_id, message}`
- **THEN** the server SHALL append a timestamped note to the task's note thread (same mechanism as `legato task note` CLI)

#### Scenario: link_note_to_task tool

- **WHEN** `link_note_to_task` is invoked with `{note_slug, task_id}`
- **THEN** the server SHALL insert a `note_links` row with `target_kind='task'` and `target_id=task_id`

### Requirement: Tool discovery

The MCP server SHALL respond to the standard MCP `tools/list` request with the full set of tools and their JSON schemas.

#### Scenario: tools/list response

- **WHEN** an MCP client sends `tools/list`
- **THEN** the server SHALL return all registered tools with name, description, and `inputSchema` (JSON Schema)

### Requirement: Read-only mode

The MCP server SHALL support a `--read-only` flag (and config `memory.mcp.read_only`) that disables write tools.

#### Scenario: Read-only blocks writes

- **WHEN** the server runs with `--read-only` and a client invokes `create_note`
- **THEN** the server SHALL return an error indicating the server is read-only
- **AND** read tools (`get_note`, `search_notes`, `list_tasks`, etc.) SHALL still function
