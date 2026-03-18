## Why

Jira integration is the core value proposition of Legato v0 — without it, the kanban board is just a static display. Users need their assigned tickets pulled from Jira automatically, displayed on the board, and card movements synced back. This phase replaces the stub SyncService with a real Jira-backed implementation and adds the ADF→Markdown converter for rendering ticket descriptions.

## What Changes

- Build a Jira REST API v3 client: search (JQL), get issue, list transitions, execute transition
- Implement ADF (Atlassian Document Format) → Markdown converter for ticket descriptions
- Replace the stub SyncService with a real implementation: pull sync (Jira→SQLite), push sync (card move→Jira transition)
- Implement the sync algorithm: periodic pull, async push, conflict resolution (local wins within 5-minute window)
- Add rate limiting handling (exponential backoff on 429)
- Build a first-run setup wizard: prompt for Jira credentials, discover project workflows, auto-generate column mappings
- Handle error scenarios: offline mode, auth failures, transition failures

## Capabilities

### New Capabilities
- `jira-client`: Jira REST API v3 HTTP client with authentication, search, issue detail, and transition operations
- `adf-to-markdown`: Standalone ADF→Markdown converter handling all common ADF node types (headings, lists, code blocks, tables, mentions, panels)
- `jira-sync`: Real SyncService implementation with bidirectional sync, conflict resolution, and async push
- `setup-wizard`: First-run interactive setup for Jira credentials, project discovery, and automatic column mapping generation

### Modified Capabilities
<!-- None — replaces the stub, doesn't modify its interface -->

## Impact

- New package: `internal/engine/jira/`
- Modifies `internal/service/sync.go` to use real Jira client instead of stub
- Requires network access to Jira REST API
- Requires user to have a Jira API token
- Adds rate limiting and retry logic
