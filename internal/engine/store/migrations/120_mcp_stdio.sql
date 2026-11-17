-- Local MCP servers (LEG-223): a command Legato runs on the host and speaks
-- MCP to over stdio. A stdio connection has no URL and auth 'none'; its
-- command, arguments (JSON list), working directory and environment (JSON
-- object; a value may be a legato:// secret reference) are what the operator
-- approved.
ALTER TABLE mcp_connections ADD COLUMN kind TEXT NOT NULL DEFAULT 'http' CHECK (kind IN ('http', 'stdio'));
ALTER TABLE mcp_connections ADD COLUMN command TEXT NOT NULL DEFAULT '';
ALTER TABLE mcp_connections ADD COLUMN args_json TEXT NOT NULL DEFAULT '[]';
ALTER TABLE mcp_connections ADD COLUMN dir TEXT NOT NULL DEFAULT '';
ALTER TABLE mcp_connections ADD COLUMN env_json TEXT NOT NULL DEFAULT '{}';
