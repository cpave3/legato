-- Launch authentication is private, never part of session listings.
CREATE TABLE IF NOT EXISTS chimera_launch_secrets (
    agent_session_id INTEGER PRIMARY KEY REFERENCES agent_sessions(id) ON DELETE CASCADE,
    secret TEXT NOT NULL,
    file_path TEXT NOT NULL
);
