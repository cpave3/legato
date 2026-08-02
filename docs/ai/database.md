## Database

### Persistence boundary and connection policy

Application entry points resolve `config.DBConfig` through `config.ResolveDatabase` and use the shared opener in `cmd/legato/database.go`. An omitted `db.driver` resolves to `sqlite`, preserving existing `db.path` configurations; unsupported drivers fail at startup. `data_dir` is resolved separately and defaults to the SQLite file's parent, so existing auth tokens, certificates, and artifacts do not move.

Production services, CLI commands, TUI code, and the web server use store behavior instead of a raw `*sqlx.DB`. SQLite-specific migrations, reporting SQL, full-text search, generated IDs, and connection setup stay behind the store boundary.

The SQLite DSN enables foreign keys and a 5-second busy timeout on every pooled connection. The pool is capped at four connections. A read-only compatibility preflight runs before WAL mode can be changed. Migration transactions use an immediate SQLite transaction, taking ownership before reading schema state; WAL initialization retries bounded lock contention so racing Legato processes can safely open the same database.

### Migrations and compatibility

Migration SQL remains embedded and SQLite-specific. The backend-neutral `schema_migrations` table becomes authoritative after first open by this version. Existing databases bootstrap it transactionally from `PRAGMA user_version`; each missing migration and its ledger row commit together. The opener validates that ledger versions are contiguous and names match the embedded migrations, rejects schema revisions newer than the binary, and rejects disagreement between the ledger and `user_version`. Supported partial `ADD COLUMN` migrations skip only columns already present and execute every remaining statement before metadata advances. This is the approved new-binary-only compatibility policy. `Store.SchemaStatus(ctx)` reports the current and supported revisions without exposing the raw connection.

Store tests use real temporary SQLite files and cover fresh creation, reopen durability, legacy bootstrap, future/mismatched schema refusal, per-connection PRAGMAs, and racing migration openers. Architecture tests prevent production packages from importing `sqlx`, calling `Store.DB()`, bypassing the shared command opener, or creating sibling dependencies between engine packages.

### Schema

- SQLite file location: `cfg.DB.Path` > `$XDG_DATA_HOME/legato/legato.db` > `~/.local/share/legato/legato.db`
- Core schema includes `tasks`, `column_mappings`, `sync_log`, `agent_sessions`, `state_intervals`, `workspaces`, swarm tables, review tours/passes/findings, plans/revisions/comments/interactions, workflow runs/steps/attempts/artifacts, and `task_notes`.
- `tasks` table: core fields (id, title, description, description_md, status, priority, sort_order, workspace_id, archived_at, created_at, updated_at) + nullable provider link (provider, remote_id, remote_meta JSON) + nullable `code_context` TEXT (durable repository/branch/SHA/path/provenance JSON) + nullable `pr_meta` TEXT (refreshable PR number/URL/state/review/check/comment/pin/outcome JSON). `archived_at` is nullable DATETIME — NULL means active, non-NULL means archived (hidden from board but retained in DB)
- `workspaces` table: id (INTEGER PRIMARY KEY), name (TEXT UNIQUE), color (TEXT), sort_order (INTEGER)
- Local tasks: provider/remote_id/remote_meta are NULL. Synced tasks: provider='jira', remote_id=Jira key, remote_meta=JSON with remote_status, issue_type, assignee, labels, etc.
- Task IDs: 8-char lowercase alphanumeric (crypto/rand) for local tasks, provider IDs (e.g. REX-1234) for synced tasks
- `agent_sessions` and `sync_log` reference `task_id` (not ticket_id).
- `workflow_runs.task_id` is the workflow-owned execution task; `related_task_id` is the optional existing task selected through a typed `task` input. The execution task defaults to the managed **Workflows** workspace and moves to Done only after successful final-step completion.
- `workflow_step_runs`, `workflow_step_attempts`, and `workflow_artifacts` retain ordered state, retries, agent identity, managed paths, media types, sizes, and digests. `(attempt_id, output_key)` is unique so interrupted completion callbacks can safely reuse an unchanged artifact.
- `task_notes` stores durable Markdown notes independently of task descriptions (`id`, `task_id`, `body`, `source`, optional remote ID, timestamp). Workflow `task.note` records local notes after any required provider publication; Jira-backed targets receive Jira comments.
- `state_intervals` table: tracks working/waiting time spans per task_id. After migration 021, `task_id` has no FK and accepts both task IDs (from `tasks`) and swarm sub-task IDs (from `swarm_subtasks`). `Store.DeleteTask` manually cleans up state_intervals for the task and any swarm sub-task children since the FK cascade was removed.
- Migration files are discovered in lexical order from `internal/engine/store/migrations`. The embedded filename sequence is the authoritative catalog; current checked-in revisions run through the highest numbered migration in that directory.

## Task metadata and workflow automation

Tasks store local arbitrary JSON metadata in `tasks.metadata`. This is separate from provider-owned `remote_meta`; provider upserts preserve local metadata. Dotted-path updates are transactional so unrelated keys are retained.

Explicit local column moves atomically persist a `task_transitions` snapshot. Matching workflow definitions create `workflow_trigger_requests` with a definition digest and full definition snapshot. `(transition_id, workflow_name, definition_digest)` is unique, and `workflow_runs.trigger_request_id` is unique when present, preventing replay from launching duplicate runs. Requests remain durable in `pending`, `waiting_input`, `started`, `skipped`, or `failed` state.

Migrations 044-046 add task metadata, transition/request persistence, and the workflow-run trigger link. Migration 047 adds `code_context`, backfills repository identity from legacy `pr_meta`, and pins existing numbered PR links. Migration 048 repairs early migration-045 databases by adding the missing workflow-trigger definition snapshot column.


### Future PostgreSQL work

This boundary prepares but does not claim PostgreSQL support. A later change must test against a real PostgreSQL server, add a PostgreSQL baseline and advisory migration locking, implement dialect-specific reporting/search/generated IDs, and provide a one-way SQLite importer. Safe shared multi-host operation also requires node ownership and coordination beyond the database driver.
