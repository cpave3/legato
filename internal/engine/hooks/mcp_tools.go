package hooks

import "strings"

// mcpServerName is the name every agent tool knows Legato's MCP server by.
// Tool names an agent sees are built from it (`mcp__legato__show_card` in
// Codex, `mcp_legato_show_card` in Chimera).
const mcpServerName = "legato"

// mcpServerEnv names what `legato mcp serve` needs from the session's
// environment: which session it acts for, and where that session's Legato
// keeps its configuration, database, sockets and tmux server. Agent tools
// that start an MCP server with a reduced environment are told to pass these.
var mcpServerEnv = []string{
	"LEGATO_TASK_ID", "LEGATO_EXECUTION_ID",
	"LEGATO_CONFIG", "LEGATO_HOME",
	"XDG_CONFIG_HOME", "XDG_DATA_HOME", "XDG_RUNTIME_DIR",
	"TMUX", "TMUX_TMPDIR",
}

// mcpServerArgs are the arguments that make the Legato binary an MCP server.
var mcpServerArgs = []string{"mcp", "serve"}

// tomlString writes s as a TOML basic string.
func tomlString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// tomlStringArray writes items as a TOML array of basic strings.
func tomlStringArray(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = tomlString(item)
	}
	return "[" + strings.Join(quoted, ",") + "]"
}
