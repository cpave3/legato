-- The loop record replaced handoff_review* and the legato.review_loop task
-- metadata (LEG-606): migration 148 copied them onto review_loop*, and since
-- then nothing reads or writes them. A database from before 148 runs 148
-- first, then this. Child tables go before the tables they reference.
DROP TABLE IF EXISTS handoff_review_prs;
DROP TABLE IF EXISTS handoff_review_findings;
DROP TABLE IF EXISTS handoff_review_items;
DROP TABLE IF EXISTS handoff_review_poll_pages;
DROP TABLE IF EXISTS handoff_review_passes;
DROP TABLE IF EXISTS handoff_reviews;

-- The run caller's record of a run's loop, now on the loop, leaves each
-- task's metadata; a legato object left empty goes with it, and metadata
-- left empty is none.
UPDATE tasks SET metadata = json_remove(metadata, '$.legato.review_loop')
WHERE json_valid(metadata) AND json_type(metadata, '$.legato.review_loop') IS NOT NULL;
UPDATE tasks SET metadata = json_remove(metadata, '$.legato')
WHERE json_valid(metadata) AND json_type(metadata, '$.legato') = 'object' AND json(json_extract(metadata, '$.legato')) = '{}';
UPDATE tasks SET metadata = NULL WHERE json_valid(metadata) AND json(metadata) = '{}';
