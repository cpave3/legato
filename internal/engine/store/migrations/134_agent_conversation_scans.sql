-- The executions the session history's recovery has looked for
-- conversations of, so a later pass does not read their folders again.
CREATE TABLE IF NOT EXISTS agent_conversation_scans (
    execution_id TEXT PRIMARY KEY,
    scanned_at DATETIME NOT NULL
);
