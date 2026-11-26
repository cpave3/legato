CREATE TABLE IF NOT EXISTS active_usage_profile (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    state TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS usage_profile_switches (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    from_profile TEXT NOT NULL,
    to_profile TEXT NOT NULL,
    since TEXT NOT NULL,
    reason TEXT NOT NULL
);
