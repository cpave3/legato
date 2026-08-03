CREATE TABLE IF NOT EXISTS learning_embeddings (
    learning_id TEXT PRIMARY KEY,
    revision INTEGER NOT NULL,
    content_digest TEXT NOT NULL,
    provider TEXT NOT NULL DEFAULT '',
    model TEXT NOT NULL,
    dimensions INTEGER NOT NULL,
    vector_json TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL DEFAULT 'pending',
    error TEXT NOT NULL DEFAULT '',
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (learning_id) REFERENCES learnings(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_learning_embeddings_model_status ON learning_embeddings(model, status);
