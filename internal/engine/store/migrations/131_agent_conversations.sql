-- Session history: every provider conversation an agent reported (a Claude
-- Code session, a Codex thread, a Chimera session), and each execution it was
-- live in (its runs). task_native_sessions stays what a slot resumes by
-- default; these rows outlive it, the agent session and the task itself.
CREATE TABLE IF NOT EXISTS agent_conversations (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    agent_kind TEXT NOT NULL CHECK (agent_kind IN ('chimera', 'claude-code', 'codex')),
    provider_session_id TEXT NOT NULL CHECK (provider_session_id != ''),
    storage_dir TEXT NOT NULL DEFAULT '',
    -- No foreign key: deleting a task leaves its conversations.
    task_id TEXT NOT NULL DEFAULT '',
    slot TEXT NOT NULL DEFAULT 'main',
    task_title TEXT NOT NULL DEFAULT '',
    ephemeral INTEGER NOT NULL DEFAULT 0,
    reeve INTEGER NOT NULL DEFAULT 0,
    cwd TEXT NOT NULL DEFAULT '',
    repo TEXT NOT NULL DEFAULT '',
    branch TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'recorded' CHECK (source IN ('recorded', 'recovered')),
    first_message TEXT NOT NULL DEFAULT '',
    last_message TEXT NOT NULL DEFAULT '',
    last_message_at DATETIME,
    first_seen_at DATETIME NOT NULL,
    last_seen_at DATETIME NOT NULL,
    transcript_path TEXT NOT NULL DEFAULT '',
    transcript_state TEXT NOT NULL DEFAULT 'unknown' CHECK (transcript_state IN ('present', 'missing', 'unknown')),
    transcript_checked_at DATETIME,
    UNIQUE (agent_kind, storage_dir, provider_session_id)
);
CREATE INDEX IF NOT EXISTS idx_agent_conversations_task ON agent_conversations(task_id, slot);
CREATE INDEX IF NOT EXISTS idx_agent_conversations_last_seen ON agent_conversations(last_seen_at);

CREATE TABLE IF NOT EXISTS agent_conversation_runs (
    conversation_id INTEGER NOT NULL REFERENCES agent_conversations(id) ON DELETE CASCADE,
    execution_id TEXT NOT NULL,
    started_at DATETIME NOT NULL,
    ended_at DATETIME,
    -- NULL while the run is live.
    end_reason TEXT CHECK (end_reason IN ('exited', 'stopped', 'lost', 'interrupted', 'dismissed', 'replaced')),
    PRIMARY KEY (conversation_id, execution_id),
    CHECK ((end_reason IS NULL) = (ended_at IS NULL))
);
CREATE INDEX IF NOT EXISTS idx_agent_conversation_runs_execution ON agent_conversation_runs(execution_id);

-- Written in Go in the same transaction as its row, as learnings_fts is.
CREATE VIRTUAL TABLE IF NOT EXISTS agent_conversations_fts USING fts5(
    conversation_id UNINDEXED, task_title, task_id, cwd, repo, branch, first_message, last_message
);

-- Backfill: one conversation for each saved resume record, its execution as
-- its run, ended as that execution ended.
INSERT INTO agent_conversations (
    agent_kind, provider_session_id, storage_dir, task_id, slot, task_title,
    ephemeral, reeve, cwd, repo, branch, first_seen_at, last_seen_at)
SELECT n.agent_kind, n.provider_session_id, n.storage_dir, n.task_id, n.slot,
    COALESCE(t.title, ''), COALESCE(t.ephemeral, 0),
    CASE WHEN COALESCE(t.kind, '') = 'reeve' THEN 1 ELSE 0 END,
    COALESCE(NULLIF(n.cwd, ''), e.working_dir, ''),
    COALESCE(c.repo, ''), COALESCE(c.branch, ''),
    COALESCE(e.started_at, n.updated_at), n.updated_at
FROM task_native_sessions n
LEFT JOIN tasks t ON t.id = n.task_id
LEFT JOIN executions e ON e.id = n.execution_id AND n.execution_id != ''
LEFT JOIN job_clones c ON c.path = COALESCE(NULLIF(n.cwd, ''), e.working_dir) AND c.removed_at IS NULL
WHERE true
ON CONFLICT (agent_kind, storage_dir, provider_session_id) DO NOTHING;

INSERT OR IGNORE INTO agent_conversation_runs (conversation_id, execution_id, started_at, ended_at, end_reason)
SELECT ac.id, e.id, e.started_at, e.ended_at,
    CASE
        WHEN e.status = 'running' THEN NULL
        WHEN e.status = 'cancelled' THEN 'stopped'
        WHEN e.status = 'lost' AND COALESCE(a.status, '') = 'interrupted' THEN 'interrupted'
        WHEN e.status = 'lost' THEN 'lost'
        ELSE 'exited'
    END
FROM task_native_sessions n
JOIN agent_conversations ac ON ac.agent_kind = n.agent_kind AND ac.storage_dir = n.storage_dir AND ac.provider_session_id = n.provider_session_id
JOIN executions e ON e.id = n.execution_id
LEFT JOIN agent_sessions a ON a.id = e.agent_session_id;

INSERT INTO agent_conversations_fts (conversation_id, task_title, task_id, cwd, repo, branch, first_message, last_message)
SELECT id, task_title, task_id, cwd, repo, branch, first_message, last_message FROM agent_conversations;
