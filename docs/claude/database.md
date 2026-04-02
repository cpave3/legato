## Database

- SQLite file location: `cfg.DB.Path` > `$XDG_DATA_HOME/legato/legato.db` > `~/.local/share/legato/legato.db`
- Migrations embedded via `embed.FS`, tracked with `PRAGMA user_version`
- WAL mode enabled, foreign keys ON
- Schema: `tasks`, `column_mappings`, `sync_log`, `agent_sessions`, `state_intervals`, `workspaces` tables
- `tasks` table: core fields (id, title, description, description_md, status, priority, sort_order, workspace_id, archived_at, created_at, updated_at) + nullable provider link (provider, remote_id, remote_meta JSON) + nullable `pr_meta` TEXT (JSON with branch, pr_number, pr_url, state, is_draft, review_decision, check_status, comment_count, updated_at). `archived_at` is nullable DATETIME — NULL means active, non-NULL means archived (hidden from board but retained in DB)
- `workspaces` table: id (INTEGER PRIMARY KEY), name (TEXT UNIQUE), color (TEXT), sort_order (INTEGER)
- Local tasks: provider/remote_id/remote_meta are NULL. Synced tasks: provider='jira', remote_id=Jira key, remote_meta=JSON with remote_status, issue_type, assignee, labels, etc.
- Task IDs: 8-char lowercase alphanumeric (crypto/rand) for local tasks, provider IDs (e.g. REX-1234) for synced tasks
- `agent_sessions` and `sync_log` reference `task_id` (not ticket_id)
- Migrations: `001_init.sql` (base), `002_stale_and_move_tracking.sql`, `003_rename_jira_to_remote.sql`, `004_agent_sessions.sql`, `005_tasks.sql` (tickets→tasks migration with remote_meta JSON packing), `006_agent_activity.sql`, `007_state_intervals.sql`, `008_workspaces.sql` (workspaces table + tasks.workspace_id FK), `009_archive.sql` (archived_at column on tasks), `010_pr_meta.sql` (pr_meta TEXT column on tasks)
