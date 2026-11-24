-- When the Reeve was told of an agent's open wait (LEG-623): the watcher
-- holds an agent's message while the Reeve is busy, so the time to answer
-- runs from told_at (RFC 3339, UTC), not from since. NULL until told; a
-- wait that opens again after it was cleared is not told yet.
ALTER TABLE reeve_waits ADD COLUMN told_at TEXT;
