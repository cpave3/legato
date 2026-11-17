-- Whether the session's agent may give its containers the host's network
-- through its filtered Docker socket (off unless asked for at spawn or
-- reload): kept so a reload or a resume keeps it, and so the UI marks it.
ALTER TABLE agent_sessions ADD COLUMN docker_host_network INTEGER NOT NULL DEFAULT 0;
