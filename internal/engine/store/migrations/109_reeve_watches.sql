-- The agents a Reeve session started or messaged through its change sets,
-- whose notes, pull requests, turns and end it is told of. seen is the
-- watcher's JSON record of what it last reported.
CREATE TABLE IF NOT EXISTS reeve_watches (
    task_id       TEXT PRIMARY KEY,
    reeve_task_id TEXT NOT NULL,
    seen          TEXT NOT NULL DEFAULT '{}',
    created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
