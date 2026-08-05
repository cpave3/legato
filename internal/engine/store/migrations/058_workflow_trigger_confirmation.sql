PRAGMA writable_schema = ON;

UPDATE sqlite_schema
SET sql = replace(
    sql,
    'CHECK (status IN (''pending'',''waiting_input'',''started'',''skipped'',''failed''))',
    'CHECK (status IN (''pending'',''waiting_input'',''waiting_confirmation'',''starting'',''started'',''skipped'',''failed''))'
)
WHERE type = 'table' AND name = 'workflow_trigger_requests';

PRAGMA writable_schema = OFF;
