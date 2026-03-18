-- Rename jira-specific columns to provider-agnostic names.
-- SQLite doesn't support ALTER TABLE RENAME COLUMN in older versions,
-- so we recreate the tables.

-- Tickets: jira_status -> remote_status, jira_updated_at -> remote_updated_at
CREATE TABLE tickets_new (
    id                TEXT PRIMARY KEY,
    summary           TEXT NOT NULL,
    description       TEXT,
    description_md    TEXT,
    status            TEXT NOT NULL,
    remote_status     TEXT NOT NULL,
    priority          TEXT,
    issue_type        TEXT,
    assignee          TEXT,
    labels            TEXT,
    epic_key          TEXT,
    epic_name         TEXT,
    url               TEXT,
    created_at        TEXT NOT NULL,
    updated_at        TEXT NOT NULL,
    remote_updated_at TEXT NOT NULL,
    sort_order        INTEGER DEFAULT 0,
    stale_at          TEXT,
    local_move_at     TEXT
);

INSERT INTO tickets_new
SELECT id, summary, description, description_md, status, jira_status,
       priority, issue_type, assignee, labels, epic_key, epic_name, url,
       created_at, updated_at, jira_updated_at, sort_order, stale_at, local_move_at
FROM tickets;

DROP TABLE tickets;
ALTER TABLE tickets_new RENAME TO tickets;

CREATE INDEX idx_tickets_status ON tickets(status);
CREATE INDEX idx_tickets_updated ON tickets(remote_updated_at);

-- Column mappings: jira_statuses -> remote_statuses, jira_transition -> remote_transition
CREATE TABLE column_mappings_new (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    column_name       TEXT NOT NULL UNIQUE,
    remote_statuses   TEXT NOT NULL,
    remote_transition TEXT,
    sort_order        INTEGER DEFAULT 0
);

INSERT INTO column_mappings_new
SELECT id, column_name, jira_statuses, jira_transition, sort_order
FROM column_mappings;

DROP TABLE column_mappings;
ALTER TABLE column_mappings_new RENAME TO column_mappings;
