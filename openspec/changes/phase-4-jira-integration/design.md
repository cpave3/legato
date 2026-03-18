## Context

Jira integration is the core value proposition of Legato v0. Without it, the kanban board is a static display with stub data. This phase replaces the stub SyncService with a real Jira-backed implementation, adds the ADF-to-Markdown converter for rendering ticket descriptions, and provides a first-run setup wizard so users do not have to manually discover Jira transition IDs and status mappings.

The Jira REST API v3 is the integration target. Users authenticate with email + API token (Atlassian Cloud standard). All Jira communication is async and non-blocking so the TUI remains responsive.

---

## Goals

- Build a Jira REST API v3 HTTP client supporting search (JQL), get issue, list transitions, and execute transition operations.
- Implement an ADF (Atlassian Document Format) to Markdown converter as a standalone, independently testable package.
- Replace the stub SyncService with bidirectional sync: pull (Jira to SQLite) and push (card move to Jira transition).
- Implement conflict resolution with local-wins semantics within a 5-minute window.
- Build a first-run setup wizard that discovers project workflows and auto-generates column mappings.
- Handle rate limiting, auth failures, and offline scenarios gracefully.

## Non-Goals

- Webhook-based real-time sync (polling is sufficient for v0).
- Multi-user or team-level Jira operations.
- Jira write operations beyond status transitions (no editing summaries, descriptions, or fields).
- OAuth2 authentication flow (API token only for v0).
- Markdown-to-ADF conversion (read-only descriptions for v0).

---

## Decisions

### Direct HTTP client, no SDK

Use Go's `net/http` client to call Jira REST API v3 directly rather than adopting a third-party Jira SDK. The API surface needed is small (5 endpoints), and a direct client avoids pulling in a large dependency with its own opinions on error handling and data types. The client lives in `internal/engine/jira/client.go`.

### ADF converter as standalone package

The ADF-to-Markdown converter is implemented as a pure function package in `internal/engine/jira/adf.go` with no dependencies on the Jira client, database, or any other Legato package. This makes it independently testable with fixture-based tests and potentially extractable as a shared library later.

### Async push with local-wins conflict resolution

When a user moves a card, the local SQLite state updates immediately and the Jira transition fires asynchronously. If the transition fails, the card stays in the local column with a warning indicator. On conflict (both Jira and local changed), local wins within a 5-minute window from the local move. After that window, the next pull sync accepts the Jira state. This preserves user intent for recent actions while converging to Jira truth over time.

### Exponential backoff for rate limiting

On HTTP 429 responses, the client applies exponential backoff starting at 1 second, doubling up to a 60-second cap. The Retry-After header is respected when present. Rate-limited state is surfaced in the status bar so the user knows sync is delayed.

### Setup wizard discovers transitions automatically

Rather than requiring users to manually find Jira transition IDs, the setup wizard calls `GET /rest/api/3/project/{key}/statuses` to enumerate available statuses and `GET /rest/api/3/issue/{key}/transitions` to discover transition IDs. It then generates column mappings automatically based on status name heuristics (matching common names like "To Do", "In Progress", "Done").

---

## Risks / Trade-offs

### Jira API rate limits

Atlassian Cloud enforces rate limits that vary by tenant and are not well-documented. Aggressive polling could hit limits, especially during initial sync of large backlogs. Mitigation: exponential backoff, configurable sync interval (default 60s), and JQL filters to limit result set size.

### ADF complexity and edge cases

ADF supports a wide range of node types, marks, and nesting patterns. Some combinations (deeply nested lists inside tables, media references to attachment IDs) are difficult to convert cleanly to Markdown. Mitigation: handle all common node types listed in the spec, render unknown nodes as plain text with a marker, and log warnings for unhandled types so they can be addressed incrementally.

### Transition ID discovery is fragile

Jira workflows vary widely across projects and organizations. The setup wizard's heuristic matching (status name to column name) will not work for every workflow. Mitigation: the wizard presents discovered mappings for user confirmation and allows manual override. Invalid transition IDs produce clear error messages suggesting re-running the wizard.

### Conflict resolution window is a heuristic

The 5-minute local-wins window is a pragmatic choice, not a guarantee. A user who moves a card and walks away for 6 minutes may see their move overwritten by a Jira sync. Mitigation: conflicts are logged to `sync_log` and surfaceable in a future diagnostics view. The window duration could be made configurable if users report issues.
