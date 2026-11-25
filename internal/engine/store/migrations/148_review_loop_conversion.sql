-- The conversion (LEG-579, spec LEG-559 § Migration, backup and restart):
-- every review loop kept in handoff_review* (one per task: a handed-off
-- card's, or a workflow run's own task's, whose legato.review_loop
-- metadata names the run) is copied onto the loop record. The old tables
-- stay until the engine reads the record (LEG-605, LEG-606). See
-- docs/ai/database.md § Loop record › The conversion.

-- What the old rows carry that the record had no place for: a handoff's
-- review passes turned off by its main (settings.enabled), the main
-- instance of a handoff owner, the reviewer's own branch of each layer in
-- its clone, and how many times a pass's number was pinned before.
ALTER TABLE review_loops ADD COLUMN passes_off INTEGER NOT NULL DEFAULT 0;
ALTER TABLE review_loops ADD COLUMN main_instance_id TEXT NOT NULL DEFAULT '';
ALTER TABLE review_loop_prs ADD COLUMN local_branch TEXT NOT NULL DEFAULT '';
ALTER TABLE review_loop_passes ADD COLUMN attempt INTEGER NOT NULL DEFAULT 0;

-- The conversion.
-- Each old loop with its owner: the run its task's legato.review_loop
-- names, else the task's handoff (the open one, else the newest).
CREATE TEMP TABLE review_conversion_source AS
SELECT r.rowid AS src, r.*,
    CASE WHEN json_valid(t.metadata) AND json_type(t.metadata, '$.legato.review_loop') = 'object'
        THEN json_extract(t.metadata, '$.legato.review_loop') END AS meta,
    CASE WHEN json_valid(r.settings) AND json_type(r.settings) = 'object' THEN r.settings ELSE '{}' END AS settings_json,
    (SELECT h.rowid FROM handoffs h WHERE h.task_id = r.task_id
        ORDER BY h.closed_at IS NULL DESC, h.created_at DESC, h.rowid DESC LIMIT 1) AS handoff_rowid
FROM handoff_reviews r LEFT JOIN tasks t ON t.id = r.task_id;

CREATE TEMP TABLE review_conversion_loops AS
SELECT s.src, s.task_id,
    'rl-' || lower(hex(randomblob(6))) AS id,
    CASE WHEN COALESCE(json_extract(s.meta, '$.run_id'), '') != '' THEN 'run' ELSE 'handoff' END AS owner_kind,
    COALESCE(json_extract(s.meta, '$.run_id'), '') AS run_id,
    COALESCE(json_extract(s.meta, '$.step_id'), '') AS step_path,
    COALESCE(json_extract(s.meta, '$.attempt_id'), '') AS attempt_id,
    s.meta, s.settings_json,
    h.handoff_id, h.main_instance_id, h.repo AS handoff_repo, h.created_at AS handoff_created_at, h.closed_at AS handoff_closed_at,
    w.id AS wr_id, w.status AS run_status, w.created_at AS run_created_at, w.completed_at AS run_completed_at,
    st.status AS step_status,
    s.phase, s.reason, s.pass, s.mismatches, s.nudged_at, s.author_busy_since_nudge, s.queued_at, s.updated_at,
    s.delivered_pass, s.polled_at, s.last_new_at, s.skipped_comments, s.checks_polled_at, s.checks_heads, s.checks_pending
FROM review_conversion_source s
LEFT JOIN handoffs h ON h.rowid = s.handoff_rowid
LEFT JOIN workflow_runs w ON w.id = json_extract(s.meta, '$.run_id')
LEFT JOIN workflow_steps st ON st.run_id = json_extract(s.meta, '$.run_id') AND st.path = json_extract(s.meta, '$.step_id');

-- A run's loop owns nothing of a handoff; a handoff's loop has no run.
UPDATE review_conversion_loops SET handoff_id = NULL, main_instance_id = NULL WHERE owner_kind = 'run';

ALTER TABLE review_conversion_loops ADD COLUMN repo TEXT;
ALTER TABLE review_conversion_loops ADD COLUMN agent_author INTEGER;
ALTER TABLE review_conversion_loops ADD COLUMN author_task TEXT;
ALTER TABLE review_conversion_loops ADD COLUMN opened_at TEXT;
ALTER TABLE review_conversion_loops ADD COLUMN closed INTEGER;
ALTER TABLE review_conversion_loops ADD COLUMN outcome TEXT;
ALTER TABLE review_conversion_loops ADD COLUMN closed_at TEXT;
ALTER TABLE review_conversion_loops ADD COLUMN lost INTEGER NOT NULL DEFAULT 0;
ALTER TABLE review_conversion_loops ADD COLUMN confirmed INTEGER NOT NULL DEFAULT 0;

UPDATE review_conversion_loops SET
    repo = COALESCE(NULLIF(json_extract(meta, '$.repo'), ''), NULLIF(handoff_repo, ''),
        (SELECT p.repo FROM handoff_review_prs p WHERE p.task_id = review_conversion_loops.task_id ORDER BY p.pass DESC, p.rowid LIMIT 1), ''),
    agent_author = CASE WHEN owner_kind = 'run' THEN COALESCE(json_extract(meta, '$.author_task'), '') != ''
        ELSE handoff_id IS NOT NULL END,
    author_task = CASE WHEN owner_kind = 'run' THEN COALESCE(json_extract(meta, '$.author_task'), '')
        WHEN handoff_id IS NOT NULL THEN task_id ELSE '' END,
    -- When the loop opened, for "the newest": its run's start, else its
    -- handoff's, else its first pass, else its last change.
    opened_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', CASE WHEN owner_kind = 'run' THEN run_created_at ELSE handoff_created_at END),
        (SELECT strftime('%Y-%m-%dT%H:%M:%SZ', MIN(p.started_at)) FROM handoff_review_passes p WHERE p.task_id = review_conversion_loops.task_id),
        strftime('%Y-%m-%dT%H:%M:%SZ', updated_at), strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    -- Closed: ended, or its owner is gone or done. A failed run keeps its
    -- loop for Retry (D1).
    closed = phase = 'ended' OR CASE WHEN owner_kind = 'run'
        THEN wr_id IS NULL OR run_status IN ('succeeded', 'cancelled', 'stopped') OR COALESCE(step_status, '') IN ('succeeded', 'skipped', 'cancelled')
        ELSE handoff_id IS NULL OR handoff_closed_at IS NOT NULL END;

-- Approved only when it was (invariant 9): clean, no longer waiting for a
-- confirm of GitHub's heads (the run's confirm_clean), and closed by its
-- review step's success. A run cancelled or stopped after its clean pass
-- posted, before or after that confirm, ended otherwise; and the older
-- Legato never confirmed a handoff's heads, so a handoff's loop never
-- converts approved. An open clean loop converts unconfirmed: the engine's
-- Confirm reads the heads.
UPDATE review_conversion_loops SET
    confirmed = closed = 1 AND phase = 'clean' AND owner_kind = 'run' AND COALESCE(json_extract(meta, '$.confirm_clean'), 0) = 0
        AND wr_id IS NOT NULL AND run_status NOT IN ('cancelled', 'stopped')
        AND (step_status = 'succeeded' OR (step_status IS NULL AND run_status = 'succeeded'));

UPDATE review_conversion_loops SET
    outcome = CASE WHEN closed = 0 THEN ''
        WHEN phase = 'ended' THEN COALESCE(NULLIF(reason, ''), 'ended')
        WHEN confirmed = 1 THEN 'approved'
        WHEN owner_kind = 'run' AND (run_status IN ('cancelled', 'stopped') OR COALESCE(step_status, '') = 'cancelled') THEN 'cancelled'
        ELSE 'ended' END,
    closed_at = CASE WHEN closed = 0 THEN '' ELSE COALESCE(
        strftime('%Y-%m-%dT%H:%M:%SZ', CASE WHEN owner_kind = 'run' THEN run_completed_at ELSE handoff_closed_at END),
        strftime('%Y-%m-%dT%H:%M:%SZ', updated_at), strftime('%Y-%m-%dT%H:%M:%SZ', 'now')) END;

-- One open loop per run, and one open agent-authored loop per author: the
-- newest stays open, an older one closes stuck, naming it.
UPDATE review_conversion_loops SET closed = 1, phase = 'stuck', outcome = 'stuck', queued_at = '', closed_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', updated_at), opened_at),
    reason = 'Legato upgraded with two open review loops of run ' || run_id || ': ' ||
        (SELECT o.id FROM review_conversion_loops o WHERE o.closed = 0 AND o.owner_kind = 'run' AND o.run_id = review_conversion_loops.run_id
            ORDER BY o.opened_at DESC, o.src DESC LIMIT 1) || ', the newer, carries on'
WHERE closed = 0 AND owner_kind = 'run' AND EXISTS (SELECT 1 FROM review_conversion_loops o
    WHERE o.closed = 0 AND o.owner_kind = 'run' AND o.run_id = review_conversion_loops.run_id
    AND (o.opened_at > review_conversion_loops.opened_at OR (o.opened_at = review_conversion_loops.opened_at AND o.src > review_conversion_loops.src)));
UPDATE review_conversion_loops SET closed = 1, phase = 'stuck', outcome = 'stuck', queued_at = '', closed_at = COALESCE(strftime('%Y-%m-%dT%H:%M:%SZ', updated_at), opened_at),
    reason = 'Legato upgraded with two open review loops authored by ' || author_task || '''s agent: ' ||
        (SELECT o.id FROM review_conversion_loops o WHERE o.closed = 0 AND o.agent_author = 1 AND o.author_task = review_conversion_loops.author_task
            ORDER BY o.opened_at DESC, o.src DESC LIMIT 1) || ', the newer, carries on'
WHERE closed = 0 AND agent_author = 1 AND author_task != '' AND EXISTS (SELECT 1 FROM review_conversion_loops o
    WHERE o.closed = 0 AND o.agent_author = 1 AND o.author_task = review_conversion_loops.author_task
    AND (o.opened_at > review_conversion_loops.opened_at OR (o.opened_at = review_conversion_loops.opened_at AND o.src > review_conversion_loops.src)));

-- Each loop's layers, bottom first: a run's stack (or its one pull
-- request) from its metadata, with the reviewer's branch of each; else the
-- pull requests its latest pass pinned.
CREATE TEMP TABLE review_conversion_prs (
    loop_id TEXT NOT NULL,
    number INTEGER NOT NULL,
    position INTEGER NOT NULL,
    local_branch TEXT NOT NULL,
    PRIMARY KEY (loop_id, number)
);
INSERT OR IGNORE INTO review_conversion_prs (loop_id, number, position, local_branch)
SELECT l.id, CAST(json_extract(j.value, '$.number') AS INTEGER), CAST(j.key AS INTEGER), COALESCE(json_extract(j.value, '$.branch'), '')
FROM review_conversion_loops l, json_each(l.meta, '$.stack') j
WHERE json_type(l.meta, '$.stack') = 'array' AND json_type(j.value) = 'object' AND CAST(json_extract(j.value, '$.number') AS INTEGER) > 0;
INSERT OR IGNORE INTO review_conversion_prs (loop_id, number, position, local_branch)
SELECT l.id, CAST(json_extract(l.meta, '$.pr') AS INTEGER), 0, COALESCE(json_extract(l.meta, '$.branch'), '')
FROM review_conversion_loops l
WHERE CAST(COALESCE(json_extract(l.meta, '$.pr'), 0) AS INTEGER) > 0
    AND NOT EXISTS (SELECT 1 FROM review_conversion_prs c WHERE c.loop_id = l.id);
INSERT OR IGNORE INTO review_conversion_prs (loop_id, number, position, local_branch)
SELECT l.id, p.number, ROW_NUMBER() OVER (PARTITION BY l.id ORDER BY p.rowid) - 1, p.local_branch
FROM review_conversion_loops l JOIN handoff_review_prs p ON p.task_id = l.task_id
    AND p.pass = (SELECT MAX(q.pass) FROM handoff_review_prs q WHERE q.task_id = l.task_id)
WHERE NOT EXISTS (SELECT 1 FROM review_conversion_prs c WHERE c.loop_id = l.id);

-- Claims: among open loops, the newest on a pull request (the
-- repository's case ignored) holds it.
CREATE TEMP TABLE review_conversion_claims AS
SELECT c.loop_id, c.number, c.position, l.repo, l.opened_at,
    FIRST_VALUE(c.loop_id) OVER newest AS holder,
    ROW_NUMBER() OVER newest AS rank
FROM review_conversion_prs c JOIN review_conversion_loops l ON l.id = c.loop_id
WHERE l.closed = 0
WINDOW newest AS (PARTITION BY lower(l.repo), c.number ORDER BY l.opened_at DESC, l.src DESC);

-- An older loop on a pull request another holds is stuck, naming it.
UPDATE review_conversion_loops SET lost = 1, phase = 'stuck', queued_at = '',
    reason = (SELECT c.repo || '#' || c.number || ' is held by review loop ' || c.holder ||
            ', the newer loop on it when Legato upgraded: End this one, or End that one and run this review again'
        FROM review_conversion_claims c WHERE c.loop_id = review_conversion_loops.id AND c.rank > 1 ORDER BY c.position LIMIT 1)
WHERE id IN (SELECT loop_id FROM review_conversion_claims WHERE rank > 1);

INSERT INTO review_loops (id, rev, owner_kind, run_id, step_path, attempt_id, handoff_id, main_instance_id, host_task, repo,
    subject_pr, scope, agent_author, author_task, linked_task, state, phase, outcome, reason, passes_off,
    max_passes, reviewer, reviewer_model, reviewer_tier, skill, skill_managed,
    pass, window_start, mismatches, delivered_pass, confirmed_at, clean_pass, confirm_clean, nudged_at, author_busy_since_nudge, seen_head, seen_at, replies,
    polled_at, last_new_at, skipped_comments, checks_polled_at, checks_heads, checks_pending,
    marker_key, clone_id, clone_dir, queued_at, created_at, updated_at, closed_at)
SELECT l.id, 1, l.owner_kind, l.run_id, l.step_path, l.attempt_id, COALESCE(l.handoff_id, ''), COALESCE(l.main_instance_id, ''), l.task_id, l.repo,
    CAST(COALESCE(json_extract(l.meta, '$.pr'), 0) AS INTEGER), COALESCE(json_extract(l.meta, '$.scope'), ''),
    l.agent_author, l.author_task,
    CASE WHEN l.owner_kind = 'run' THEN COALESCE(json_extract(l.meta, '$.linked_task'), '') WHEN l.handoff_id IS NOT NULL THEN l.task_id ELSE '' END,
    CASE WHEN l.closed THEN 'closed' ELSE 'open' END, l.phase, l.outcome, l.reason,
    COALESCE(json_extract(l.settings_json, '$.enabled'), 1) = 0,
    CAST(COALESCE(json_extract(l.settings_json, '$.max_passes'), 0) AS INTEGER),
    COALESCE(json_extract(l.settings_json, '$.agent'), ''), COALESCE(json_extract(l.settings_json, '$.model'), ''),
    COALESCE(json_extract(l.settings_json, '$.tier'), ''), COALESCE(json_extract(l.settings_json, '$.skill'), ''),
    COALESCE(json_extract(l.settings_json, '$.skill_managed'), 0) != 0,
    l.pass, CASE WHEN l.confirmed THEN l.pass ELSE 0 END, l.mismatches, l.delivered_pass,
    CASE WHEN l.confirmed THEN l.closed_at ELSE '' END, CASE WHEN l.confirmed THEN l.pass ELSE 0 END,
    CASE WHEN l.closed = 0 AND l.phase = 'clean' THEN 1 ELSE COALESCE(json_extract(l.meta, '$.confirm_clean'), 0) != 0 END,
    l.nudged_at, l.author_busy_since_nudge,
    COALESCE(json_extract(l.meta, '$.seen_head'), ''), COALESCE(json_extract(l.meta, '$.seen_at'), ''),
    CASE WHEN json_type(l.meta, '$.replies') = 'array' THEN json_extract(l.meta, '$.replies') ELSE '[]' END,
    l.polled_at, l.last_new_at, l.skipped_comments, l.checks_polled_at, l.checks_heads, l.checks_pending,
    l.task_id, CAST(COALESCE(json_extract(l.meta, '$.clone_id'), 0) AS INTEGER), COALESCE(json_extract(l.meta, '$.dir'), ''),
    CASE WHEN l.closed THEN '' ELSE l.queued_at END, l.opened_at, l.updated_at, l.closed_at
FROM review_conversion_loops l ORDER BY l.src;

-- The layers, each with what its latest pass pinned and the latest review
-- Legato posted on it.
INSERT INTO review_loop_prs (loop_id, number, position, branch, base, title, url, head_repo, state, author, author_task,
    pinned_head, clean_head, review_id, review_url, claimed_at, local_branch)
SELECT c.loop_id, c.number, c.position, COALESCE(p.branch, ''), COALESCE(p.base, ''), COALESCE(p.title, ''), COALESCE(p.url, ''),
    COALESCE(p.head_repo, ''), 'open', COALESCE(p.author, ''), CASE WHEN l.agent_author THEN l.author_task ELSE '' END,
    COALESCE(p.head_sha, ''),
    CASE WHEN l.confirmed THEN COALESCE(p.head_sha, '') ELSE '' END,
    COALESCE(rv.review_id, 0), COALESCE(rv.review_url, ''),
    CASE WHEN l.closed = 0 AND EXISTS (SELECT 1 FROM review_conversion_claims k WHERE k.loop_id = c.loop_id AND k.number = c.number AND k.rank = 1)
        THEN l.opened_at ELSE '' END,
    CASE WHEN c.local_branch != '' THEN c.local_branch ELSE COALESCE(p.local_branch, '') END
FROM review_conversion_prs c JOIN review_conversion_loops l ON l.id = c.loop_id
LEFT JOIN handoff_review_prs p ON p.task_id = l.task_id AND p.number = c.number
    AND p.pass = (SELECT MAX(q.pass) FROM handoff_review_prs q WHERE q.task_id = l.task_id AND q.number = c.number)
LEFT JOIN handoff_review_prs rv ON rv.task_id = l.task_id AND rv.number = c.number
    AND rv.pass = (SELECT MAX(q.pass) FROM handoff_review_prs q WHERE q.task_id = l.task_id AND q.number = c.number AND q.review_id != 0);

INSERT INTO review_loop_claims (repo, pr, loop_id, claimed_at)
SELECT repo, number, loop_id, opened_at FROM review_conversion_claims WHERE rank = 1;

-- The passes, each with its pins. A pass in flight stays in flight only as
-- the newest of an open loop that holds its pull requests; any other is
-- set aside, as closing a loop does.
INSERT INTO review_loop_passes (loop_id, pass, step_path, state, verdict, summary, reason, execution_id, checkout, head_sha, pins,
    posting_started_at, attempts, attempt, started_at, ended_at)
SELECT l.id, p.pass, l.step_path,
    CASE WHEN p.state IN ('reviewing', 'submitted') AND (l.closed = 1 OR l.lost = 1 OR p.pass != (SELECT MAX(q.pass) FROM handoff_review_passes q
            WHERE q.task_id = p.task_id AND q.state IN ('reviewing', 'submitted'))) THEN 'discarded'
        WHEN p.state IN ('reviewing', 'submitted', 'posting', 'posted', 'discarded', 'unable') THEN p.state
        ELSE 'discarded' END,
    p.verdict, p.summary,
    CASE WHEN p.state IN ('reviewing', 'submitted') AND (l.closed = 1 OR l.lost = 1 OR p.pass != (SELECT MAX(q.pass) FROM handoff_review_passes q
            WHERE q.task_id = p.task_id AND q.state IN ('reviewing', 'submitted'))) THEN 'set aside when Legato upgraded: its loop holds no claim to post it'
        ELSE p.reason END,
    p.execution_id, p.checkout, p.head_sha,
    (SELECT json_group_array(json_object('number', x.number, 'repo', x.repo, 'branch', x.branch, 'base', x.base, 'title', x.title,
            'body', x.body, 'url', x.url, 'head_sha', x.head_sha, 'author', x.author, 'local_branch', x.local_branch,
            'head_repo', x.head_repo, 'review_id', x.review_id, 'review_url', x.review_url, 'event', x.event))
        FROM (SELECT * FROM handoff_review_prs x WHERE x.task_id = p.task_id AND x.pass = p.pass ORDER BY x.rowid) x),
    p.posting_started_at, p.post_attempts, p.attempt, p.started_at,
    CASE WHEN p.state IN ('reviewing', 'submitted') AND p.ended_at = '' AND (l.closed = 1 OR l.lost = 1 OR p.pass != (SELECT MAX(q.pass) FROM handoff_review_passes q
            WHERE q.task_id = p.task_id AND q.state IN ('reviewing', 'submitted'))) THEN COALESCE(NULLIF(l.closed_at, ''), strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
        ELSE p.ended_at END
FROM handoff_review_passes p JOIN review_conversion_loops l ON l.task_id = p.task_id
ORDER BY p.task_id, p.pass;

INSERT INTO review_loop_findings (loop_id, id, pass, pr, severity, title, body, path, line, start_line, side, comment_id, status, ruled_pass, ruling)
SELECT l.id, f.id, f.pass, f.pr, f.severity, f.title, f.body, f.path, f.line, f.start_line, f.side, f.comment_id, f.status, f.ruled_pass, f.ruling
FROM handoff_review_findings f JOIN review_conversion_loops l ON l.task_id = f.task_id
ORDER BY f.rowid;

-- Items keep their answers, posted or not, and their markers: an answer
-- posted before the upgrade is not posted again, and one not yet posted is
-- looked for on GitHub by its marker first.
INSERT INTO review_loop_items (loop_id, id, kind, pass, repo, pr, path, line, url, version, outdated, author, title, body,
    thread_comment_id, source_id, answer, answer_note, answered_at, answer_marker, posted, closed, delivered, created_at)
SELECT l.id, i.id,
    CASE WHEN i.kind IN ('blocking', 'nit', 'comment', 'check', 'mergeability', 'merged') THEN i.kind ELSE 'comment' END,
    i.pass, i.repo, i.pr, i.path, i.line, i.url, i.version, i.outdated, i.author, i.title, i.body,
    i.thread_comment_id, i.source_id, i.answer, i.answer_note, i.answered_at,
    CASE WHEN i.answer != '' AND i.answer_marker = '' THEN lower(hex(randomblob(8))) ELSE i.answer_marker END,
    i.posted, i.closed, i.delivered, i.created_at
FROM handoff_review_items i JOIN review_conversion_loops l ON l.task_id = i.task_id
ORDER BY i.rowid;

INSERT INTO review_loop_poll_pages (loop_id, pr, list, page, etag, body)
SELECT l.id, g.pr, g.list, g.page, g.etag, g.body
FROM handoff_review_poll_pages g JOIN review_conversion_loops l ON l.task_id = g.task_id;

DROP TABLE review_conversion_claims;
DROP TABLE review_conversion_prs;
DROP TABLE review_conversion_loops;
DROP TABLE review_conversion_source;
