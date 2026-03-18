## 1. Jira REST API Client

- [ ] 1.1 Define Jira data types (Issue, Transition, SearchResult, etc.) in `internal/engine/jira/types.go`. Verify: types compile, JSON tags match Jira API v3 response shapes.
- [ ] 1.2 Implement HTTP client struct with Basic Auth, base URL, configurable timeout (30s default), and `Retry-After`-aware exponential backoff (1s initial, 60s cap) in `internal/engine/jira/client.go`. Verify: unit test confirms auth header format, backoff timing, and timeout behavior using httptest server.
- [ ] 1.3 Implement `Search(ctx, jql) ([]Issue, error)` with automatic pagination (page size 50) in `client.go`. Verify: unit test with httptest confirms multi-page fetching, empty results, and JQL error handling.
- [ ] 1.4 Implement `GetIssue(ctx, key) (*Issue, error)` in `client.go`. Verify: unit test confirms full field extraction including ADF description, epic link, and browse URL construction.
- [ ] 1.5 Implement `GetTransitions(ctx, key) ([]Transition, error)` in `client.go`. Verify: unit test confirms transition ID, name, and target status are parsed.
- [ ] 1.6 Implement `DoTransition(ctx, key, transitionID) error` in `client.go`. Verify: unit test confirms correct POST body and handling of 204 success and 400/404 error responses.
- [ ] 1.7 Implement rate limit handling: detect 429 responses, apply exponential backoff, respect `Retry-After` header, reset on success. Verify: unit test with httptest returns 429 then 200, confirms retry delay and backoff reset.

## 2. ADF-to-Markdown Converter

- [ ] 2.1 Implement core ADF tree walker and text node rendering with inline marks (bold, italic, code, strikethrough, underline, link) in `internal/engine/jira/adf.go`. Verify: unit test with ADF JSON fixtures confirms correct Markdown output for each mark type.
- [ ] 2.2 Implement block node conversion: heading (1-6), paragraph, codeBlock (with language), blockquote. Verify: fixture-based unit tests for each block type.
- [ ] 2.3 Implement list conversion: bulletList (`- ` prefix), orderedList (`1. ` prefix), nested lists with correct indentation. Verify: fixture-based unit tests including 2+ levels of nesting.
- [ ] 2.4 Implement table conversion: header row, separator row, data rows with pipe-delimited cells. Verify: fixture-based unit test with multi-column table including formatted cells.
- [ ] 2.5 Implement inline node conversion: mention (`@name`), inlineCard (`[url](url)`), emoji (shortName to Unicode with fallback), status (`[TEXT]`). Verify: unit test for each inline type including unknown emoji fallback.
- [ ] 2.6 Implement panel conversion: info/note/warning/error/success panels as blockquotes with bold type prefix. Verify: unit test for each panel type.
- [ ] 2.7 Implement media conversion: mediaSingle/media with URL as `![](url)`, attachment-by-ID as `[Attachment: filename]` placeholder. Verify: unit test for both URL and attachment-ref cases.
- [ ] 2.8 Implement unknown node fallback: extract text content from unrecognized nodes, never panic. Verify: unit test with fabricated unknown node type confirms graceful text extraction.

## 3. Sync Service — Pull

- [ ] 3.1 Implement pull sync: fetch issues via JQL, upsert into SQLite (insert new, update changed based on `jira_updated_at` comparison), convert ADF descriptions to Markdown on insert/update. Verify: integration test with mock Jira client confirms insert and update paths.
- [ ] 3.2 Implement Jira status-to-column mapping during pull: look up column_mappings table to assign local column on insert. Verify: test confirms tickets are assigned to correct columns based on their Jira status.
- [ ] 3.3 Implement column update on pull when Jira status changed externally and no pending local move exists. Verify: test confirms column updates when Jira status diverges from local column.
- [ ] 3.4 Implement stale ticket detection: mark tickets absent from JQL results as stale, hide after configurable retention period (default 7 days), retain in database. Verify: test confirms stale marking and hiding behavior.

## 4. Sync Service — Push

- [ ] 4.1 Implement async push: on card move, immediately update SQLite, queue Jira transition for background execution. Verify: test confirms SQLite updates before Jira call completes, UI thread is not blocked.
- [ ] 4.2 Implement push success handling: update `jira_status` in SQLite, write success entry to `sync_log`. Verify: test confirms `jira_status` field and sync_log entry after successful transition.
- [ ] 4.3 Implement push failure handling: preserve local column, set warning indicator on card, surface error in status bar, write failure entry to `sync_log`. Verify: test confirms card stays in moved column and warning is set on transition failure.
- [ ] 4.4 Implement retry of failed pushes on manual sync trigger (r key). Verify: test confirms previously failed transitions are retried when sync is manually triggered.

## 5. Sync Service — Conflict Resolution and Scheduling

- [ ] 5.1 Implement local-wins conflict resolution within 5-minute window: track local move timestamps, skip Jira status updates during window, accept Jira status after window expires. Verify: test with moves at various timestamps confirms correct winner selection.
- [ ] 5.2 Implement conflict logging to `sync_log` with local and remote state details. Verify: test confirms sync_log entries contain both local column and Jira status on conflict.
- [ ] 5.3 Implement periodic sync scheduler with configurable interval (default 60s), publishing SyncStarted/SyncCompleted/SyncFailed events on the event bus. Verify: test confirms events are published on each sync cycle.
- [ ] 5.4 Implement offline resilience: detect network failures, load from SQLite, show offline status, retry on next interval. Verify: test confirms board loads from SQLite when Jira is unreachable and recovers when network returns.

## 6. Setup Wizard

- [ ] 6.1 Implement first-run detection: check for config file at `~/.config/legato/config.yaml`, launch wizard if absent, support manual invocation via `legato setup`. Verify: test confirms wizard triggers on missing config and on explicit command.
- [ ] 6.2 Implement Jira connection prompts: collect base URL (validate HTTPS) and email. Verify: test confirms URL validation rejects non-HTTPS URLs.
- [ ] 6.3 Implement API token step: display instructions with Atlassian token URL, prompt for token, validate with test API call, re-prompt on 401. Verify: test confirms validation call is made and re-prompt on failure.
- [ ] 6.4 Implement project key selection: fetch available projects from Jira API, display list, allow user to select one or more. Verify: test confirms projects are listed and selections stored.
- [ ] 6.5 Implement workflow discovery: fetch statuses via `GET /rest/api/3/project/{key}/statuses`, collect union across issue types. Verify: test confirms all statuses from multiple issue types are collected.
- [ ] 6.6 Implement auto-generate column mappings: match discovered statuses to default columns by name heuristics, present for user confirmation, allow manual adjustment. Verify: test confirms standard status names map correctly and unmatched statuses are flagged.
- [ ] 6.7 Implement transition ID discovery: for each column mapping, discover the transition ID needed to move issues into that status. Verify: test confirms transition IDs are recorded for each column.
- [ ] 6.8 Implement config file writing: create `~/.config/legato/` directory if needed, write YAML config with `${LEGATO_JIRA_TOKEN}` env var reference for token, instruct user to set env var. Verify: test confirms written YAML parses correctly and token is env var reference.
