CREATE TABLE IF NOT EXISTS handoff_packets (
    id                   TEXT PRIMARY KEY,
    tour_id              TEXT NOT NULL,
    revision             INTEGER NOT NULL,
    pass_id              TEXT NOT NULL,
    base_sha             TEXT NOT NULL,
    head_sha             TEXT NOT NULL,
    repository           TEXT NOT NULL,
    boundary_fingerprint TEXT NOT NULL,
    packet_json           TEXT NOT NULL,
    outdated             INTEGER NOT NULL DEFAULT 0,
    outdated_reason      TEXT NOT NULL DEFAULT '',
    created_at           DATETIME NOT NULL DEFAULT (datetime('now')),
    UNIQUE(tour_id, revision),
    FOREIGN KEY(tour_id) REFERENCES review_tours(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_handoff_packets_tour ON handoff_packets(tour_id, revision DESC);

CREATE TABLE IF NOT EXISTS handoff_notes (
    packet_id TEXT PRIMARY KEY,
    revision  INTEGER NOT NULL DEFAULT 0,
    note_json TEXT NOT NULL DEFAULT '{"revision":0,"readiness":"","body":"","answers":{},"skipped":[]}',
    FOREIGN KEY(packet_id) REFERENCES handoff_packets(id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS handoff_observations (
    tour_id       TEXT PRIMARY KEY,
    checked_at    DATETIME NOT NULL,
    observed_base TEXT NOT NULL,
    observed_head TEXT NOT NULL,
    clean         INTEGER NOT NULL,
    FOREIGN KEY(tour_id) REFERENCES review_tours(id) ON DELETE CASCADE
);
