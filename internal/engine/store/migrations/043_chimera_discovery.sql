-- Chimera lockfile discovery metadata for agent sessions.
-- Populated after spawning a Chimera agent via tmux, when the lockfile
-- is discovered at ~/.chimera/instances/<pid>.json.
ALTER TABLE agent_sessions ADD COLUMN chimera_session_id TEXT;
ALTER TABLE agent_sessions ADD COLUMN chimera_endpoint TEXT;
ALTER TABLE agent_sessions ADD COLUMN last_event_id TEXT;
