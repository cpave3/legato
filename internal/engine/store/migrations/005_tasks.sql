-- Replace tickets table with provider-agnostic tasks table.
-- Core fields are direct columns; provider-specific metadata lives in remote_meta JSON.

CREATE TABLE tasks (
    id             TEXT PRIMARY KEY,
    title          TEXT NOT NULL,
    description    TEXT NOT NULL DEFAULT '',
    description_md TEXT NOT NULL DEFAULT '',
    status         TEXT NOT NULL DEFAULT '',
    priority       TEXT NOT NULL DEFAULT '',
    sort_order     INTEGER NOT NULL DEFAULT 0,
    provider       TEXT,
    remote_id      TEXT,
    remote_meta    TEXT,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at     DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_tasks_status ON tasks(status);
CREATE INDEX idx_tasks_provider ON tasks(provider);

-- Migrate existing ticket data: pack Jira-specific fields into remote_meta JSON.
INSERT INTO tasks (id, title, description, description_md, status, priority, sort_order,
                   provider, remote_id, remote_meta, created_at, updated_at)
SELECT
    id,
    summary,
    COALESCE(description, ''),
    COALESCE(description_md, ''),
    status,
    COALESCE(priority, ''),
    COALESCE(sort_order, 0),
    'jira',
    id,
    json_object(
        'remote_status', COALESCE(remote_status, ''),
        'remote_updated_at', COALESCE(remote_updated_at, ''),
        'issue_type', COALESCE(issue_type, ''),
        'assignee', COALESCE(assignee, ''),
        'labels', COALESCE(labels, ''),
        'epic_key', COALESCE(epic_key, ''),
        'epic_name', COALESCE(epic_name, ''),
        'url', COALESCE(url, ''),
        'stale_at', stale_at,
        'local_move_at', local_move_at,
        'remote_transition', COALESCE((
            SELECT cm.remote_transition FROM column_mappings cm
            WHERE cm.column_name = status
            LIMIT 1
        ), '')
    ),
    created_at,
    updated_at
FROM tickets;

-- Update agent_sessions: rename ticket_id → task_id
CREATE TABLE agent_sessions_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL,
    tmux_session TEXT NOT NULL UNIQUE,
    command TEXT NOT NULL DEFAULT 'shell',
    status TEXT NOT NULL DEFAULT 'running',
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    ended_at DATETIME
);

INSERT INTO agent_sessions_new (id, task_id, tmux_session, command, status, started_at, ended_at)
SELECT id, ticket_id, tmux_session, command, status, started_at, ended_at
FROM agent_sessions;

DROP TABLE agent_sessions;
ALTER TABLE agent_sessions_new RENAME TO agent_sessions;

-- Update sync_log: rename ticket_id → task_id
CREATE TABLE sync_log_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id TEXT NOT NULL,
    action TEXT NOT NULL,
    detail TEXT,
    created_at TEXT NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO sync_log_new (id, task_id, action, detail, created_at)
SELECT id, ticket_id, action, detail, created_at
FROM sync_log;

DROP TABLE sync_log;
ALTER TABLE sync_log_new RENAME TO sync_log;

-- Drop old tickets table
DROP TABLE IF EXISTS tickets;
