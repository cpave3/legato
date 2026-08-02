-- Migration 049 existed briefly during development before learning_proposals was
-- added. Create its final base table when upgrading one of those databases,
-- then add the revision snapshot column shared by every schema path.
CREATE TABLE IF NOT EXISTS learning_proposals (
    id TEXT PRIMARY KEY,
    generation_id TEXT NOT NULL,
    operation TEXT NOT NULL,
    learning_id TEXT NOT NULL DEFAULT '',
    canonical_learning_id TEXT NOT NULL DEFAULT '',
    scope TEXT NOT NULL DEFAULT '',
    project_key TEXT NOT NULL DEFAULT '',
    evidence_ids_json TEXT NOT NULL DEFAULT '[]',
    reason TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'pending',
    revision INTEGER NOT NULL DEFAULT 1,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (generation_id) REFERENCES learning_curation_generations(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_learning_proposals_status ON learning_proposals(status, created_at);
CREATE INDEX IF NOT EXISTS idx_learning_proposals_learning ON learning_proposals(learning_id, status);

ALTER TABLE learning_proposals
ADD COLUMN target_learning_revision INTEGER NOT NULL DEFAULT 0;
