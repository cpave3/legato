ALTER TABLE tasks ADD COLUMN remote_description_md TEXT NOT NULL DEFAULT '';

UPDATE tasks
SET remote_description_md = description_md,
    description = '',
    description_md = ''
WHERE provider IS NOT NULL;
