-- Terminal child metadata survives SSE gaps and native-panel reconnects.
ALTER TABLE agent_sessions ADD COLUMN chimera_subagents TEXT NOT NULL DEFAULT '[]';
