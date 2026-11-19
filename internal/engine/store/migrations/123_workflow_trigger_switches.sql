-- Whether a workflow's trigger is on in this Legato (LEG-353): set from the
-- Workflows page, it beats the definition's enabled field, which it never
-- rewrites. No row: the definition decides. See docs/ai/workflows.md.
CREATE TABLE workflow_trigger_switches (
    workflow_name TEXT NOT NULL,
    trigger       TEXT NOT NULL,
    enabled       INTEGER NOT NULL,
    updated_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (workflow_name, trigger)
);
