-- Where a pass's pull request is in the reviewer's checkout (LEG-613): the
-- branch of the clone it was put on when that is not its head branch on
-- GitHub, and the repository its head branch is in, for a fork's. Both are
-- '' for passes before, and for a handoff's, which reviews the author's own
-- branches.
ALTER TABLE handoff_review_prs ADD COLUMN local_branch TEXT NOT NULL DEFAULT '';
ALTER TABLE handoff_review_prs ADD COLUMN head_repo TEXT NOT NULL DEFAULT '';
