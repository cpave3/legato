-- The reviews PR tracking has seen on each pull request (LEG-357), so a new
-- changes-requested review fires pr.changes_requested once. review_id 0 is
-- the baseline: the PR was first seen then, and the reviews it had were
-- recorded without firing. github_app_logins are the bot logins of
-- Legato's own GitHub Apps, learned from the reviews Legato posts, whose
-- activity never fires a trigger. See docs/ai/pr-tracking.md.
CREATE TABLE IF NOT EXISTS pr_reviews_seen (
    repo      TEXT NOT NULL,
    pr_number INTEGER NOT NULL,
    review_id INTEGER NOT NULL,
    state     TEXT NOT NULL DEFAULT '',
    login     TEXT NOT NULL DEFAULT '',
    seen_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    PRIMARY KEY (repo, pr_number, review_id)
);

CREATE TABLE IF NOT EXISTS github_app_logins (
    login   TEXT PRIMARY KEY,
    seen_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
);
