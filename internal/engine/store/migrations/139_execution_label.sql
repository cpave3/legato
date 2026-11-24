-- An execution's own name (LEG-613): an agent.run step's with.name, which
-- names that agent in lists without renaming the run's task. '' shows the
-- task's title.
ALTER TABLE executions ADD COLUMN label TEXT NOT NULL DEFAULT '';
