CREATE TABLE IF NOT EXISTS learning_evidence (
    id TEXT PRIMARY KEY,
    source_kind TEXT NOT NULL,
    source_ref TEXT NOT NULL,
    content TEXT NOT NULL,
    content_digest TEXT NOT NULL,
    author TEXT NOT NULL,
    task_id TEXT NOT NULL DEFAULT '',
    agent_session_id INTEGER,
    workflow_run_id TEXT NOT NULL DEFAULT '',
    project_key TEXT NOT NULL DEFAULT '',
    project_label TEXT NOT NULL DEFAULT '',
    metadata_json TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(source_kind, source_ref, content_digest)
);
CREATE INDEX IF NOT EXISTS idx_learning_evidence_task ON learning_evidence(task_id, created_at);
CREATE INDEX IF NOT EXISTS idx_learning_evidence_project ON learning_evidence(project_key, created_at);

CREATE TABLE IF NOT EXISTS learnings (
    id TEXT PRIMARY KEY,
    scope TEXT NOT NULL CHECK (scope IN ('user', 'project')),
    project_key TEXT NOT NULL DEFAULT '',
    kind TEXT NOT NULL,
    claim TEXT NOT NULL,
    rationale TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'candidate',
    confidence REAL NOT NULL DEFAULT 0,
    revision INTEGER NOT NULL DEFAULT 1,
    canonical_learning_id TEXT,
    curator_run_id TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (canonical_learning_id) REFERENCES learnings(id)
);
CREATE INDEX IF NOT EXISTS idx_learnings_scope_status ON learnings(scope, project_key, status, updated_at);

CREATE TABLE IF NOT EXISTS learning_evidence_links (
    learning_id TEXT NOT NULL,
    evidence_id TEXT NOT NULL,
    relation TEXT NOT NULL CHECK (relation IN ('support', 'counter')),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (learning_id, evidence_id, relation),
    FOREIGN KEY (learning_id) REFERENCES learnings(id) ON DELETE CASCADE,
    FOREIGN KEY (evidence_id) REFERENCES learning_evidence(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS learning_reflection_requests (
    id TEXT PRIMARY KEY,
    workflow_run_id TEXT NOT NULL,
    step_id TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    task_id TEXT NOT NULL,
    agent_session_id INTEGER,
    schema_version INTEGER NOT NULL,
    prompt TEXT NOT NULL,
    status TEXT NOT NULL,
    observations_json TEXT NOT NULL DEFAULT '[]',
    error TEXT NOT NULL DEFAULT '',
    delivered_at DATETIME,
    submitted_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(attempt_id)
);

CREATE TABLE IF NOT EXISTS learning_curation_generations (
    id TEXT PRIMARY KEY,
    workflow_run_id TEXT NOT NULL DEFAULT '',
    attempt_id TEXT NOT NULL DEFAULT '',
    packet_digest TEXT NOT NULL,
    packet_json TEXT NOT NULL,
    operations_json TEXT NOT NULL DEFAULT '[]',
    status TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    completed_at DATETIME,
    UNIQUE(attempt_id)
);

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
    target_learning_revision INTEGER NOT NULL DEFAULT 0,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (generation_id) REFERENCES learning_curation_generations(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_learning_proposals_status ON learning_proposals(status, created_at);
CREATE INDEX IF NOT EXISTS idx_learning_proposals_learning ON learning_proposals(learning_id, status);

CREATE TABLE IF NOT EXISTS learning_incorporations (
    id TEXT PRIMARY KEY,
    learning_id TEXT NOT NULL,
    destination TEXT NOT NULL,
    target TEXT NOT NULL DEFAULT '',
    action TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'proposed',
    revision INTEGER NOT NULL DEFAULT 1,
    implementation_task_id TEXT NOT NULL DEFAULT '',
    workflow_run_id TEXT NOT NULL DEFAULT '',
    review_tour_id TEXT NOT NULL DEFAULT '',
    references_json TEXT NOT NULL DEFAULT '[]',
    verification_kind TEXT NOT NULL DEFAULT '',
    verification_result TEXT NOT NULL DEFAULT '',
    target_digest TEXT NOT NULL DEFAULT '',
    verified_at DATETIME,
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now')),
    FOREIGN KEY (learning_id) REFERENCES learnings(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_learning_incorporations_learning ON learning_incorporations(learning_id, status);

CREATE TABLE IF NOT EXISTS learning_prompt_manifests (
    id TEXT PRIMARY KEY,
    agent_session_id INTEGER,
    task_id TEXT NOT NULL,
    adapter TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT '',
    project_key TEXT NOT NULL DEFAULT '',
    included_json TEXT NOT NULL DEFAULT '[]',
    excluded_json TEXT NOT NULL DEFAULT '[]',
    rendered_digest TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT (datetime('now'))
);
CREATE INDEX IF NOT EXISTS idx_learning_prompt_manifest_session ON learning_prompt_manifests(agent_session_id, created_at);

CREATE VIRTUAL TABLE IF NOT EXISTS learnings_fts USING fts5(learning_id UNINDEXED, claim, rationale);
CREATE VIRTUAL TABLE IF NOT EXISTS learning_evidence_fts USING fts5(evidence_id UNINDEXED, content);
