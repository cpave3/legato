-- The loop record (LEG-578, spec LEG-559 § Loop record schema and claims):
-- the review loop engine's own record of each review loop, `rl-` and 12 hex
-- characters, owned by the workflow run (or, until handoffs run review-loop,
-- the handoff) that opened it. Nothing writes these tables yet: a later
-- migration converts handoff_review* into them. See docs/ai/database.md
-- § Loop record.
CREATE TABLE IF NOT EXISTS review_loops (
    id TEXT PRIMARY KEY,
    -- Every write is a compare-and-swap on rev.
    rev INTEGER NOT NULL DEFAULT 1,
    -- The owner: a workflow run's step (run_id, step_path, attempt_id), or a
    -- handoff (handoff_id).
    owner_kind TEXT NOT NULL CHECK (owner_kind IN ('run', 'handoff')),
    run_id TEXT NOT NULL DEFAULT '',
    step_path TEXT NOT NULL DEFAULT '',
    attempt_id TEXT NOT NULL DEFAULT '',
    -- The handoff binding: the handoff and its card's columns (JSON).
    handoff_id TEXT NOT NULL DEFAULT '',
    handoff_columns TEXT NOT NULL DEFAULT '',
    -- The task that holds the reviewer slot and the clone.
    host_task TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    repo TEXT NOT NULL,
    -- The subject: the pull request named (0 when a list was), and the scope
    -- (stack, pr; '' for a handoff's).
    subject_pr INTEGER NOT NULL DEFAULT 0,
    scope TEXT NOT NULL DEFAULT '',
    agent_author INTEGER NOT NULL DEFAULT 0,
    author_task TEXT NOT NULL DEFAULT '',
    linked_task TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'closed')),
    phase TEXT NOT NULL DEFAULT '',
    outcome TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    -- The settings.
    max_passes INTEGER NOT NULL DEFAULT 0,
    max_reentries INTEGER NOT NULL DEFAULT 0,
    ready INTEGER NOT NULL DEFAULT 0,
    steward INTEGER NOT NULL DEFAULT 0,
    reviewer TEXT NOT NULL DEFAULT '',
    reviewer_model TEXT NOT NULL DEFAULT '',
    reviewer_tier TEXT NOT NULL DEFAULT '',
    skill TEXT NOT NULL DEFAULT '',
    skill_managed INTEGER NOT NULL DEFAULT 0,
    -- The counters.
    pass INTEGER NOT NULL DEFAULT 0,
    window_start INTEGER NOT NULL DEFAULT 0,
    reentries INTEGER NOT NULL DEFAULT 0,
    passes_since_approval INTEGER NOT NULL DEFAULT 0,
    pass_deaths INTEGER NOT NULL DEFAULT 0,
    not_converging INTEGER NOT NULL DEFAULT 0,
    mismatches INTEGER NOT NULL DEFAULT 0,
    delivered_pass INTEGER NOT NULL DEFAULT 0,
    last_wake TEXT NOT NULL DEFAULT '',
    last_wake_at TEXT NOT NULL DEFAULT '',
    held INTEGER NOT NULL DEFAULT 0,
    idle_deadline TEXT NOT NULL DEFAULT '',
    confirmed_at TEXT NOT NULL DEFAULT '',
    clean_pass INTEGER NOT NULL DEFAULT 0,
    confirm_clean INTEGER NOT NULL DEFAULT 0,
    -- The author's nudge, and the head the loop last saw move.
    nudged_at TEXT NOT NULL DEFAULT '',
    author_busy_since_nudge INTEGER NOT NULL DEFAULT 0,
    seen_head TEXT NOT NULL DEFAULT '',
    seen_at TEXT NOT NULL DEFAULT '',
    -- Thread replies taken as answers (JSON list of comment IDs).
    replies TEXT NOT NULL DEFAULT '[]',
    -- The comment and check polls.
    polled_at TEXT NOT NULL DEFAULT '',
    last_new_at TEXT NOT NULL DEFAULT '',
    skipped_comments INTEGER NOT NULL DEFAULT 0,
    checks_polled_at TEXT NOT NULL DEFAULT '',
    checks_heads TEXT NOT NULL DEFAULT '',
    checks_pending INTEGER NOT NULL DEFAULT 0,
    -- Markers and pin refs (the old task ID for a converted loop).
    marker_key TEXT NOT NULL,
    clone_id INTEGER NOT NULL DEFAULT 0,
    clone_dir TEXT NOT NULL DEFAULT '',
    queued_at TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    closed_at TEXT NOT NULL DEFAULT ''
);
-- One open agent-authored loop per author.
CREATE UNIQUE INDEX IF NOT EXISTS idx_review_loops_open_author ON review_loops(author_task)
    WHERE state = 'open' AND agent_author = 1;
-- A workflow run has at most one open loop; a step opens one loop.
CREATE UNIQUE INDEX IF NOT EXISTS idx_review_loops_open_run ON review_loops(run_id)
    WHERE state = 'open' AND owner_kind = 'run';
CREATE INDEX IF NOT EXISTS idx_review_loops_step ON review_loops(run_id, step_path);
CREATE INDEX IF NOT EXISTS idx_review_loops_state ON review_loops(state);

-- The pull requests of a loop, its layers in stack order.
CREATE TABLE IF NOT EXISTS review_loop_prs (
    loop_id TEXT NOT NULL REFERENCES review_loops(id) ON DELETE CASCADE,
    number INTEGER NOT NULL,
    position INTEGER NOT NULL,
    branch TEXT NOT NULL DEFAULT '',
    base TEXT NOT NULL DEFAULT '',
    title TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    head_repo TEXT NOT NULL DEFAULT '',
    -- open, closed, merged
    state TEXT NOT NULL DEFAULT 'open',
    draft INTEGER NOT NULL DEFAULT 0,
    mergeable TEXT NOT NULL DEFAULT '',
    author TEXT NOT NULL DEFAULT '',
    author_association TEXT NOT NULL DEFAULT '',
    author_task TEXT NOT NULL DEFAULT '',
    pinned_head TEXT NOT NULL DEFAULT '',
    clean_head TEXT NOT NULL DEFAULT '',
    review_id INTEGER NOT NULL DEFAULT 0,
    review_url TEXT NOT NULL DEFAULT '',
    claimed_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (loop_id, number)
);

-- Who holds each pull request: one loop, from open to close.
CREATE TABLE IF NOT EXISTS review_loop_claims (
    repo TEXT NOT NULL COLLATE NOCASE,
    pr INTEGER NOT NULL,
    loop_id TEXT NOT NULL REFERENCES review_loops(id) ON DELETE CASCADE,
    claimed_at TEXT NOT NULL,
    PRIMARY KEY (repo, pr)
);
CREATE INDEX IF NOT EXISTS idx_review_loop_claims_loop ON review_loop_claims(loop_id);

-- A loop's passes. pins is the JSON list of each pull request's pin.
CREATE TABLE IF NOT EXISTS review_loop_passes (
    loop_id TEXT NOT NULL REFERENCES review_loops(id) ON DELETE CASCADE,
    pass INTEGER NOT NULL,
    pass_window INTEGER NOT NULL DEFAULT 0,
    step_path TEXT NOT NULL DEFAULT '',
    state TEXT NOT NULL CHECK (state IN ('reviewing', 'submitted', 'posting', 'posted', 'discarded', 'unable')),
    verdict TEXT NOT NULL DEFAULT '',
    summary TEXT NOT NULL DEFAULT '',
    reason TEXT NOT NULL DEFAULT '',
    execution_id TEXT NOT NULL DEFAULT '',
    checkout TEXT NOT NULL DEFAULT '',
    head_sha TEXT NOT NULL DEFAULT '',
    pins TEXT NOT NULL DEFAULT '[]',
    submission_digest TEXT NOT NULL DEFAULT '',
    posting_started_at TEXT NOT NULL DEFAULT '',
    attempts INTEGER NOT NULL DEFAULT 0,
    started_at TEXT NOT NULL,
    ended_at TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (loop_id, pass)
);
-- At most one pass in flight per loop.
CREATE UNIQUE INDEX IF NOT EXISTS idx_review_loop_passes_in_flight ON review_loop_passes(loop_id)
    WHERE state IN ('reviewing', 'submitted', 'posting');

CREATE TABLE IF NOT EXISTS review_loop_findings (
    loop_id TEXT NOT NULL REFERENCES review_loops(id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    pass INTEGER NOT NULL,
    pr INTEGER NOT NULL,
    severity TEXT NOT NULL CHECK (severity IN ('blocking', 'nit')),
    critical INTEGER NOT NULL DEFAULT 0,
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
    PRIMARY KEY (loop_id, id)
);
CREATE INDEX IF NOT EXISTS idx_review_loop_findings_pass ON review_loop_findings(loop_id, pass);

-- What the author must handle, and its answer.
CREATE TABLE IF NOT EXISTS review_loop_items (
    loop_id TEXT NOT NULL REFERENCES review_loops(id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('blocking', 'nit', 'comment', 'check', 'mergeability', 'merged')),
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
    answer_marker TEXT NOT NULL DEFAULT '',
    posted INTEGER NOT NULL DEFAULT 0,
    closed INTEGER NOT NULL DEFAULT 0,
    delivered INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    PRIMARY KEY (loop_id, id)
);
CREATE INDEX IF NOT EXISTS idx_review_loop_items_pass ON review_loop_items(loop_id, pass);

CREATE TABLE IF NOT EXISTS review_loop_poll_pages (
    loop_id TEXT NOT NULL REFERENCES review_loops(id) ON DELETE CASCADE,
    pr INTEGER NOT NULL,
    list TEXT NOT NULL,
    page INTEGER NOT NULL,
    etag TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (loop_id, pr, list, page)
);
