CREATE TABLE native_bridge_registrations (
    agent_session_id INTEGER PRIMARY KEY REFERENCES agent_sessions(id) ON DELETE CASCADE,
    execution_id TEXT NOT NULL,
    public_endpoint TEXT NOT NULL UNIQUE,
    control_endpoint TEXT NOT NULL,
    generation TEXT NOT NULL
);
