ALTER TABLE learnings ADD COLUMN category TEXT NOT NULL DEFAULT '';

CREATE TABLE IF NOT EXISTS learning_tags (
    learning_id TEXT NOT NULL,
    tag TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (learning_id, tag),
    FOREIGN KEY (learning_id) REFERENCES learnings(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_learning_tags_tag ON learning_tags(tag, learning_id);
