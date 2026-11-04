## Provider Architecture

Several ticket providers sync at once. The sync service (`internal/service/sync.go`, `NewTicketSync`) holds one `service.TicketSource` per provider (`internal/service/ticket_source.go`), and a task belongs to the source whose `Name` is its `provider`. A source owns what is its own: `Pull` (the tracked tasks' tickets, and `Missing` for the keys the provider no longer has), `Search` by text, `Get` by key, `Column` (where a status goes on the board), `Enabled` (configured and connected; a source that is not is skipped quietly), `Interval` and `MovesCards`. The service owns what they share: updating tasks, stale marks, the conflict window, local-only columns, attachments, events.

- **Jira** is `NewJiraSource` over `service.TicketProvider` (`internal/service/provider.go`; `JiraProviderAdapter` in `provider_jira.go` bridges `jira.Provider`). It builds JQL for pulls and searches and places statuses through the column mappings. ADF-to-Markdown conversion is internal to the Jira provider. Jira is the one provider that takes card moves.
- `cmd/legato/ticket_sync_runtime.go` (`newTicketSync`) builds the sources the configuration names, for the TUI, `legato serve` and workflow commands; nil when there are none.
- A source that fails leaves the others syncing: `SyncResult.Sources` has each one's outcome by name, and `Sync` returns an error only with them.
- Remote update times are kept with sub-second precision (`RFC3339Nano`), so a ticket changed within a second is not applied again each cycle.

### Sync Algorithm

### Jira image attachments

Tracked Jira tickets cache `image/*` attachments during import and every sync. Existing tickets are backfilled on the first sync after enabling this behavior, even when the Jira issue timestamp has not changed. Missing temporary files are restored on later syncs, removed Jira attachments are deleted locally, and archiving a task removes its attachment directory.

The cache lives under `$XDG_RUNTIME_DIR/legato/jira-attachments` when available, otherwise under the user's system temporary directory. Absolute paths appear in `legato task show` output and swarm briefs so agents can inspect images locally. Configure `jira.attachment_max_size_bytes` to limit each image; the default is 25 MiB. Non-image attachments are not downloaded.


- **Pull**: periodic fetch via provider → **update existing tracked tasks only** (new tickets must be imported manually via `i` overlay or `ImportRemoteTask`). Provider Markdown refreshes `remote_description_md` while preserving editable Legato `description_md`; status-to-column mapping and stale detection use remote_meta. Archived tasks skipped. Untracked remote tickets are silently ignored — pull never auto-imports
- **Push**: local SQLite update first (non-blocking), then async remote transition, for Jira's tasks only: other providers' cards move locally; failure logs to `sync_log` and preserves local column
- **Conflict resolution**: local wins within 5-minute window of `local_move_at` (stored in remote_meta); after window, remote state accepted on next pull
- **Scheduler**: ticks at the shortest source interval (Jira's default 60s) and pulls each source when its own interval has passed; a manual sync pulls them all. Publishes SyncStarted/SyncCompleted/SyncFailed events
- **SearchRemote**: asks every enabled source (min 2-char query) and labels each result with its `Provider`; results from the sources that answered, an error only when none did. Jira's search is JQL `summary ~ "query" OR key = "query"` scoped to `projectKeys`, `ORDER BY updated DESC`
- **ImportRemoteTask**: `ImportRemoteTask(ctx, provider, ticketID, workspaceID)` fetches one ticket from the named source (`""`: the only enabled one, else an error naming them) and creates a local task with that provider and optional workspace. A task of the same key from the same provider is returned as it is; one from another provider, or a local task, is `ErrTicketKeyTaken`, since the key is the task ID. `POST /api/remote/import` takes `provider` in the same way, and `/api/remote/search` results carry it
- **Provider sync** (`POST /api/board/sync`, `r` in the TUI): `ProviderSyncResult` is each provider's outcome by name (`jira`, `burnlite`, `github`), `jira` and `github` always present
