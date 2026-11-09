-- An agent the person switched to its terminal on purpose: a restart of
-- Legato does not bind it to its native endpoint again. Binding it (an
-- upgrade) clears the mark.
ALTER TABLE agent_sessions ADD COLUMN native_off INTEGER NOT NULL DEFAULT 0;
