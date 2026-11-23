-- One Burnlite connection for the board and agents (LEG-544). An MCP
-- connection made for the Burnlite board integration at an address the
-- operator typed as a loopback literal (a Burnlite run locally) may reach it:
-- egress refuses loopback otherwise. Set only by that link, never by a
-- request, and cleared when the connection's address changes.
ALTER TABLE mcp_connections ADD COLUMN loopback INTEGER NOT NULL DEFAULT 0 CHECK (loopback IN (0, 1));
