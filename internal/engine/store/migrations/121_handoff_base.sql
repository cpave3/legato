-- The branch a new-branch handoff starts from (LEG-236): the main's preset
-- base_branch for the repository, or '' for GitHub's default branch. See
-- docs/ai/integrations.md § Handing off a task.
ALTER TABLE handoffs ADD COLUMN base TEXT NOT NULL DEFAULT '';
