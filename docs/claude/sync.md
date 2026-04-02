## Provider Architecture

The ticket source is abstracted behind `service.TicketProvider` — Jira is the first implementation, but others (Linear, GitHub Issues, etc.) can be plugged in by implementing the same interface:

- `service.TicketProvider` interface defined in `internal/service/provider.go`
- `service.JiraProviderAdapter` in `internal/service/provider_jira.go` bridges `jira.Provider` → `TicketProvider`
- Sync service (`internal/service/sync.go`) depends only on the interface, never on Jira directly
- ADF-to-Markdown conversion is internal to the Jira provider — the interface returns markdown

## Sync Algorithm

- **Pull**: periodic fetch via provider → **update existing tracked tasks only** (new tickets must be imported manually via `i` overlay or `ImportRemoteTask`). Status-to-column mapping, stale detection via remote_meta. Archived tasks skipped. Untracked remote tickets are silently ignored — pull never auto-imports
- **Push**: local SQLite update first (non-blocking), then async remote transition; skipped for local tasks (provider=NULL); failure logs to `sync_log` and preserves local column
- **Conflict resolution**: local wins within 5-minute window of `local_move_at` (stored in remote_meta); after window, remote state accepted on next pull
- **Scheduler**: configurable interval (default 60s), publishes SyncStarted/SyncCompleted/SyncFailed events
- **SearchRemote**: builds JQL (`summary ~ "query" OR key = "query"` scoped to `projectKeys`), min 2-char query, `ORDER BY updated DESC`
- **ImportRemoteTask**: `ImportRemoteTask(ctx, ticketID, workspaceID)` fetches single ticket via `provider.GetTicket`, creates local task with provider metadata and optional workspace, skips if already tracked
- **Wiring**: `main.go` creates Jira provider + sync service when config is present, runs initial sync + periodic scheduler, passes `SyncService` to TUI app
