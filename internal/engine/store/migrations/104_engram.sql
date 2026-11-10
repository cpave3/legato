-- Engram sessions. An execution started as an engram records which one and
-- its engram session key (ENGRAM_SESSION), which a resumed conversation
-- reuses. engram_bindings are the engrams bound to a place key (repo:… or
-- dir:…), with at most one default per place.
ALTER TABLE executions ADD COLUMN engram TEXT NOT NULL DEFAULT '';
ALTER TABLE executions ADD COLUMN engram_session TEXT NOT NULL DEFAULT '';
CREATE TABLE IF NOT EXISTS engram_bindings (
    place_key    TEXT NOT NULL,
    engram       TEXT NOT NULL,
    is_default   INTEGER NOT NULL DEFAULT 0,
    last_used_at TEXT,
    created_at   TEXT NOT NULL,
    PRIMARY KEY (place_key, engram)
);
CREATE UNIQUE INDEX IF NOT EXISTS engram_bindings_one_default ON engram_bindings (place_key) WHERE is_default = 1;
