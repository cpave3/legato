-- Resources: named sets of files and folders the person lets an agent read or
-- write, attached when an agent is started or while it runs. The ones named in
-- the configuration are not stored here.
CREATE TABLE IF NOT EXISTS resources (
    id         INTEGER PRIMARY KEY,
    name       TEXT NOT NULL UNIQUE COLLATE NOCASE CHECK (name != ''),
    created_at DATETIME NOT NULL DEFAULT (datetime('now')),
    updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE IF NOT EXISTS resource_paths (
    resource_id INTEGER NOT NULL REFERENCES resources(id) ON DELETE CASCADE,
    position    INTEGER NOT NULL,
    path        TEXT NOT NULL CHECK (path != ''),
    access      TEXT NOT NULL CHECK (access IN ('read', 'write')),
    PRIMARY KEY (resource_id, position)
);

-- The resources an execution was given, by name, at launch or later.
CREATE TABLE IF NOT EXISTS execution_resources (
    execution_id TEXT NOT NULL,
    name         TEXT NOT NULL,
    attached_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    PRIMARY KEY (execution_id, name)
);
