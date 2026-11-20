import { apiFetch } from "./api"

// A setting of the registry with its value and where it comes from
// (GET /api/config/settings, service.SettingValue).
export interface ConfigSetting {
  key: string
  title: string
  // Its home in Settings: a section ID and a group within it (absent from
  // a server older than the redesign).
  section?: string
  group?: string
  // One line on what it does. description is its old name, sent for one
  // release.
  help?: string
  description?: string
  unit?: "seconds" | "ms" | "bytes"
  // What it is when unset, when that is not its zero value.
  default_label?: string
  kind: "bool" | "string" | "int" | "enum" | "duration" | "strings"
  enum?: string[]
  class: "read" | "change" | "risky"
  mode: "live" | "new_agents" | "restart"
  secret?: boolean
  // One of the Reeve's own rules: changed only with a signed approval.
  own_rule?: boolean
  value: unknown
  source: "default" | "config.yaml" | "database"
  pinned: boolean
  pending_restart?: boolean
  // While it waits for a restart, the value Legato still uses.
  started_value?: unknown
  // The value with nothing set: what Reset goes back to.
  default?: unknown
  // A secret setting's secret: whether Legato can read it.
  secret_status?: "stored" | "missing"
  updated_at?: string
  updated_by?: string
}

async function failed(response: Response): Promise<Error> {
  const body = await response.json().catch(() => null) as { error?: string } | null
  return new Error(body?.error || response.statusText)
}

export async function fetchConfigSettings(baseUrl: string): Promise<{ settings: ConfigSetting[]; file_only: string[]; file_only_values: Record<string, string> }> {
  const response = await apiFetch(baseUrl, "/api/config/settings")
  if (!response.ok) throw await failed(response)
  const body = await response.json() as { settings?: ConfigSetting[]; file_only?: string[]; file_only_values?: Record<string, string> }
  return { settings: body.settings ?? [], file_only: body.file_only ?? [], file_only_values: body.file_only_values ?? {} }
}

export async function setConfigSetting(baseUrl: string, key: string, value: unknown): Promise<ConfigSetting> {
  const response = await apiFetch(baseUrl, `/api/config/settings/${encodeURIComponent(key)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ value }),
  })
  if (!response.ok) throw await failed(response)
  return response.json()
}

export async function resetConfigSetting(baseUrl: string, key: string): Promise<ConfigSetting> {
  const response = await apiFetch(baseUrl, `/api/config/settings/${encodeURIComponent(key)}`, { method: "DELETE" })
  if (!response.ok) throw await failed(response)
  return response.json()
}

// One entry of a list of named entries (service.SettingEntryValue).
export interface ConfigListEntry {
  name: string
  value: unknown
  source: "config.yaml" | "database" | "default"
  pinned: boolean
  updated_at?: string
  // A built-in macro: edited (overridden) or as shipped; reset, never removed.
  builtin?: boolean
  overridden?: boolean
}

// A list of named entries, the file's and the database's.
export interface ConfigList {
  key: string
  title: string
  class: "read" | "change" | "risky"
  mode: "live" | "new_agents" | "restart"
  name_field?: string
  entries: ConfigListEntry[]
  // Entries added, changed or removed here since Legato started, which
  // wait for a restart (a restart list only).
  pending_entries?: string[]
}

export async function fetchConfigLists(baseUrl: string): Promise<ConfigList[]> {
  const response = await apiFetch(baseUrl, "/api/config/lists")
  if (!response.ok) throw await failed(response)
  const body = await response.json() as { lists: ConfigList[] }
  return body.lists ?? []
}

export async function saveConfigListEntry(baseUrl: string, list: string, name: string, value: unknown): Promise<{ status?: string }> {
  const response = await apiFetch(baseUrl, `/api/config/lists/${encodeURIComponent(list)}/${encodeURIComponent(name)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ value }),
  })
  if (!response.ok) throw await failed(response)
  return response.json()
}

export async function removeConfigListEntry(baseUrl: string, list: string, name: string): Promise<{ status?: string }> {
  const response = await apiFetch(baseUrl, `/api/config/lists/${encodeURIComponent(list)}/${encodeURIComponent(name)}`, { method: "DELETE" })
  if (!response.ok) throw await failed(response)
  return response.json()
}

// What can move from config.yaml into the database (GET /api/config/move).
export interface SettingsMove {
  settings: string[]
  entries: { list: string; name: string }[]
}

export async function fetchMovable(baseUrl: string): Promise<SettingsMove> {
  const response = await apiFetch(baseUrl, "/api/config/move")
  if (!response.ok) throw await failed(response)
  return response.json()
}

// moveSettings moves settings into the database; Legato restarts after it.
export async function moveSettings(baseUrl: string, move: SettingsMove): Promise<void> {
  const response = await apiFetch(baseUrl, "/api/config/move", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(move),
  })
  if (!response.ok) throw await failed(response)
}

// exportSettings fetches the settings set on the server as YAML shaped like
// config.yaml (GET /api/config/export); pinned adds config.yaml's own.
export async function exportSettings(baseUrl: string, pinned: boolean): Promise<string> {
  const response = await apiFetch(baseUrl, `/api/config/export${pinned ? "?pinned=1" : ""}`)
  if (!response.ok) throw await failed(response)
  return response.text()
}

// What an import proposed (POST /api/config/import).
export interface ImportOutcome {
  id: string
  approval_id: string
  level: string
  calls: number
  skipped?: string[]
}

// importSettings sends a file written by Export settings; it becomes one
// change set to approve in the tray.
export async function importSettings(baseUrl: string, text: string): Promise<ImportOutcome> {
  const response = await apiFetch(baseUrl, "/api/config/import", {
    method: "POST",
    headers: { "Content-Type": "application/yaml" },
    body: text,
  })
  if (!response.ok) throw await failed(response)
  return response.json()
}
