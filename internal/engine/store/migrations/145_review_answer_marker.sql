-- The marker of an item's answer (LEG-562): hidden in the reply or comment
-- the author App posts, and looked for on GitHub before the answer is
-- posted again, so a crash between the post and marking it posted never
-- posts it twice. A new answer gets a new marker. Answers given before
-- this migration get theirs now.
ALTER TABLE handoff_review_items ADD COLUMN answer_marker TEXT NOT NULL DEFAULT '';
UPDATE handoff_review_items SET answer_marker = lower(hex(randomblob(8))) WHERE answer != '';
