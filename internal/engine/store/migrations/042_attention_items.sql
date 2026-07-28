CREATE TABLE attention_items (
    id                   TEXT PRIMARY KEY,
    producer             TEXT NOT NULL,
    source_key           TEXT NOT NULL,
    source_revision      TEXT NOT NULL,
    task_id              TEXT NOT NULL DEFAULT '',
    workspace_id         INTEGER,
    title                TEXT NOT NULL,
    reason               TEXT NOT NULL,
    detail               TEXT NOT NULL DEFAULT '',
    category             TEXT NOT NULL,
    urgency              TEXT NOT NULL,
    navigation_json      TEXT NOT NULL,
    actions_json         TEXT NOT NULL,
    status               TEXT NOT NULL CHECK (status IN ('open', 'dismissed', 'resolved')),
    dismissed_revision   TEXT NOT NULL DEFAULT '',
    created_at           DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at           DATETIME NOT NULL DEFAULT (datetime('now')),
    resolved_at          DATETIME,
    UNIQUE (producer, source_key)
);

CREATE INDEX idx_attention_items_active
    ON attention_items(status, urgency, updated_at);
CREATE INDEX idx_attention_items_producer
    ON attention_items(producer, status, source_key);
CREATE INDEX idx_attention_items_task
    ON attention_items(task_id, status, updated_at);
