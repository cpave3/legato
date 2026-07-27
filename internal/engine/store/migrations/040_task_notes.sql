CREATE TABLE task_notes (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    body TEXT NOT NULL,
    source TEXT NOT NULL DEFAULT 'local',
    remote_id TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_task_notes_task ON task_notes(task_id, created_at, id);
