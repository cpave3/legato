-- Attention items an integration pushes (Argus's CI cases). The "argus"
-- Attention producer snapshots the open ones. See
-- openspec/changes/argus-attention.
CREATE TABLE IF NOT EXISTS integration_attention (
    integration       TEXT NOT NULL,
    key               TEXT NOT NULL,
    revision          TEXT NOT NULL,
    signal            TEXT NOT NULL,
    title             TEXT NOT NULL,
    reason            TEXT NOT NULL,
    detail            TEXT NOT NULL DEFAULT '',
    urgency           TEXT NOT NULL,
    target            TEXT NOT NULL DEFAULT '{}',
    link              TEXT NOT NULL,
    state             TEXT NOT NULL DEFAULT 'open',
    resolved_revision TEXT NOT NULL DEFAULT '',
    resolved_reason   TEXT NOT NULL DEFAULT '',
    created_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at        TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (integration, key)
);
CREATE INDEX IF NOT EXISTS idx_integration_attention_open ON integration_attention(integration) WHERE state = 'open';
