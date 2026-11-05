-- Pairing a main instance with a worker (handoff). See docs/ai/integrations.md
-- § Main instances and workers.

-- On the worker: an integration token is Argus's or a paired main's. A main's
-- token carries the main's instance ID, which is who it is: one live token
-- per instance, and names are unique within a kind, so a main named argus
-- does not collide with Argus's token.
ALTER TABLE integration_tokens ADD COLUMN kind TEXT NOT NULL DEFAULT 'argus';
ALTER TABLE integration_tokens ADD COLUMN instance_id TEXT NOT NULL DEFAULT '';
DROP INDEX IF EXISTS idx_integration_tokens_live_name;
CREATE UNIQUE INDEX IF NOT EXISTS idx_integration_tokens_live_name ON integration_tokens(kind, name) WHERE revoked_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_integration_tokens_live_main ON integration_tokens(instance_id) WHERE kind = 'main' AND revoked_at IS NULL;

-- On the main: each worker it paired with, by the worker's instance ID. The
-- token is in Legato's sealed secret store under token_secret; ca_certificate
-- is what the main trusts for the worker's HTTPS. main_name is this main's
-- name on the worker.
CREATE TABLE IF NOT EXISTS workers (
    instance_id     TEXT PRIMARY KEY,
    address         TEXT NOT NULL,
    name            TEXT NOT NULL,
    main_name       TEXT NOT NULL,
    ca_certificate  TEXT NOT NULL DEFAULT '',
    token_secret    TEXT NOT NULL,
    sandbox         INTEGER NOT NULL DEFAULT 0,
    handoff_version INTEGER NOT NULL DEFAULT 0,
    paired_at       TEXT NOT NULL,
    last_pull_at    TEXT
);
