-- An agent asked to start as an engram whose boot failed starts without
-- it: the engram it was asked for and why are kept on its execution, for
-- the Attention item that says so.
ALTER TABLE executions ADD COLUMN engram_boot_failed TEXT NOT NULL DEFAULT '';
ALTER TABLE executions ADD COLUMN engram_boot_error TEXT NOT NULL DEFAULT '';
