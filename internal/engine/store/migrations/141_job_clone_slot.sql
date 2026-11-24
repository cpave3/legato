-- The agent slot of its task a job clone was made for (LEG-618): main for
-- the task's own agent, a named slot (a workflow's triage agent, a
-- reviewer) otherwise. Only a main slot's clone is joined again by the
-- task's next agent, so a named slot's agent never shares the author's.
ALTER TABLE job_clones ADD COLUMN slot TEXT NOT NULL DEFAULT 'main';
