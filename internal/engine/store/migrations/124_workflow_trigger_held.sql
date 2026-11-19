-- Trigger requests for every trigger kind (LEG-354), part 1: the table's
-- text edited (store.go bumps schema_version after it, as for 058). A
-- request no longer
-- needs a column move (transition_id may be NULL for a schedule or a pull
-- request event); trigger_kind names its trigger; item is what the loop
-- stop counts by (the task, the PR, the due time); event_key makes one
-- request per event of a kind (a due time, a review); event_json is what
-- the event says, for the trigger's inputs. A request the loop stop holds
-- is 'held'. See docs/ai/workflows.md § Loop stop.
PRAGMA writable_schema = ON;

UPDATE sqlite_schema
SET sql = replace(replace(
    sql,
    'transition_id      TEXT NOT NULL REFERENCES',
    'transition_id      TEXT REFERENCES'),
    '''skipped'',''failed''))',
    '''skipped'',''failed'',''held''))'
)
WHERE type = 'table' AND name = 'workflow_trigger_requests';

PRAGMA writable_schema = OFF;
