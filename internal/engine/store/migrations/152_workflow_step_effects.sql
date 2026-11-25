-- What a workflow step made outside Legato (LEG-640): a ticket.create
-- step's ticket, a ticket.comment step's comment. A retry is a new attempt
-- of the same step instance, so the record is keyed by the run and the
-- step's path, and a retry finds what an earlier attempt made instead of
-- making it again. pending: an attempt began to make it and did not learn
-- whether it did; done: it is made.
CREATE TABLE IF NOT EXISTS workflow_step_effects (
    run_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    path TEXT NOT NULL,
    effect TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    state TEXT NOT NULL,
    provider TEXT NOT NULL,
    -- The project a ticket is made in, or the ticket a comment is added to.
    target TEXT NOT NULL DEFAULT '',
    -- What a pending ticket was to be called, to find it again.
    summary TEXT NOT NULL DEFAULT '',
    key TEXT NOT NULL DEFAULT '',
    remote_id TEXT NOT NULL DEFAULT '',
    url TEXT NOT NULL DEFAULT '',
    started_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY (run_id, path, effect)
);
