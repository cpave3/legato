-- A pass whose head moved on GitHub is discarded and pinned again under
-- the same number (LEG-560), so it counts toward no limit. attempt is how
-- many times its number was pinned before: the markers of its reviews
-- differ from a discarded attempt's, so a review an earlier attempt left on
-- GitHub is never taken for this one's.
ALTER TABLE handoff_review_passes ADD COLUMN attempt INTEGER NOT NULL DEFAULT 0;
