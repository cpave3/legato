-- What a worker advertises it can do for a handoff (LEG-226), as the main
-- last read it from the worker's capabilities route: JSON, NULL until read
-- (or for a worker too old to have the route). See docs/ai/integrations.md
-- § What a worker advertises.
ALTER TABLE workers ADD COLUMN advert TEXT;
ALTER TABLE workers ADD COLUMN advert_at TEXT;
