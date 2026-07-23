CREATE TABLE review_passes (
    id         TEXT PRIMARY KEY,
    tour_id    TEXT NOT NULL,
    number     INTEGER NOT NULL,
    status     TEXT NOT NULL DEFAULT 'capturing',
    summary    TEXT NOT NULL DEFAULT '',
    guidance   TEXT NOT NULL DEFAULT '',
    head_sha   TEXT NOT NULL DEFAULT '',
    ready_at   DATETIME,
    reviewed_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(tour_id, number)
);

CREATE TABLE review_pass_plans (
    pass_id     TEXT PRIMARY KEY,
    plan_id     TEXT NOT NULL,
    revision_id TEXT NOT NULL,
    markdown    TEXT NOT NULL,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

INSERT INTO review_passes (id, tour_id, number, status, summary, head_sha, ready_at, reviewed_at, created_at, updated_at)
SELECT id || '-p1', id, 1, status, summary, head_sha, ready_at,
    CASE WHEN status = 'reviewed' THEN updated_at ELSE NULL END, created_at, updated_at
FROM review_tours;

ALTER TABLE review_steps ADD COLUMN pass_id TEXT NOT NULL DEFAULT '';
ALTER TABLE review_hunk_notes ADD COLUMN pass_id TEXT NOT NULL DEFAULT '';
ALTER TABLE review_chapter_hunks ADD COLUMN pass_id TEXT NOT NULL DEFAULT '';
ALTER TABLE review_transcript ADD COLUMN pass_id TEXT NOT NULL DEFAULT '';

UPDATE review_steps SET pass_id = tour_id || '-p1' WHERE pass_id = '';
UPDATE review_hunk_notes SET pass_id = tour_id || '-p1' WHERE pass_id = '';
UPDATE review_chapter_hunks SET pass_id = tour_id || '-p1' WHERE pass_id = '';
UPDATE review_transcript SET pass_id = tour_id || '-p1' WHERE pass_id = '';

DROP INDEX idx_review_steps_commit;
CREATE UNIQUE INDEX idx_review_steps_commit ON review_steps(task_id, pass_id, commit_sha) WHERE commit_sha != '';

CREATE INDEX idx_review_passes_tour ON review_passes(tour_id, number);
CREATE INDEX idx_review_steps_pass ON review_steps(pass_id);
CREATE INDEX idx_review_hunk_notes_pass ON review_hunk_notes(pass_id);
CREATE INDEX idx_review_chapter_hunks_pass ON review_chapter_hunks(pass_id);
CREATE INDEX idx_review_transcript_pass ON review_transcript(pass_id);
