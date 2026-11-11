-- An engram kept in a store of its own (the Reeve's, ENGRAM_HOME) is
-- ended and restarted in that store: '' is the person's own store.
ALTER TABLE executions ADD COLUMN engram_home TEXT NOT NULL DEFAULT '';
