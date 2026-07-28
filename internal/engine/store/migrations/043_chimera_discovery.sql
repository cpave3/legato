-- Chimera lockfile discovery metadata for agent sessions.
-- Populated after spawning a Chimera agent via tmux, when the lockfile
-- is discovered at ~/.chimera/instances/<pid>.json.
-- Guard for legacy databases that may not have agent_sessions yet.
INSERT INTO agent_sessions (id) SELECT 0 WHERE 0;
ALTER TABLE agent_sessions ADD COLUMN chimera_session_id TEXT;
ALTER TABLE agent_sessions ADD COLUMN chimera_endpoint TEXT;
ALTER TABLE agent_sessions ADD COLUMN last_event_id TEXT;
