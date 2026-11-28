CREATE TABLE IF NOT EXISTS native_security_incidents (
    execution_id TEXT NOT NULL,
    generation TEXT NOT NULL,
    task_id TEXT NOT NULL,
    category TEXT NOT NULL CHECK (category IN ('permission', 'question', 'plan', 'control', 'unknown')),
    first_seen_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    count INTEGER NOT NULL DEFAULT 1,
    PRIMARY KEY (execution_id, generation)
);
