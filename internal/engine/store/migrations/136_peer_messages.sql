-- LEG-537: the agents' graph view draws a link between two agents that
-- messaged each other lately. Each peer message (legato agent message,
-- broadcast, message_peer) is kept as who sent it to whom and when; never
-- its text. Rows older than a day are pruned as new ones arrive.

CREATE TABLE IF NOT EXISTS peer_messages (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    sender_task_id TEXT NOT NULL,
    recipient_task_id TEXT NOT NULL,
    -- RFC 3339 in UTC with milliseconds, so the text sorts as time does.
    sent_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS peer_messages_sent_at ON peer_messages (sent_at);
