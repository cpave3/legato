-- Whether a run's agent ran in Legato's sandbox, kept on the run: its
-- agent session row, which says so, is deleted on the slot's next spawn.
-- NULL when it is not known.
ALTER TABLE agent_conversation_runs ADD COLUMN sandboxed INTEGER;
