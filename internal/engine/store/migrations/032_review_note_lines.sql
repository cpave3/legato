ALTER TABLE review_hunk_notes ADD COLUMN line_start INTEGER;
ALTER TABLE review_hunk_notes ADD COLUMN line_end INTEGER;
ALTER TABLE review_hunk_notes ADD COLUMN line_anchor TEXT NOT NULL DEFAULT '';
ALTER TABLE review_hunk_notes ADD COLUMN updated_at DATETIME NOT NULL DEFAULT '';
UPDATE review_hunk_notes SET updated_at = created_at WHERE updated_at = '';
