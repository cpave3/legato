export const settingsCategories = [
  { id: "preferences", title: "Preferences", terms: "appearance theme terminal motion keyboard prompt detection browser notifications" },
  { id: "agents", title: "Agents and workspaces", terms: "repositories guidance resources tools macros workspaces defaults groups sandbox swarms engrams learning models" },
  { id: "reeve", title: "Reeve", terms: "sessions automation rules engram pairings voice dictation speech microphone" },
  { id: "security", title: "Security", terms: "credentials secrets password grants audit approval devices channels tokens" },
  { id: "integrations", title: "Integrations", terms: "jira github burnlite argus mqtt home assistant broker notifications delivery" },
  { id: "connections", title: "Connections and handoff", terms: "browser servers pairing workers main instances tls network discovery" },
  { id: "server", title: "Server and maintenance", terms: "tint identity updates restart configuration migration import export tui development advanced" },
] as const
export type SettingsCategory = typeof settingsCategories[number]["id"]

const prefixes: Partial<Record<SettingsCategory, string[]>> = {
  agents: ["agents", "engram", "swarm", "learning", "groups", "default_workspace", "default_import_workspace", "worktrees", "repository_cache", "review_passes", "manticore", "workspaces", "macros", "adapters", "repositories", "workflows.repositories", "resources"],
  reeve: ["reeve", "voice"],
  security: ["credentials", "credential_sources", "credential_channels", "ssh_agent_socket", "approval_devices", "secrets"],
  integrations: ["jira", "github", "burnlite", "integrations", "notifications"],
  connections: ["web.discovery", "web.tls", "web.public_url", "web.port", "web.enabled"],
  server: ["web.tint", "updates", "theme", "icons", "editor", "keybindings", "db", "data_dir"],
}
export function settingOwner(key: string): SettingsCategory {
  return settingsCategories.find(({ id }) => prefixes[id]?.some((p) => key === p || key.startsWith(p + ".")))?.id ?? "server"
}
export function isAdvancedSetting(key: string) {
  return !Object.values(prefixes).flat().some((p) => key === p || key.startsWith(p + "."))
}

export const settingsCards: { id: string; title: string; category: SettingsCategory; terms: string; scope?: string }[] = [
  { id: "appearance", title: "Appearance", category: "preferences", terms: "app terminal theme colours motion glitch animation", scope: "This browser/device" },
  { id: "keyboard", title: "Keyboard and prompt detection", category: "preferences", terms: "switch modifier shortcuts approval terminal", scope: "This browser" },
  { id: "browser-notifications", title: "Browser notifications", category: "preferences", terms: "push approvals alert", scope: "This browser" },
  { id: "list-repositories", title: "Repositories", category: "agents", terms: "repositories workflows.repositories clone folder presets" },
  { id: "list-agents.guidance", title: "Repository guidance", category: "agents", terms: "agents.guidance instructions" },
  { id: "list-resources", title: "Resources", category: "agents", terms: "files folders" },
  { id: "directory-presets", title: "Directory presets", category: "agents", terms: "directory folder path saved" },
  { id: "job-clones", title: "Managed clones", category: "agents", terms: "remote cleanup" },
  { id: "engram-bindings", title: "Engram bindings", category: "agents", terms: "memory" },
  { id: "reeve-sessions", title: "Reeve sessions", category: "reeve", terms: "history" },
  { id: "reeve-auto", title: "Runs without asking", category: "reeve", terms: "automation" },
  { id: "reeve-engram", title: "Reeve engram", category: "reeve", terms: "memory" },
  { id: "reeve-pairings", title: "Reeve pairings", category: "reeve", terms: "peers" },
  { id: "credentials", title: "Credential overview", category: "security", terms: "grants audit secrets passwords" },
  { id: "devices", title: "Approval devices", category: "security", terms: "passkey signature phone" },
  { id: "integration-tokens", title: "Integration tokens", category: "security", terms: "argus access" },
  { id: "mqtt", title: "Home Assistant over MQTT", category: "integrations", terms: "broker smart home automation" },
  { id: "servers", title: "Browser servers", category: "connections", terms: "address connection", scope: "This browser" },
  { id: "pair-phone", title: "Pair a phone", category: "connections", terms: "qr device" },
  { id: "pair-browser", title: "Pair this browser", category: "connections", terms: "qr scan", scope: "This browser" },
  { id: "tls", title: "TLS certificate", category: "connections", terms: "https trust ca" },
  { id: "workers", title: "Workers", category: "connections", terms: "handoff remote" },
  { id: "main-instances", title: "Main instances", category: "connections", terms: "handoff pairing" },
  { id: "move", title: "Move settings into the database", category: "server", terms: "config.yaml pins migration" },
  { id: "export", title: "Export and import settings", category: "server", terms: "backup restore" },
  { id: "server-theme", title: "Preferred theme", category: "server", terms: "server appearance default theme follow server", scope: "This server" },
  { id: "updates", title: "Updates", category: "server", terms: "release version restart" },
  { id: "development", title: "Development", category: "server", terms: "rebuild hot reload" },
  { id: "list-board.columns", title: "Board columns", category: "server", terms: "board.columns mapping" },
]
const aliases: Record<string, SettingsCategory> = { "integration-token-overview": "integrations", repositories: "agents", "repository-guidance": "agents", resources: "agents", appearance: "preferences", handoff: "connections", connection: "connections", lists: "server", updates: "server", development: "server" }
export function settingsDestination(id: string): SettingsCategory | undefined {
  if (id.startsWith("setting-")) return settingOwner(id.slice(8))
  if (id.startsWith("list-")) return settingOwner(id.slice(5))
  return settingsCards.find((card) => card.id === id)?.category ?? aliases[id] ?? settingsCategories.find((category) => category.id === id)?.id
}
