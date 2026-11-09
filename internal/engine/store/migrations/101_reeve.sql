-- The Reeve: the agent of this Legato itself. Its sessions are hidden tasks
-- of kind reeve, kept off the board. reeve_sessions holds each session's
-- state (running, suspended, ended), the agent tool it runs, and its API
-- token (scoped reeve: read routes only), revoked when it ends.
ALTER TABLE tasks ADD COLUMN kind TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS reeve_sessions (
    task_id        TEXT PRIMARY KEY,
    state          TEXT NOT NULL DEFAULT 'running',
    agent_kind     TEXT NOT NULL DEFAULT '',
    model          TEXT NOT NULL DEFAULT '',
    launch_args    TEXT NOT NULL DEFAULT '[]',
    api_token      TEXT NOT NULL DEFAULT '',
    api_token_hash TEXT NOT NULL DEFAULT '',
    started_by     TEXT NOT NULL DEFAULT '',
    created_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at     TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    active_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    ended_at       TEXT
);
CREATE INDEX IF NOT EXISTS idx_reeve_sessions_token ON reeve_sessions(api_token_hash);
