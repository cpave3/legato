CREATE TABLE task_prefs (
    task_id TEXT PRIMARY KEY,
    notify_enabled INTEGER NOT NULL DEFAULT 0
);
