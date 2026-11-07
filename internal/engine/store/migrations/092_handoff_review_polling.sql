-- Polling a reviewed handoff's pull requests for comments: each list's
-- pages with their ETags (a 304 costs no rate limit), and when items were
-- told to the author.
CREATE TABLE IF NOT EXISTS handoff_review_poll_pages (
    task_id TEXT NOT NULL REFERENCES tasks(id) ON DELETE CASCADE,
    pr INTEGER NOT NULL,
    list TEXT NOT NULL,
    page INTEGER NOT NULL,
    etag TEXT NOT NULL DEFAULT '',
    body TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (task_id, pr, list, page)
);
ALTER TABLE handoff_review_items ADD COLUMN delivered INTEGER NOT NULL DEFAULT 0;
ALTER TABLE handoff_reviews ADD COLUMN polled_at TEXT NOT NULL DEFAULT '';
ALTER TABLE handoff_reviews ADD COLUMN last_new_at TEXT NOT NULL DEFAULT '';
ALTER TABLE handoff_reviews ADD COLUMN skipped_comments INTEGER NOT NULL DEFAULT 0;
ALTER TABLE handoff_review_prs ADD COLUMN author TEXT NOT NULL DEFAULT '';
