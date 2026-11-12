-- reeve.levels.risky's auto meant the default (signed once a device is
-- enrolled) before auto became a level that runs a change at once. A value
-- saved then is renamed default, so it keeps its meaning.
UPDATE settings SET value = '"default"', updated_by = 'migration'
    WHERE key = 'reeve.levels.risky' AND value = '"auto"';
