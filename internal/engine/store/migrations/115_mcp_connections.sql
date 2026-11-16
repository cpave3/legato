-- Instance-owned upstream MCP connections (LEG-203). Metadata only: tokens,
-- client secrets and verifiers stay in the sealed secrets store.
CREATE TABLE IF NOT EXISTS mcp_connections (
    name TEXT PRIMARY KEY,
    url TEXT NOT NULL,
    issuer TEXT NOT NULL DEFAULT '',
    -- Private network prefixes (comma separated) the operator allowed this
    -- connection to reach; empty means public addresses only.
    networks TEXT NOT NULL DEFAULT '',
    auth TEXT NOT NULL CHECK (auth IN ('none', 'oauth')),
    enabled INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0, 1)),
    instance_id TEXT NOT NULL,
    revision INTEGER NOT NULL,
    last_contact_at INTEGER NOT NULL DEFAULT 0,
    last_error TEXT NOT NULL DEFAULT '',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- Revisions are never reused for a name, even after removal, so work begun
-- against a removed connection cannot settle against a new one.
CREATE TABLE IF NOT EXISTS mcp_connection_revisions (
    name TEXT PRIMARY KEY,
    last INTEGER NOT NULL
);
