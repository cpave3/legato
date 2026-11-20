// The shape of Settings: its sections, in nav groups, and the groups within
// each. A registry setting's section and group come from the API
// (config/settings_layout.go); the cards, lists and file-only keys, which
// the registry does not place, are placed here. See docs/ai/settings.md
// § Web editors and navigation.

export type NavGroup = "You" | "Agents" | "Features" | "Services" | "Access" | "System"

export interface SettingsSection {
  id: string
  title: string
  nav: NavGroup
  // The groups in their order. A registry group not listed comes after
  // them; the lead group (a feature's own switch) always comes first.
  groups: string[]
  intro?: string
  // A section that is a page of its own (Macros).
  href?: string
  terms: string
}

// The group of a feature's own on/off switch (config.GroupLead): first in
// its section, with no heading.
export const leadGroup = "On or off"

// A setting whose section the registry does not know (a newer server)
// lands here, never nowhere.
export const otherGroup = "Other settings"

export const settingsSections: SettingsSection[] = [
  { id: "appearance", title: "Appearance", nav: "You", groups: ["This browser", "This Legato", "TUI"], terms: "theme terminal colours motion glitch animation tint tui icons activity" },
  { id: "attention", title: "Attention", nav: "You", groups: ["Needs-you rail", "Notify me", "What notifies"], terms: "notifications rail needs you ntfy push desktop phone approvals handoff" },
  { id: "input", title: "Input and terminal", nav: "You", groups: ["This browser", "Dictation", "TUI and tmux"], terms: "keyboard switch key prompt detection voice dictation whisper microphone editor vim tmux" },
  { id: "agents", title: "Agents", nav: "Agents", groups: ["Defaults", "Claude Code, Codex and Chimera", "What agents get", "Skills", "Resources", "Engrams"], terms: "agent tools adapters models tiers groups mcp tools skills resources engrams" },
  { id: "repositories", title: "Repositories", nav: "Agents", groups: ["Repositories and folders", "Fresh clones", "Worktrees"], terms: "repositories folders directory presets guidance clones cache worktrees yggdrasil" },
  { id: "sandbox", title: "Sandbox", nav: "Agents", groups: ["What agents see", "Docker", "Advanced"], terms: "bubblewrap bwrap mounts toolchains docker" },
  { id: "macros", title: "Macros", nav: "Agents", groups: [], href: "/macros", terms: "macros keys messages" },
  { id: "reeve", title: "Reeve", nav: "Features", groups: ["Session", "Approvals", "Host operations", "Watching agents", "Voice", "Engram", "Pairings", "Sessions"], terms: "reeve sessions automation rules engram pairings voice speech host operations" },
  { id: "swarms", title: "Swarms", nav: "Features", groups: ["Conductor and workers", "Limits", "Behaviour", "Advanced"], intro: "A conductor agent splits a task into a plan and hands its parts to worker agents, each in a scope of its own.", terms: "swarm conductor workers plan" },
  { id: "review-passes", title: "Review passes", nav: "Features", groups: ["Reviewer", "Limits", "Timing", "Bots and checks"], terms: "review passes reviewer pull requests checks bots" },
  { id: "learn", title: "Learn", nav: "Features", groups: ["Reflection", "Curator", "Prompts", "Semantic search", "Advanced"], terms: "learning lessons curator reflection embeddings" },
  { id: "board", title: "Board and tickets", nav: "Services", groups: ["Workspaces", "Columns", "Jira", "Burnlite"], terms: "board workspaces columns jira burnlite tickets import sync" },
  { id: "integrations", title: "Integrations", nav: "Services", groups: ["GitHub", "Argus", "MCP servers", "Home Assistant", "Manticore"], terms: "github argus mcp servers oauth home assistant mqtt broker manticore usage" },
  { id: "security", title: "Security", nav: "Access", groups: ["Approval devices", "Credentials", "Credential channels", "Integration tokens"], terms: "credentials secrets password grants audit approval devices channels tokens ssh" },
  { id: "connections", title: "Connections", nav: "Access", groups: ["This browser", "Phones", "Workers and mains", "Network"], terms: "browser servers pairing phone workers main instances handoff tls network discovery port" },
  { id: "system", title: "System", nav: "System", groups: ["Restart", "Updates", "config.yaml", "Only in config.yaml", "Development"], terms: "restart updates release configuration move export import database data folder development rebuild" },
]

export const navGroups: NavGroup[] = ["You", "Agents", "Features", "Services", "Access", "System"]

export function findSection(id: string): SettingsSection | undefined {
  return settingsSections.find((section) => section.id === id)
}

export interface Place { section: string; group: string }

// The cards, by their element IDs, with their homes.
export const settingsCards: (Place & { id: string; title: string; terms: string; scope?: string })[] = [
  { id: "app-appearance", title: "App and terminal theme", section: "appearance", group: "This browser", terms: "app terminal theme colours motion glitch animation", scope: "This browser" },
  { id: "tint", title: "Tint", section: "appearance", group: "This Legato", terms: "colour color identity" },
  { id: "server-theme", title: "Theme for devices that follow this Legato", section: "appearance", group: "This Legato", terms: "server appearance preferred default theme follow server" },
  { id: "needs-you-rail", title: "Needs-you rail", section: "attention", group: "Needs-you rail", terms: "rail attention" },
  { id: "browser-notifications", title: "In this browser", section: "attention", group: "Notify me", terms: "push approvals alert browser notifications", scope: "This browser" },
  { id: "keyboard", title: "Keyboard and prompt detection", section: "input", group: "This browser", terms: "switch modifier shortcuts approval terminal", scope: "This browser" },
  { id: "repositories", title: "Repositories", section: "repositories", group: "Repositories and folders", terms: "repositories workflows.repositories clone folder" },
  { id: "directory-presets", title: "Directory presets", section: "repositories", group: "Repositories and folders", terms: "directory folder path saved" },
  { id: "repository-guidance", title: "Repository guidance", section: "repositories", group: "Repositories and folders", terms: "agents.guidance instructions" },
  { id: "job-clones", title: "Managed clones", section: "repositories", group: "Fresh clones", terms: "remote cleanup" },
  { id: "skills", title: "Skills", section: "agents", group: "Skills", terms: "skill.md legato-skills plugin mod" },
  { id: "resources", title: "Resources", section: "agents", group: "Resources", terms: "files folders" },
  { id: "engram-bindings", title: "Engram bindings", section: "agents", group: "Engrams", terms: "memory" },
  { id: "reeve-auto", title: "Runs without asking", section: "reeve", group: "Approvals", terms: "automation" },
  { id: "reeve-engram", title: "Reeve engram", section: "reeve", group: "Engram", terms: "memory" },
  { id: "reeve-pairings", title: "Reeve pairings", section: "reeve", group: "Pairings", terms: "peers" },
  { id: "reeve-sessions", title: "Reeve sessions", section: "reeve", group: "Sessions", terms: "history" },
  { id: "list-board.columns", title: "Board columns", section: "board", group: "Columns", terms: "board.columns mapping" },
  { id: "burnlite", title: "Burnlite connection", section: "board", group: "Burnlite", terms: "burnlite connect oauth projects" },
  { id: "argus", title: "Argus", section: "integrations", group: "Argus", terms: "argus cases briefs delegations" },
  { id: "mcp-connections", title: "MCP servers", section: "integrations", group: "MCP servers", terms: "mcp oauth tools upstream notion linear connect authorize" },
  { id: "mqtt", title: "Home Assistant over MQTT", section: "integrations", group: "Home Assistant", terms: "broker smart home automation mqtt" },
  { id: "devices", title: "Approval devices", section: "security", group: "Approval devices", terms: "passkey signature phone" },
  { id: "credentials", title: "Credentials", section: "security", group: "Credentials", terms: "grants audit secrets passwords" },
  { id: "integration-tokens", title: "Integration tokens", section: "security", group: "Integration tokens", terms: "argus access scopes" },
  { id: "servers", title: "This browser's servers", section: "connections", group: "This browser", terms: "address connection", scope: "This browser" },
  { id: "pair-browser", title: "Pair this browser", section: "connections", group: "This browser", terms: "qr scan", scope: "This browser" },
  { id: "pair-phone", title: "Pair a phone", section: "connections", group: "Phones", terms: "qr device" },
  { id: "tls", title: "TLS certificate", section: "connections", group: "Phones", terms: "https trust ca" },
  { id: "workers", title: "Workers", section: "connections", group: "Workers and mains", terms: "handoff remote" },
  { id: "main-instances", title: "Main instances", section: "connections", group: "Workers and mains", terms: "handoff pairing" },
  { id: "restart", title: "Restart Legato", section: "system", group: "Restart", terms: "restart apply" },
  { id: "updates", title: "Updates", section: "system", group: "Updates", terms: "release version" },
  { id: "move", title: "Move out of config.yaml", section: "system", group: "config.yaml", terms: "config.yaml pins migration database" },
  { id: "export", title: "Export and import settings", section: "system", group: "config.yaml", terms: "backup restore" },
  { id: "development", title: "Development", section: "system", group: "Development", terms: "rebuild hot reload" },
]

// The lists of named entries, by key. Macros is its own page.
export const listPlaces: Record<string, Place> = {
  "repositories": { section: "repositories", group: "Repositories and folders" },
  "workflows.repositories": { section: "repositories", group: "Repositories and folders" },
  "agents.guidance": { section: "repositories", group: "Repositories and folders" },
  "resources": { section: "agents", group: "Resources" },
  "adapters": { section: "agents", group: "Claude Code, Codex and Chimera" },
  "macros": { section: "macros", group: "" },
  "agents.tmux_options": { section: "input", group: "TUI and tmux" },
  "agents.sandbox.mounts": { section: "sandbox", group: "What agents see" },
  "swarm.prompts": { section: "swarms", group: "Conductor and workers" },
  "reeve.rules": { section: "reeve", group: "Approvals" },
  "workspaces": { section: "board", group: "Workspaces" },
  "board.columns": { section: "board", group: "Columns" },
  "credentials": { section: "security", group: "Credentials" },
  "credential_sources": { section: "security", group: "Credentials" },
}

// Lists drawn by a card of their own rather than ListSettings; the card's
// element carries their list-<key> ID.
export const listsWithCards = ["repositories", "workflows.repositories", "agents.guidance", "resources", "macros", "board.columns"]

// The file-only keys (or key prefixes), shown read-only in their topic.
const fileOnlyPlaces: Record<string, Place> = {
  "db": { section: "system", group: "Only in config.yaml" },
  "data_dir": { section: "system", group: "Only in config.yaml" },
  "secrets": { section: "security", group: "Credentials" },
  "approval_devices": { section: "security", group: "Approval devices" },
  "web": { section: "connections", group: "Network" },
  "agents.repository_resources": { section: "repositories", group: "Repositories and folders" },
}

function byPrefix(places: Record<string, Place>, key: string): Place | undefined {
  let best: string | undefined
  for (const prefix of Object.keys(places)) {
    if ((key === prefix || key.startsWith(prefix + ".")) && (!best || prefix.length > best.length)) best = prefix
  }
  return best === undefined ? undefined : places[best]
}

export function fileOnlyPlace(key: string): Place {
  return byPrefix(fileOnlyPlaces, key) ?? { section: "system", group: "Only in config.yaml" }
}

export function listPlace(key: string): Place {
  return listPlaces[key] ?? { section: "system", group: otherGroup }
}

// Where a registry setting lives: the API's place, or System's other
// settings for a section this web UI does not know.
export function settingPlace(setting: { section?: string; group?: string }): Place {
  if (setting.section && findSection(setting.section)) return { section: setting.section, group: setting.group || otherGroup }
  return { section: "system", group: otherGroup }
}

// Settings drawn by a card of their own rather than a registry row; the
// card carries their setting-<key> ID.
export function hasOwnCard(key: string): boolean {
  return key === "web.tint" || key === "updates.mode" || key.startsWith("web.rail.") || key.startsWith("integrations.mqtt.")
}

// The old category IDs and other fragments links still use, and the home
// each now opens at.
const aliases: Record<string, Place & { element?: string }> = {
  preferences: { section: "appearance", group: "" },
  server: { section: "system", group: "" },
  "agents-and-workspaces": { section: "agents", group: "" },
  "connections-and-handoff": { section: "connections", group: "" },
  handoff: { section: "connections", group: "Workers and mains", element: "workers" },
  connection: { section: "connections", group: "" },
  lists: { section: "system", group: "" },
  "integration-token-overview": { section: "security", group: "Integration tokens", element: "integration-tokens" },
  "list-macros": { section: "macros", group: "" },
}

export interface Destination extends Place {
  // The element to open at, when the fragment names one.
  element?: string
}

// settingsDestination resolves a fragment: #<section>, #<section>/<group>,
// #setting-<key>, #list-<key>, a card's ID, or an old category or alias.
// A setting's own home comes from the registry, so it is known only once
// the settings have loaded.
export function settingsDestination(fragment: string, registry: { key: string; section?: string; group?: string }[] = [], fileOnly: string[] = []): Destination | undefined {
  if (!fragment) return undefined
  const [head, ...rest] = fragment.split("/")
  const section = findSection(head)
  if (section) {
    const wanted = rest.join("/")
    const group = section.groups.find((g) => slug(g) === wanted || g === wanted) ?? (wanted === slug(leadGroup) ? leadGroup : "")
    return { section: section.id, group, element: group ? groupElementId(section.id, group) : undefined }
  }
  if (fragment.startsWith("setting-")) {
    const key = fragment.slice("setting-".length)
    const setting = registry.find((s) => s.key === key)
    if (setting) return { ...settingPlace(setting), element: fragment }
    if (fileOnly.some((f) => key === f || key.startsWith(f + "."))) return { ...fileOnlyPlace(key), element: fragment }
    return undefined
  }
  if (aliases[fragment]) {
    const alias = aliases[fragment]
    return { section: alias.section, group: alias.group, element: alias.element }
  }
  if (fragment.startsWith("list-")) {
    const key = fragment.slice("list-".length)
    if (listPlaces[key]) return { ...listPlaces[key], element: fragment }
    return undefined
  }
  const card = settingsCards.find((c) => c.id === fragment)
  if (card) return { section: card.section, group: card.group, element: card.id }
  return undefined
}

export function slug(group: string): string {
  return group.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "")
}

// The element ID of a group's heading block.
export function groupElementId(section: string, group: string): string {
  return `${section}--${slug(group)}`
}

// The groups of a section in order: the lead (a feature's switch) first,
// then the catalog's, then any group a newer server names.
export function sectionGroups(section: SettingsSection, settings: { section?: string; group?: string }[]): string[] {
  const groups = [...section.groups]
  for (const s of settings) {
    const place = settingPlace(s)
    if (place.section === section.id && place.group !== leadGroup && !groups.includes(place.group)) groups.push(place.group)
  }
  return settings.some((s) => { const p = settingPlace(s); return p.section === section.id && p.group === leadGroup }) ? [leadGroup, ...groups] : groups
}
