CREATE TABLE execution_accounts (
    execution_id TEXT PRIMARY KEY REFERENCES executions(id) ON DELETE CASCADE,
    identity_json TEXT NOT NULL
);
