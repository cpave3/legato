-- Content an agent asked Legato to show to the person: an artifact for the
-- side panel, or a card drawn in the conversation. task_id is the agent's own
-- task identity, which for a swarm worker is its sub-task ID, so it carries no
-- foreign key; Store.DeleteTask removes the rows.
CREATE TABLE IF NOT EXISTS agent_ui_items (
    id           TEXT PRIMARY KEY,
    task_id      TEXT NOT NULL,
    execution_id TEXT NOT NULL DEFAULT '',
    placement    TEXT NOT NULL CHECK (placement IN ('panel', 'inline')),
    item_key     TEXT,
    revision     INTEGER NOT NULL DEFAULT 1,
    created_at   DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at   DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_agent_ui_items_task ON agent_ui_items(task_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_ui_items_key
    ON agent_ui_items(task_id, placement, item_key) WHERE item_key IS NOT NULL;

CREATE TABLE IF NOT EXISTS agent_ui_item_revisions (
    item_id       TEXT NOT NULL,
    revision      INTEGER NOT NULL,
    format        TEXT NOT NULL,
    title         TEXT NOT NULL DEFAULT '',
    content       TEXT NOT NULL,
    fallback_text TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (item_id, revision),
    FOREIGN KEY (item_id) REFERENCES agent_ui_items(id) ON DELETE CASCADE
);
