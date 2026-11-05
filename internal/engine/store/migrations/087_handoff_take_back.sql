-- Taking an away task back, removing a worker (LEG-26). See
-- docs/ai/integrations.md § Take back, Drop and Leave.

-- On a main: when the person took an away task back without its results,
-- and drop_owed, 1 from then until the worker confirms it dropped the
-- task (stopped its agent, archived its card): a call the main owes the
-- worker and makes on its next connect, as long as the worker stays
-- paired.
ALTER TABLE away_tasks ADD COLUMN taken_back_at TEXT;
ALTER TABLE away_tasks ADD COLUMN drop_owed INTEGER NOT NULL DEFAULT 0;
CREATE INDEX IF NOT EXISTS idx_away_tasks_drop_owed ON away_tasks(worker_instance_id) WHERE drop_owed = 1;

-- On a main: a worker the person removed that has not yet been told to
-- unpair this main (Leave). Its address and token are kept, hidden from
-- every list, only to send the calls owed it, until leaving_until; then
-- it is forgotten.
ALTER TABLE workers ADD COLUMN leaving_until TEXT;
