-- Bringing an away task home: return and recall (LEG-25). See
-- docs/ai/integrations.md § Return and recall.

-- On a main: when the person asked to return or recall an away task, and,
-- for a recall, the state it was recalled from (empty for a return), so a
-- return the connection finishes later still adds the recall note.
-- returned_stack is the Staccato stack document the worker gave back, kept
-- as it came. archive_owed is 1 once the worker's results are applied (the
-- away task closed) until the worker confirms it archived its card (step
-- 2): a call the main owes the worker and makes on its next connect.
ALTER TABLE away_tasks ADD COLUMN return_asked_at TEXT;
ALTER TABLE away_tasks ADD COLUMN recalled_from TEXT NOT NULL DEFAULT '';
ALTER TABLE away_tasks ADD COLUMN returned_stack TEXT;
ALTER TABLE away_tasks ADD COLUMN archive_owed INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_away_tasks_archive_owed ON away_tasks(worker_instance_id) WHERE archive_owed = 1;
