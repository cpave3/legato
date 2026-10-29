-- The SHA-256 of the token an execution's agent-side processes present to
-- the host broker. The token itself is only in the agent's environment.
ALTER TABLE executions ADD COLUMN broker_token_hash TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_executions_broker_token ON executions(broker_token_hash) WHERE broker_token_hash != '';
