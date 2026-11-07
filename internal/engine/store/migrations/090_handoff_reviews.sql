-- Review passes on a worker's make-a-change handoffs. handoff_reviews is
-- one card's review loop; a pass is one reviewer session over every pull
-- request of the handoff, each pinned at its head.
CREATE TABLE IF NOT EXISTS handoff_reviews (
    task_id TEXT PRIMARY KEY REFERENCES tasks(id) ON DELETE CASCADE,
    settings TEXT NOT NULL DEFAULT '{}',
    -- '' (the author works), ready, waiting_for_reviewer, reviewing,
    -- waiting_for_author, clean, stuck, skipped (no pull requests)
    phase TEXT NOT NULL DEFAULT '',
    pass INTEGER NOT NULL DEFAULT 0,
    mismatches INTEGER NOT NULL DEFAULT 0,
    nudged_at TEXT NOT NULL DEFAULT '',
    author_busy_since_nudge INTEGER NOT NULL DEFAULT 0,
    reason TEXT NOT NULL DEFAULT '',
    queued_at TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS handoff_review_passes (
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    pass INTEGER NOT NULL,
    -- reviewing, submitted, posted, discarded, unable
    state TEXT NOT NULL,
    verdict TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    execution_id TEXT NOT NULL DEFAULT '',
    checkout TEXT NOT NULL DEFAULT '',
    head_sha TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    ended_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (task_id, pass)
);

CREATE TABLE IF NOT EXISTS handoff_review_prs (
    task_id TEXT NOT NULL,
    pass INTEGER NOT NULL,
    repo TEXT NOT NULL,
    number INTEGER NOT NULL,
    branch TEXT NOT NULL,
    base TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    head_sha TEXT NOT NULL,
    review_id INTEGER NOT NULL DEFAULT 0,
    review_url TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (task_id, pass, number),
    FOREIGN KEY (task_id, pass) REFERENCES handoff_review_passes(task_id, pass) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS handoff_review_findings (
    id TEXT PRIMARY KEY,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    pass INTEGER NOT NULL,
    pr INTEGER NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('blocking', 'nit')),
    title TEXT NOT NULL,
    body TEXT NOT NULL,
    path TEXT NOT NULL DEFAULT '',
    line INTEGER NOT NULL DEFAULT 0,
    start_line INTEGER NOT NULL DEFAULT 0,
    side TEXT NOT NULL DEFAULT '',
    -- The inline comment that opened the finding's thread; 0 when it went
    -- in the review body.
    comment_id INTEGER NOT NULL DEFAULT 0,
    -- open until a later pass rules it resolved.
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
    ruled_pass INTEGER NOT NULL DEFAULT 0,
    ruling TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_handoff_review_findings_task ON handoff_review_findings(task_id, pass);
