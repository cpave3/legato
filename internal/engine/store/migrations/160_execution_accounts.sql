-- The Claude account each execution was launched with, recorded once before
-- launch so a resume uses the same one. IF NOT EXISTS, as every migration
-- that only adds a table: a ledger rebuilt behind its schema replays it.
CREATE TABLE IF NOT EXISTS execution_accounts (
    execution_id TEXT PRIMARY KEY REFERENCES executions(id) ON DELETE CASCADE,
    identity_json TEXT NOT NULL
);
