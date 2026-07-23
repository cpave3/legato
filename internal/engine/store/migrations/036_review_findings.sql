CREATE TABLE review_findings (
    id          TEXT PRIMARY KEY,
    task_id     TEXT NOT NULL,
    tour_id     TEXT NOT NULL,
    pass_id     TEXT NOT NULL,
    step_id     TEXT NOT NULL DEFAULT '',
    file_path   TEXT NOT NULL DEFAULT '',
    hunk_anchor TEXT NOT NULL DEFAULT '',
    line_start  INTEGER,
    line_end    INTEGER,
    body        TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'open' CHECK(status IN ('open', 'resolved')),
    resolved_at DATETIME,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_review_findings_pass ON review_findings(pass_id, status, created_at);

CREATE TABLE review_plan_requests (
    id           TEXT PRIMARY KEY,
    task_id      TEXT NOT NULL,
    tour_id      TEXT NOT NULL,
    pass_id      TEXT NOT NULL,
    delivered_at DATETIME,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE review_plan_request_findings (
    request_id TEXT NOT NULL,
    finding_id TEXT NOT NULL,
    PRIMARY KEY(request_id, finding_id)
);

CREATE INDEX idx_review_plan_requests_pass ON review_plan_requests(pass_id, created_at);

CREATE TABLE plan_review_origins (
    plan_id        TEXT NOT NULL,
    review_pass_id TEXT NOT NULL,
    finding_id     TEXT NOT NULL,
    created_at     DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY(plan_id, finding_id)
);

CREATE INDEX idx_plan_review_origins_pass ON plan_review_origins(review_pass_id, finding_id);
