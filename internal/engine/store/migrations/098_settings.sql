-- Settings kept in the database, beside config.yaml: one row per setting of
-- the registry (config/settings.go), its value as JSON. A value in
-- config.yaml pins its setting, and is never written here.
CREATE TABLE IF NOT EXISTS settings (
    key        TEXT PRIMARY KEY,
    value      TEXT NOT NULL,
    updated_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ', 'now')),
    updated_by TEXT NOT NULL DEFAULT ''
);

-- The tint, the update mode and Burnlite's projects were instance values.
INSERT OR IGNORE INTO settings (key, value, updated_by)
    SELECT 'web.tint', json_quote(value), 'migration' FROM instance_values WHERE key = 'web.tint' AND value != '';
INSERT OR IGNORE INTO settings (key, value, updated_by)
    SELECT 'updates.mode', json_quote(value), 'migration' FROM instance_values WHERE key = 'updates.mode' AND value != '';
INSERT OR IGNORE INTO settings (key, value, updated_by)
    SELECT 'burnlite.projects', '["' || replace(value, ',', '","') || '"]', 'migration' FROM instance_values WHERE key = 'burnlite.projects' AND value != '';
DELETE FROM instance_values WHERE key IN ('web.tint', 'updates.mode', 'burnlite.projects');
