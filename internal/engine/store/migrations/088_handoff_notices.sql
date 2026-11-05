-- The phone notifications a main sent about its away tasks and workers
-- (LEG-30): one row per state notified (a failed or lost handoff, a worker
-- that no longer accepts this main, one outage of a worker), so that a TUI
-- and legato serve on one database notify the phone once between them, and
-- a restart does not notify again. See docs/ai/attention.md § Handoff.
CREATE TABLE IF NOT EXISTS handoff_notices (
    notice  TEXT PRIMARY KEY,
    sent_at TEXT NOT NULL
);
