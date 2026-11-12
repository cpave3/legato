-- The agents the Reeve watches: it is told when one waits, goes idle,
-- errors, ends, adds a note or has a pull request change. events is the
-- comma-separated events asked for ('' for all); seen is the watcher's JSON
-- record of what it last saw.
CREATE TABLE IF NOT EXISTS reeve_watches (
    task_id       TEXT PRIMARY KEY,
    reeve_task_id TEXT NOT NULL,
    events        TEXT NOT NULL DEFAULT '',
    seen          TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);

-- What watched agents did that the Reeve has not been told yet: kept until
-- a message carrying it is delivered, so nothing is lost while no Reeve
-- runs or while it is in the middle of a turn. body is the event as JSON.
CREATE TABLE IF NOT EXISTS reeve_watch_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id    TEXT NOT NULL,
    body       TEXT NOT NULL,
    created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
