-- A review loop's findings and items are its own (LEG-568). Their IDs were
-- global, so the same pull request number and check name in two
-- repositories ("k-7-build") made one row that both loops wrote, and the
-- loop that did not own it was approved with its check failing. Both tables
-- are keyed by (task_id, id) now, task_id being the loop; every row is kept.
CREATE TABLE handoff_review_findings_per_loop (
    id TEXT NOT NULL,
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
    comment_id INTEGER NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
    ruled_pass INTEGER NOT NULL DEFAULT 0,
    ruling TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (task_id, id)
);
INSERT INTO handoff_review_findings_per_loop (id, task_id, pass, pr, severity, title, body, path, line, start_line, side, comment_id, status, ruled_pass, ruling)
    SELECT id, task_id, pass, pr, severity, title, body, path, line, start_line, side, comment_id, status, ruled_pass, ruling
    FROM handoff_review_findings ORDER BY rowid;
DROP TABLE handoff_review_findings;
ALTER TABLE handoff_review_findings_per_loop RENAME TO handoff_review_findings;
CREATE INDEX IF NOT EXISTS idx_handoff_review_findings_task ON handoff_review_findings(task_id, pass);

CREATE TABLE handoff_review_items_per_loop (
    id TEXT NOT NULL,
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    kind TEXT NOT NULL,
    pass INTEGER NOT NULL,
    repo TEXT NOT NULL DEFAULT '',
    pr INTEGER NOT NULL DEFAULT 0,
    path TEXT NOT NULL DEFAULT '',
    line INTEGER NOT NULL DEFAULT 0,
    url TEXT NOT NULL DEFAULT '',
    version INTEGER NOT NULL DEFAULT 1,
    outdated INTEGER NOT NULL DEFAULT 0,
    author TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    thread_comment_id INTEGER NOT NULL DEFAULT 0,
    source_id TEXT NOT NULL DEFAULT '',
    answer TEXT NOT NULL DEFAULT '',
    answer_note TEXT NOT NULL DEFAULT '',
    answered_at TEXT NOT NULL DEFAULT '',
    posted INTEGER NOT NULL DEFAULT 0,
    closed INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    delivered INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (task_id, id)
);
INSERT INTO handoff_review_items_per_loop (id, task_id, kind, pass, repo, pr, path, line, url, version, outdated, author, title, body,
        thread_comment_id, source_id, answer, answer_note, answered_at, posted, closed, created_at, delivered)
    SELECT id, task_id, kind, pass, repo, pr, path, line, url, version, outdated, author, title, body,
        thread_comment_id, source_id, answer, answer_note, answered_at, posted, closed, created_at, delivered
    FROM handoff_review_items ORDER BY rowid;
DROP TABLE handoff_review_items;
ALTER TABLE handoff_review_items_per_loop RENAME TO handoff_review_items;
CREATE INDEX IF NOT EXISTS idx_handoff_review_items_task ON handoff_review_items(task_id, pass);

-- A handoff that ended (closed, or returning) while its loop was mid-pass
-- or queued kept a reviewing row, which held a review_passes.max_concurrent
-- slot for good. Such a loop ends now, and its pass in flight is set aside,
-- as ending a handoff does from now on. Loops of workflow runs have no
-- handoff and are left alone.
UPDATE handoff_review_passes SET state = 'discarded', reason = 'the handoff ended', ended_at = strftime('%Y-%m-%dT%H:%M:%SZ', 'now')
    WHERE state IN ('reviewing', 'submitted')
    AND task_id IN (SELECT task_id FROM handoffs)
    AND task_id NOT IN (SELECT task_id FROM handoffs WHERE closed_at IS NULL AND state != 'returning');
UPDATE handoff_reviews SET phase = 'ended', reason = 'closed', queued_at = ''
    WHERE phase IN ('ready', 'waiting_for_reviewer', 'reviewing')
    AND task_id IN (SELECT task_id FROM handoffs)
    AND task_id NOT IN (SELECT task_id FROM handoffs WHERE closed_at IS NULL AND state != 'returning');
