## Database

### Persistence boundary and connection policy

Application entry points resolve `config.DBConfig` through `config.ResolveDatabase` and use the shared opener in `cmd/legato/database.go`. An omitted `db.driver` resolves to `sqlite`, preserving existing `db.path` configurations; unsupported drivers fail at startup. `data_dir` is resolved separately and defaults to the SQLite file's parent, so existing auth tokens, certificates, and artifacts do not move.

Production services, CLI commands, TUI code, and the web server use store behavior instead of a raw `*sqlx.DB`. SQLite-specific migrations, reporting SQL, full-text search, generated IDs, and connection setup stay behind the store boundary.

The SQLite DSN enables foreign keys and a 5-second busy timeout on every pooled connection. The pool is capped at four connections. A read-only compatibility preflight runs before WAL mode can be changed. Migration transactions use an immediate SQLite transaction, taking ownership before reading schema state; WAL initialization retries bounded lock contention so racing Legato processes can safely open the same database.

### Migrations and compatibility

Migration SQL remains embedded and SQLite-specific. The backend-neutral `schema_migrations` table becomes authoritative after first open by this version. Existing databases bootstrap it transactionally from `PRAGMA user_version`; each missing migration and its ledger row commit together. The opener validates that ledger versions are contiguous and names match the embedded migrations, rejects schema revisions newer than the binary, and rejects disagreement between the ledger and `user_version`. Supported partial `ADD COLUMN` migrations skip only columns already present and execute every remaining statement before metadata advances. This is the approved new-binary-only compatibility policy. `Store.SchemaStatus(ctx)` reports the current and supported revisions without exposing the raw connection.

Store tests use real temporary SQLite files and cover fresh creation, reopen durability, legacy bootstrap, future/mismatched schema refusal, per-connection PRAGMAs, and racing migration openers. Architecture tests prevent production packages from importing `sqlx`, calling `Store.DB()`, bypassing the shared command opener, or creating sibling dependencies between engine packages.

### Execution activity and reporting

An **execution** is one independently startable and terminable unit of agent work. Interactive agents, workflow `agent.run` attempts, swarm conductors, and swarm workers each receive a Legato-generated `LEGATO_EXECUTION_ID` before spawn. Provider session IDs, tmux names, task IDs, and workflow IDs are metadata and must never select an activity stream.

`executions` durably owns lifecycle (`running` → `succeeded|failed|cancelled|lost`), task/project/workflow/swarm attribution, provider-session links, and start/end timestamps. `execution_activity` stores its `working` and `waiting` spans with at most one open span per execution. Terminalization atomically sets `ended_at` and closes open activity; reporting additionally clips every span to execution end. Parallel execution active time is additive and may exceed workflow/swarm wall-clock time.

Migration 055 intentionally drops `state_intervals` without importing it because task-keyed intervals cannot be reliably associated with concurrent executions and include known orphan inflation. `reporting_metadata.activity_epoch` records the reset boundary. Tasks, workflows, attempts, sessions, swarms, reviews, plans, and non-duration history are preserved. Product reports treat pre-epoch activity as unavailable, not proven zero.

Execution history is not deleted during normal session cleanup or task deletion. `agent_sessions` remains operational and may be removed only after its linked execution is terminal. Task-triggered workflow executions retain their synthetic task as `execution_task_id` but attribute report totals to `workflow_runs.related_task_id`; repository-only workflow activity remains project/workflow-only.

## Schema

- SQLite file location: `cfg.DB.Path` > `$XDG_DATA_HOME/legato/legato.db` > `~/.local/share/legato/legato.db`
- Core schema includes `tasks`, `column_mappings`, `sync_log`, `agent_sessions`, `executions`, `execution_activity`, `reporting_metadata`, `workspaces`, swarm tables, review tours/passes/findings, plans/revisions/comments/interactions, workflow runs/steps/attempts/artifacts, `task_notes`, and the evidence-backed learning ledger.
- `tasks` table: core fields (id, title, local `description`/`description_md`, provider-owned `remote_description_md`, status, priority, sort_order, workspace_id, archived_at, created_at, updated_at) + nullable provider link (provider, remote_id, remote_meta JSON) + nullable `code_context` TEXT (durable repository/branch/SHA/path/provenance JSON) + nullable `pr_meta` TEXT (refreshable PR number/URL/state/review/check/comment/pin/outcome JSON). `archived_at` is nullable DATETIME — NULL means active, non-NULL means archived (hidden from board but retained in DB)
- `workspaces` table: id (INTEGER PRIMARY KEY), name (TEXT UNIQUE), color (TEXT), sort_order (INTEGER)
- Local tasks: provider/remote_id/remote_meta are NULL and render `description_md` unchanged. Synced tasks: provider='jira', remote_id=Jira key, remote provider Markdown in `remote_description_md`, and remote_meta=JSON with remote_status, issue_type, assignee, labels, etc. Their editable `description_md` is composed under `## Legato details` at read time; the combined value is never persisted.
- Task IDs: 8-char lowercase alphanumeric (crypto/rand) for local tasks, provider IDs (e.g. REX-1234) for synced tasks
- `agent_sessions` and `sync_log` reference `task_id` (not ticket_id).
- `workflow_runs.task_id` is the workflow-owned execution task; `related_task_id` is the optional existing task selected through a typed `task` input. The execution task always belongs to the managed **Workflows** workspace. Successful final-step completion atomically moves it to Done and sets `archived_at`; failed and cancelled runs remain active for intervention.
- `workflow_step_runs`, `workflow_step_attempts`, and `workflow_artifacts` retain ordered state, retries, agent identity, managed paths, media types, sizes, and digests. `(attempt_id, output_key)` is unique so interrupted completion callbacks can safely reuse an unchanged artifact.
- `task_notes` stores durable Markdown notes independently of task descriptions (`id`, `task_id`, `body`, `source`, optional remote ID, timestamp). Workflow `task.note` records notes locally for every task, including Jira-backed targets, without publishing Jira comments.
- Migration 049 owns Learn persistence: `learning_evidence` and support/counter links, revisioned `learnings`, reflection requests, replay-safe curation generations, incorporation/verification records, prompt audit manifests, and FTS5 claim/rationale/evidence indexes. Full terminal/JSONL transcripts are not stored. See [Legato Learn](learning.md).
- `state_intervals` table: tracks working/waiting time spans per task_id. After migration 021, `task_id` has no FK and accepts both task IDs (from `tasks`) and swarm sub-task IDs (from `swarm_subtasks`). `Store.DeleteTask` manually cleans up state_intervals for the task and any swarm sub-task children since the FK cascade was removed.
- Migrations: `001_init.sql` through `048_workflow_trigger_definition.sql` cover core tasks, sync, agents, workspaces, swarms, review tours, plans, workflows, attention, Chimera discovery, metadata, code context, and triggers. `049_learning.sql`, `050_learning_proposal_target_revision.sql`, and `051_learning_prompt_activation.sql` own the learning ledger and its revision/activation additions.

## Task metadata and workflow automation

Tasks store local arbitrary JSON metadata in `tasks.metadata`. This is separate from provider-owned `remote_meta`; provider upserts preserve local metadata. Dotted-path updates are transactional so unrelated keys are retained.

Explicit local column moves atomically persist a `task_transitions` snapshot. Matching workflow definitions create `workflow_trigger_requests` with a definition digest and full definition snapshot. `(transition_id, workflow_name, definition_digest)` is unique, and `workflow_runs.trigger_request_id` is unique when present, preventing replay from launching duplicate runs. Requests remain durable in `pending`, `waiting_input`, `waiting_confirmation`, `starting`, `started`, `skipped`, or `failed` state. `starting` is an atomic confirmation claim that prevents duplicate workflow launches.

Migrations 044-046 add task metadata, transition/request persistence, and the workflow-run trigger link. Migration 047 adds `code_context`, backfills repository identity from legacy `pr_meta`, and pins existing numbered PR links. Migration 048 repairs early migration-045 databases by adding the missing workflow-trigger definition snapshot column. Migration 058 extends the trigger status constraint for explicit confirmation.

### Agent UI items

Migration 063 adds `agent_ui_items` and `agent_ui_item_revisions`: what agents asked Legato to show through `legato mcp serve`. An item (`ui-<10 hex>`) has a placement (`panel` for an artifact, `inline` for a card), an optional agent-chosen key that is unique per task and placement, and a current revision; each revision holds its format (`html`, `markdown`, `components`), title, content and fallback text. `task_id` is the agent's own identity, which is a sub-task ID for a swarm worker, so it has no foreign key: `Store.DeleteTask` removes a task's items and its workers', and `Store.DeleteSubtask` a worker's. `SaveAgentUIItem` trims an item to its newest revisions and a task to its newest items in the same transaction; the limits are the service's. See [Agent tools](agent-mcp.md).

Migration 064 adds `agent_ui_item_revisions.images`: the image files one revision uses, as the service encodes them (the name the content refers to an image by, and the stored copy's file name). The files themselves are not in the database: they are under `<data_dir>/agent-ui-images/<task-id>/`, kept and removed by `AgentUIService`. See [Agent tools](agent-mcp.md) § Image files.

### Future PostgreSQL work

This boundary prepares but does not claim PostgreSQL support. A later change must test against a real PostgreSQL server, add a PostgreSQL baseline and advisory migration locking, implement dialect-specific reporting/search/generated IDs, and provide a one-way SQLite importer. Safe shared multi-host operation also requires node ownership and coordination beyond the database driver.
