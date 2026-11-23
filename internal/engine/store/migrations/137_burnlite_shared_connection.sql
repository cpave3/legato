-- One Burnlite connection for the board and agents (LEG-544). An MCP
-- connection made for the Burnlite board integration at an address the
-- operator typed as a loopback literal (a Burnlite run locally) may reach it:
-- egress refuses loopback otherwise. Set only by that link, never by a
-- request, and cleared when the connection's address changes.
ALTER TABLE mcp_connections ADD COLUMN loopback INTEGER NOT NULL DEFAULT 0 CHECK (loopback IN (0, 1));

-- One process at a time joins a board connection from before LEG-544 with
-- agents' MCP connection: it holds this lease while it reads, revokes and
-- adopts, so two starting at once never revoke the token the other kept.
CREATE TABLE IF NOT EXISTS burnlite_migration_lease (
    id INTEGER PRIMARY KEY CHECK (id = 1),
    holder TEXT NOT NULL DEFAULT '',
    lease_until INTEGER NOT NULL DEFAULT 0
);
