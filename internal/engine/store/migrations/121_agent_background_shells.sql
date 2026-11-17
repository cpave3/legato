-- The background shells an agent's mod reported (Chimera's background.start
-- and background.exit): command, state and exit code, never their output.
ALTER TABLE agent_sessions ADD COLUMN background_shells TEXT NOT NULL DEFAULT '[]';
