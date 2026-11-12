-- A resource an execution was given because its repository gives it to
-- every agent (agents.repository_resources), not because it was chosen. A
-- restart carries only the chosen ones; the spawn works the others out again.
ALTER TABLE execution_resources ADD COLUMN from_repository INTEGER NOT NULL DEFAULT 0;
