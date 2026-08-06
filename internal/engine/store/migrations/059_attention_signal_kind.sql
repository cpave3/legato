ALTER TABLE attention_items
    ADD COLUMN signal_kind TEXT NOT NULL DEFAULT 'legacy_unclassified';

UPDATE attention_items
SET signal_kind = CASE
    WHEN producer = 'plans' AND source_revision LIKE 'proposed:%' THEN 'plan_decision'
    WHEN producer = 'reviews' THEN 'review_ready'
    WHEN producer = 'workflows' AND source_revision LIKE 'manual:%' THEN 'workflow_manual_gate'
    WHEN producer = 'workflows' AND source_revision LIKE 'failed:%' THEN 'workflow_failed'
    WHEN producer = 'workflows' AND source_revision LIKE 'agent:%' THEN 'workflow_waiting_agent'
    WHEN producer = 'workflows' AND source_revision LIKE 'completed:%' THEN 'workflow_completion'
    WHEN producer = 'workflow_triggers' AND source_revision LIKE '%:waiting_input' THEN 'workflow_trigger_input'
    WHEN producer = 'workflow_triggers' AND source_revision LIKE '%:waiting_confirmation' THEN 'workflow_trigger_confirmation'
    WHEN producer = 'workflow_triggers' AND source_revision LIKE '%:failed' THEN 'workflow_trigger_failed'
    WHEN producer = 'learnings' THEN 'learning_curator_work'
    ELSE 'legacy_unclassified'
END;

UPDATE attention_items
SET status = 'resolved',
    resolved_at = COALESCE(resolved_at, datetime('now')),
    updated_at = datetime('now')
WHERE status != 'resolved'
  AND signal_kind IN ('review_ready', 'workflow_completion', 'learning_curator_work');

CREATE INDEX IF NOT EXISTS idx_attention_items_signal
    ON attention_items(status, signal_kind, urgency, updated_at);
