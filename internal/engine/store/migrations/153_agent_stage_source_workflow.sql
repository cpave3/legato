-- Workflow agent waits (LEG-485, spec LEG-482 §5.3): a step that waits for
-- an agent's Finished resets its place to Implementing with the new source
-- workflow before it sends its prompt. SQLite cannot alter a CHECK, so
-- agent_stages is rebuilt with the same rows.
CREATE TABLE agent_stages_v153 (
    task_id            TEXT NOT NULL,
    slot               TEXT NOT NULL DEFAULT 'main',
    place_execution_id TEXT NOT NULL DEFAULT '',
    stage              TEXT NOT NULL CHECK (stage IN ('exploring', 'planning', 'implementing', 'reviewing', 'finished')),
    reason             TEXT NOT NULL DEFAULT '',
    source             TEXT NOT NULL CHECK (source IN ('start', 'operator', 'agent', 'milestone', 'evidence', 'workflow')),
    since              TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    execution_id       TEXT NOT NULL DEFAULT '',
    updated_at         TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (task_id, slot, place_execution_id)
);
INSERT INTO agent_stages_v153 (task_id, slot, place_execution_id, stage, reason, source, since, execution_id, updated_at)
SELECT task_id, slot, place_execution_id, stage, reason, source, since, execution_id, updated_at FROM agent_stages;
DROP TABLE agent_stages;
ALTER TABLE agent_stages_v153 RENAME TO agent_stages;

