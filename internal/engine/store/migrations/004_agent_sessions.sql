CREATE TABLE agent_sessions (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id TEXT NOT NULL,
    tmux_session TEXT NOT NULL UNIQUE,
    command TEXT NOT NULL DEFAULT 'shell',
    status TEXT NOT NULL DEFAULT 'running',
    started_at DATETIME NOT NULL DEFAULT (datetime('now')),
    ended_at DATETIME
);
