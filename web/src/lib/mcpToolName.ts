// An MCP server's tool, as each agent tool names it:
//
//   - Claude Code: `mcp__<server>__<tool>`, and for a plugin's server
//     `mcp__plugin_<plugin>_<server>__<tool>`. Like Claude Code itself, the
//     server is what comes before the first `__` and the tool is the rest.
//     Plugin names are kebab-case, so the plugin ends at the first `_`.
//   - Codex (through Legato's bridge): `<server>.<tool>`.
//   - Chimera: `mcp_<server>_<tool>`, where each character outside
//     [a-zA-Z0-9] in the server or tool is written `_x<six hex digits>_`, so
//     a bare `_` only ever separates the parts.
//
// A name in none of these forms is not read as an MCP tool: it is shown as
// it is.
export interface McpToolName {
  raw: string
  server: string
  plugin?: string
  tool: string
  // How the panel names them: "Burnlite", "List tasks".
  serverLabel: string
  toolLabel: string
}

const CODEX_FORM = /^([A-Za-z0-9_-]+)\.([A-Za-z0-9_-]+)$/
const PLUGIN_SERVER = /^plugin_([^_]+)_(.+)$/
const CHIMERA_ESCAPE = /_x([0-9a-f]{6})_/g
// The names of claude.ai's connectors, as Claude Code names their servers.
const CLAUDE_AI_PREFIX = "claude_ai_"

export function mcpToolName(raw: string): McpToolName | null {
  const parts = parse(raw)
  if (!parts) return null
  return { raw, ...parts, serverLabel: serverLabel(parts.server), toolLabel: humanizeToolName(parts.tool) }
}

// toolDisplayName is a tool's name on one line: "Burnlite › List tasks" for
// an MCP tool, any other name as it is.
export function toolDisplayName(raw: string): string {
  const name = mcpToolName(raw)
  return name ? `${name.serverLabel} › ${name.toolLabel}` : raw
}

function parse(raw: string): { server: string; plugin: string | undefined; tool: string } | null {
  if (raw.startsWith("mcp__")) {
    const rest = raw.slice("mcp__".length)
    const cut = rest.indexOf("__")
    if (cut <= 0) return null
    const server = rest.slice(0, cut)
    const tool = rest.slice(cut + 2)
    if (!tool) return null
    if (server.startsWith("plugin_")) {
      const plugin = PLUGIN_SERVER.exec(server)
      return plugin ? { server: plugin[2], plugin: plugin[1], tool } : null
    }
    return { server, plugin: undefined, tool }
  }
  if (raw.startsWith("mcp_")) {
    // Escapes are taken out before splitting, so their underscores do not
    // count as separators.
    const parts = raw.replace(CHIMERA_ESCAPE, (_match, hex: string) => `\uE000${hex}\uE001`).split("_")
    if (parts.length !== 3 || !parts[1] || !parts[2]) return null
    const decode = (part: string) => part.replace(/\uE000([0-9a-f]{6})\uE001/g, (_match, hex: string) => String.fromCodePoint(parseInt(hex, 16)))
    return { server: decode(parts[1]), plugin: undefined, tool: decode(parts[2]) }
  }
  const codex = CODEX_FORM.exec(raw)
  return codex ? { server: codex[1], plugin: undefined, tool: codex[2] } : null
}

// Words that read better in capitals.
const ACRONYMS = new Set(["api", "cli", "css", "html", "http", "https", "id", "json", "llm", "mcp", "pr", "sql", "ssh", "ui", "uri", "url", "xml"])
// Servers whose usual spelling title case would get wrong.
const KNOWN_SERVERS: Record<string, string> = { github: "GitHub", gitlab: "GitLab" }

function words(name: string): string[] {
  return name.replace(/([a-z0-9])([A-Z])/g, "$1 $2").split(/[\s_-]+/).filter(Boolean)
}

// humanizeToolName turns a tool's machine name into words: list_tasks and
// listTasks are "List tasks".
export function humanizeToolName(name: string): string {
  const text = words(name).map((word) => {
    if (ACRONYMS.has(word.toLowerCase())) return word.toUpperCase()
    if (word.length > 1 && word === word.toUpperCase() && /[A-Z]/.test(word)) return word
    return word.toLowerCase()
  }).join(" ")
  return text ? text[0].toUpperCase() + text.slice(1) : name
}

function serverLabel(server: string): string {
  const known = KNOWN_SERVERS[server.toLowerCase()]
  if (known) return known
  // A claude.ai connector is named as claude.ai names it: Google_Calendar.
  if (server.startsWith(CLAUDE_AI_PREFIX) && server.length > CLAUDE_AI_PREFIX.length) {
    return server.slice(CLAUDE_AI_PREFIX.length).split("_").filter(Boolean).join(" ")
  }
  const text = words(server).map((word) => ACRONYMS.has(word.toLowerCase()) ? word.toUpperCase() : word[0].toUpperCase() + word.slice(1)).join(" ")
  return text || server
}
