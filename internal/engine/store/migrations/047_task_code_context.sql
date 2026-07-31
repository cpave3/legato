ALTER TABLE tasks ADD COLUMN code_context TEXT;

UPDATE tasks
SET code_context = json_object(
    'repo', COALESCE(json_extract(pr_meta, '$.repo'), ''),
    'branch', COALESCE(json_extract(pr_meta, '$.branch'), ''),
    'head_sha', COALESCE(json_extract(pr_meta, '$.head_sha'), ''),
    'linked_at', COALESCE(json_extract(pr_meta, '$.linked_at'), ''),
    'repo_source', CASE WHEN COALESCE(json_extract(pr_meta, '$.repo'), '') <> '' THEN 'legacy' ELSE '' END,
    'branch_source', CASE WHEN COALESCE(json_extract(pr_meta, '$.branch'), '') <> '' THEN 'legacy' ELSE '' END
)
WHERE pr_meta IS NOT NULL
  AND (COALESCE(json_extract(pr_meta, '$.repo'), '') <> ''
       OR COALESCE(json_extract(pr_meta, '$.branch'), '') <> '');

UPDATE tasks
SET pr_meta = json_set(pr_meta, '$.pinned', json('true'))
WHERE pr_meta IS NOT NULL
  AND COALESCE(json_extract(pr_meta, '$.pr_number'), 0) > 0;
