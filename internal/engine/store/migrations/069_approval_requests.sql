-- Approval requests: each request that needs the person (an agent's permission
-- or question, and later a credential request), kept until it is decided,
-- withdrawn, dismissed or expired. A source names one request by its source
-- key, so a request seen twice is stored once.
CREATE TABLE IF NOT EXISTS approval_requests (
    id            TEXT PRIMARY KEY,
    source        TEXT NOT NULL CHECK (source != ''),
    source_key    TEXT NOT NULL CHECK (source_key != ''),
    task_id       TEXT NOT NULL DEFAULT '',
    execution_id  TEXT NOT NULL DEFAULT '',
    title         TEXT NOT NULL,
    summary       TEXT NOT NULL DEFAULT '',
    details_json  TEXT NOT NULL DEFAULT '{}',
    options_json  TEXT NOT NULL DEFAULT '[]',
    blocking      INTEGER NOT NULL DEFAULT 1,
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN
                  ('pending', 'approved', 'denied', 'answered', 'dismissed', 'withdrawn', 'expired', 'failed')),
    decision_json TEXT NOT NULL DEFAULT '',
    decided_by    TEXT NOT NULL DEFAULT '',
    error         TEXT NOT NULL DEFAULT '',
    revision      INTEGER NOT NULL DEFAULT 1,
    created_at    DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at    DATETIME NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    decided_at    DATETIME,
    UNIQUE (source, source_key)
);

CREATE INDEX IF NOT EXISTS approval_requests_status ON approval_requests (status, created_at);
CREATE INDEX IF NOT EXISTS approval_requests_task ON approval_requests (task_id, status);
