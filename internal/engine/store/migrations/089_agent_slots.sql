-- Agent slots: a task's agents are its main slot's (the author's, and any
-- session the person starts) and its named slots' (a reviewer's). One agent
-- runs per slot; existing sessions are the main slot's.
ALTER TABLE agent_sessions ADD COLUMN slot TEXT NOT NULL DEFAULT 'main';

-- The resume record is kept per slot and agent tool. Chimera's selector moves
-- here from the task row; a Chimera record has no directory or owner.
CREATE TABLE task_native_sessions_slots (
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    slot TEXT NOT NULL DEFAULT 'main',
    agent_kind TEXT NOT NULL CHECK (agent_kind IN ('chimera', 'claude-code', 'codex')),
    provider_session_id TEXT NOT NULL CHECK (provider_session_id != ''),
    cwd TEXT NOT NULL DEFAULT '',
    storage_dir TEXT NOT NULL DEFAULT '',
    -- Ownership is historical: pruning agent_sessions must not remove resume IDs.
    agent_session_id INTEGER NOT NULL DEFAULT 0,
    execution_id TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (task_id, slot, agent_kind)
);
INSERT INTO task_native_sessions_slots (task_id, slot, agent_kind, provider_session_id, cwd, storage_dir, agent_session_id, execution_id, updated_at)
    SELECT task_id, 'main', agent_kind, provider_session_id, cwd, storage_dir, agent_session_id, execution_id, updated_at
    FROM task_native_sessions;
INSERT INTO task_native_sessions_slots (task_id, slot, agent_kind, provider_session_id)
    SELECT id, 'main', 'chimera', chimera_session_id FROM tasks
    WHERE chimera_session_id IS NOT NULL AND chimera_session_id != '';
DROP TABLE task_native_sessions;
ALTER TABLE task_native_sessions_slots RENAME TO task_native_sessions;
ALTER TABLE tasks DROP COLUMN chimera_session_id;
