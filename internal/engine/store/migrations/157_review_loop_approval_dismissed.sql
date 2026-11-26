-- The steward's dismissed approvals (LEG-593): a layer whose legato-reviews
-- approval a person dismissed at the head Confirm approved (no push since),
-- as the steward's watch last read it. Kept on the record so every process
-- on the data folder (a TUI beside legato serve) sees it, not only the
-- engine's holder, whose watch reads GitHub.
ALTER TABLE review_loop_prs ADD COLUMN approval_dismissed INTEGER NOT NULL DEFAULT 0;
