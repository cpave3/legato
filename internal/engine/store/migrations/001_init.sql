CREATE TABLE tickets (
    id              TEXT PRIMARY KEY,
    summary         TEXT NOT NULL,
    description     TEXT,
    description_md  TEXT,
    status          TEXT NOT NULL,
    jira_status     TEXT NOT NULL,
    priority        TEXT,
    issue_type      TEXT,
    assignee        TEXT,
    labels          TEXT,
    epic_key        TEXT,
    epic_name       TEXT,
    url             TEXT,
    created_at      TEXT NOT NULL,
    updated_at      TEXT NOT NULL,
    jira_updated_at TEXT NOT NULL,
    sort_order      INTEGER DEFAULT 0
);

CREATE TABLE column_mappings (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    column_name     TEXT NOT NULL UNIQUE,
    jira_statuses   TEXT NOT NULL,
    jira_transition TEXT,
    sort_order      INTEGER DEFAULT 0
);

CREATE TABLE sync_log (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    ticket_id       TEXT NOT NULL,
    action          TEXT NOT NULL,
    detail          TEXT,
    created_at      TEXT NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX idx_tickets_status ON tickets(status);
CREATE INDEX idx_tickets_updated ON tickets(jira_updated_at);
