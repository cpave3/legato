CREATE TABLE artifact_claims (
    artifact_kind    TEXT NOT NULL CHECK (artifact_kind IN ('plan', 'review')),
    artifact_id      TEXT NOT NULL,
    claimant_task_id TEXT NOT NULL,
    generation       INTEGER NOT NULL DEFAULT 1,
    claimed_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at       DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (artifact_kind, artifact_id),
    FOREIGN KEY (claimant_task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE INDEX idx_artifact_claims_claimant
    ON artifact_claims(claimant_task_id, artifact_kind, updated_at);

CREATE TABLE artifact_interactions (
    id                 TEXT PRIMARY KEY,
    artifact_kind      TEXT NOT NULL CHECK (artifact_kind IN ('plan', 'review')),
    artifact_id        TEXT NOT NULL,
    revision_id        TEXT NOT NULL DEFAULT '',
    thread_id          TEXT NOT NULL DEFAULT '',
    kind               TEXT NOT NULL,
    body               TEXT NOT NULL,
    claimant_task_id   TEXT NOT NULL,
    claim_generation   INTEGER NOT NULL,
    attempt_count      INTEGER NOT NULL DEFAULT 0,
    delivered_at       DATETIME,
    acknowledged_at    DATETIME,
    last_error         TEXT NOT NULL DEFAULT '',
    created_at         DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at         DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (claimant_task_id) REFERENCES tasks(id) ON DELETE CASCADE
);
CREATE INDEX idx_artifact_interactions_pending
    ON artifact_interactions(claimant_task_id, acknowledged_at, delivered_at, created_at);
CREATE INDEX idx_artifact_interactions_artifact
    ON artifact_interactions(artifact_kind, artifact_id, created_at);
