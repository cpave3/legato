-- Whether the session's agent runs in Legato's sandbox, whose mounts are
-- fixed at launch: the UI marks an unsandboxed one, and resources cannot be
-- added to a sandboxed one while it runs.
ALTER TABLE agent_sessions ADD COLUMN sandboxed INTEGER NOT NULL DEFAULT 0;
