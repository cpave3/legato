ALTER TABLE review_hunk_notes ADD COLUMN risk TEXT NOT NULL DEFAULT '';
ALTER TABLE review_hunk_notes ADD COLUMN order_hint INTEGER;
