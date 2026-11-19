-- Legato Scope (LEG-290): pins the operator places on a live page, sent in
-- batches into the pool, and the origin subscriptions that route them to a
-- task slot's agent. Screenshots are files under <data_dir>/scope/, named
-- by scope_pins.screenshot. See docs/ai/scope.md.
CREATE TABLE IF NOT EXISTS scope_batches (
    id             TEXT PRIMARY KEY,
    origin         TEXT NOT NULL,
    created_at     TEXT NOT NULL,
    delivered_at   TEXT,
    delivery_error TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS scope_pins (
    id               TEXT PRIMARY KEY,
    batch_id         TEXT NOT NULL REFERENCES scope_batches(id) ON DELETE CASCADE,
    origin           TEXT NOT NULL,
    seq              INTEGER NOT NULL,
    url              TEXT NOT NULL,
    path             TEXT NOT NULL,
    comment          TEXT NOT NULL,
    state            TEXT NOT NULL DEFAULT 'sent',
    note             TEXT NOT NULL DEFAULT '',
    selector         TEXT NOT NULL DEFAULT '',
    context_json     TEXT NOT NULL DEFAULT '{}',
    source_json      TEXT,
    screenshot       TEXT NOT NULL DEFAULT '',
    resolved_by_task TEXT NOT NULL DEFAULT '',
    created_at       TEXT NOT NULL,
    updated_at       TEXT NOT NULL,
    delivered_at     TEXT,
    resolved_at      TEXT
);

CREATE INDEX IF NOT EXISTS scope_pins_origin ON scope_pins(origin, state);

-- One subscriber per origin: the newest takes over.
CREATE TABLE IF NOT EXISTS scope_subscriptions (
    origin     TEXT PRIMARY KEY,
    task_id    TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    slot       TEXT NOT NULL DEFAULT 'main',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS scope_subscriptions_slot ON scope_subscriptions(task_id, slot);
