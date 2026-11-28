-- Draft until approved (LEG-712): an agent-authored review loop that marks
-- its stack ready turns its pull requests into drafts while they are under
-- review. legato_drafted_at is when Legato last turned the layer into a
-- draft; draft_problem why it could not (the author App lacks the
-- permission, say), cleared when it next can or the layer is marked ready.
ALTER TABLE review_loop_prs ADD COLUMN legato_drafted_at TEXT NOT NULL DEFAULT '';
ALTER TABLE review_loop_prs ADD COLUMN draft_problem TEXT NOT NULL DEFAULT '';
