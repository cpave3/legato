-- What an agent started that still runs and will wake it (a shell, a
-- Monitor, a subagent, a session cron), as a JSON list of BackgroundJob;
-- and where its state comes from: 'plugin' once Legato's Claude Code state
-- plugin has reported for the session, after which the hook scripts
-- `legato hooks install` used to write are ignored for it.
ALTER TABLE agent_sessions ADD COLUMN background_jobs TEXT NOT NULL DEFAULT '[]';
ALTER TABLE agent_sessions ADD COLUMN hook_source TEXT NOT NULL DEFAULT '';
