CREATE TABLE IF NOT EXISTS task_native_sessions (
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    agent_kind TEXT NOT NULL CHECK (agent_kind IN ('claude-code', 'codex')),
    provider_session_id TEXT NOT NULL CHECK (provider_session_id != ''),
    cwd TEXT NOT NULL,
    storage_dir TEXT NOT NULL DEFAULT '',
    -- Ownership is historical: pruning agent_sessions must not remove resume IDs.
    agent_session_id INTEGER NOT NULL,
    execution_id TEXT NOT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (task_id, agent_kind)
);
