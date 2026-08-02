-- Migration 049 existed briefly during development before all learning tables
-- were present. Recreate the final base table for those development databases;
-- normal databases already have it and keep their data unchanged.
CREATE TABLE IF NOT EXISTS learnings (
    id TEXT PRIMARY KEY,
    scope TEXT NOT NULL CHECK (scope IN ('user', 'project')),
    project_key TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    claim TEXT NOT NULL,
    rationale TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'candidate',
    confidence REAL NOT NULL DEFAULT 0,
    revision INTEGER NOT NULL DEFAULT 1,
    canonical_learning_id TEXT,
    curator_run_id TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (canonical_learning_id) REFERENCES learnings(id)
);

-- Prompt activation is deliberately independent of acceptance and historical
-- prompt-profile incorporations. Every existing and future learning starts off.
ALTER TABLE learnings ADD COLUMN prompt_enabled INTEGER NOT NULL DEFAULT 0;
ALTER TABLE learnings ADD COLUMN prompt_decided INTEGER NOT NULL DEFAULT 0;
