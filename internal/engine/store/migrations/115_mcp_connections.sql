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
    -- An advanced, pre-registered OAuth client: its ID and the bare loopback
    -- redirect registered for it. Its secret, if any, is sealed.
    static_client_id TEXT NOT NULL DEFAULT '',
    static_redirect_uri TEXT NOT NULL DEFAULT '',
    static_secret_set INTEGER NOT NULL DEFAULT 0,
    -- Which agents may use which tools: credential-broker rules (JSON),
    -- none by default. read_only_tools are the tools the operator calls
    -- read-only (comma separated patterns); upstream hints never are.
    rules_json TEXT NOT NULL DEFAULT '[]',
    read_only_tools TEXT NOT NULL DEFAULT '',
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

-- Client registrations Legato made (DCR), reused for the same issuer,
-- resource and exact redirect. A confidential client's secret is sealed under
-- the registration's ID.
CREATE TABLE IF NOT EXISTS oauth_client_registrations (
    id TEXT PRIMARY KEY,
    issuer TEXT NOT NULL,
    resource TEXT NOT NULL,
    redirect_uri TEXT NOT NULL,
    client_id TEXT NOT NULL,
    auth_method TEXT NOT NULL CHECK (auth_method IN ('none', 'client_secret_post')),
    created_at INTEGER NOT NULL,
    UNIQUE (issuer, resource, redirect_uri)
);
