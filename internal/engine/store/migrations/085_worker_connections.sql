-- The main instance's live connection to its workers (LEG-24). See
-- docs/ai/integrations.md § Live away state.

-- On a main: when the worker was last heard from (last_pull_at, already
-- there), since when it has been down for longer than the grace, and when it
-- refused this main's token (401: it no longer accepts this main).
ALTER TABLE workers ADD COLUMN unreachable_since TEXT;
ALTER TABLE workers ADD COLUMN refused_at TEXT;

-- On a main: an away task the worker no longer has (a pull without it, or a
-- worker that answers with another instance ID), and why. The main never
-- takes it back by itself; the person does.
ALTER TABLE away_tasks ADD COLUMN lost_at TEXT;
ALTER TABLE away_tasks ADD COLUMN lost_reason TEXT NOT NULL DEFAULT '';
