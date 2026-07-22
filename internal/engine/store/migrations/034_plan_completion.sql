ALTER TABLE plans ADD COLUMN cleanup_after_implementation INTEGER NOT NULL DEFAULT 0;
ALTER TABLE plans ADD COLUMN source_bundle_path TEXT NOT NULL DEFAULT '';
ALTER TABLE plans ADD COLUMN completed_at DATETIME;
