CREATE TABLE review_tours (
    task_id           TEXT PRIMARY KEY,
    status            TEXT NOT NULL DEFAULT 'capturing',
    summary           TEXT NOT NULL DEFAULT '',
    base_sha          TEXT NOT NULL DEFAULT '',
    last_reviewed_sha TEXT NOT NULL DEFAULT '',
    ready_at          DATETIME,
    created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at        DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE review_steps (
    id                TEXT PRIMARY KEY,
    task_id           TEXT NOT NULL,
    kind              TEXT NOT NULL,
    commit_sha        TEXT NOT NULL DEFAULT '',
    files             TEXT NOT NULL DEFAULT '[]',
    title             TEXT NOT NULL DEFAULT '',
    narration         TEXT NOT NULL DEFAULT '',
    risk              TEXT NOT NULL DEFAULT '',
    order_hint        INTEGER,
    seq               INTEGER NOT NULL,
    subtask_id        TEXT NOT NULL DEFAULT '',
    dirty_fingerprint TEXT NOT NULL DEFAULT '',
    reviewed_at       DATETIME,
    orphaned_at       DATETIME,
    created_at        DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at        DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE UNIQUE INDEX idx_review_steps_commit ON review_steps(task_id, commit_sha) WHERE commit_sha != '';
CREATE INDEX idx_review_steps_task ON review_steps(task_id, seq);

CREATE TABLE review_transcript (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id      TEXT NOT NULL,
    step_id      TEXT NOT NULL,
    kind         TEXT NOT NULL,
    author       TEXT NOT NULL,
    body         TEXT NOT NULL,
    delivered_at DATETIME,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX idx_review_transcript_step ON review_transcript(step_id);
