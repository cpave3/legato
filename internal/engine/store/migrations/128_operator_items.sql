-- Operator items (LEG-375): what an agent posts with for_operator for the
-- operator to see or act on. Kept apart from agent_sessions.activity, which
-- the agent tools' hooks own. task_id is the posting session's task (a swarm
-- worker's sub-task); attribution_task_id the task the operator sees it on.
-- revision rises on every post of the same title, so a dismissal of the old
-- version no longer holds. See docs/ai/attention.md § Operator items.
CREATE TABLE IF NOT EXISTS operator_items (
    id                  TEXT PRIMARY KEY,
    task_id             TEXT NOT NULL,
    attribution_task_id TEXT NOT NULL,
    execution_id        TEXT NOT NULL DEFAULT '',
    kind                TEXT NOT NULL CHECK (kind IN ('action', 'question', 'done', 'other')),
    title               TEXT NOT NULL,
    why                 TEXT NOT NULL DEFAULT '',
    how_kind            TEXT NOT NULL DEFAULT '' CHECK (how_kind IN ('', 'command', 'slash', 'link')),
    how_text            TEXT NOT NULL DEFAULT '',
    options_json        TEXT NOT NULL DEFAULT '[]',
    status              TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    close_reason        TEXT NOT NULL DEFAULT '',
    revision            INTEGER NOT NULL DEFAULT 1,
    created_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_at          TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    closed_at           TEXT
);

CREATE INDEX IF NOT EXISTS idx_operator_items_open ON operator_items (status, execution_id);
