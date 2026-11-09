import { apiFetch } from "./api"

// A setting of the registry with its value and where it comes from
// (GET /api/config/settings, service.SettingValue).
export interface ConfigSetting {
  key: string
  title: string
  description?: string
  kind: "bool" | "string" | "int" | "enum" | "duration" | "strings"
  enum?: string[]
  class: "read" | "change" | "risky"
  mode: "live" | "new_agents" | "restart"
  secret?: boolean
  value: unknown
  source: "default" | "config.yaml" | "database"
  pinned: boolean
  pending_restart?: boolean
  updated_at?: string
  updated_by?: string
}

async function failed(response: Response): Promise<Error> {
  const body = await response.json().catch(() => null) as { error?: string } | null
  return new Error(body?.error || response.statusText)
}

export async function fetchConfigSettings(baseUrl: string): Promise<ConfigSetting[]> {
  const response = await apiFetch(baseUrl, "/api/config/settings")
  if (!response.ok) throw await failed(response)
  const body = await response.json() as { settings: ConfigSetting[] }
  return body.settings ?? []
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
  source: "config.yaml" | "database"
  pinned: boolean
  updated_at?: string
}

// A list of named entries, the file's and the database's.
export interface ConfigList {
  key: string
  title: string
  class: "read" | "change" | "risky"
  mode: "live" | "new_agents" | "restart"
  name_field?: string
  entries: ConfigListEntry[]
}

export async function fetchConfigLists(baseUrl: string): Promise<ConfigList[]> {
  const response = await apiFetch(baseUrl, "/api/config/lists")
  if (!response.ok) throw await failed(response)
  const body = await response.json() as { lists: ConfigList[] }
  return body.lists ?? []
}

export async function saveConfigListEntry(baseUrl: string, list: string, name: string, value: unknown): Promise<void> {
  const response = await apiFetch(baseUrl, `/api/config/lists/${encodeURIComponent(list)}/${encodeURIComponent(name)}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ value }),
  })
  if (!response.ok) throw await failed(response)
}

export async function removeConfigListEntry(baseUrl: string, list: string, name: string): Promise<void> {
  const response = await apiFetch(baseUrl, `/api/config/lists/${encodeURIComponent(list)}/${encodeURIComponent(name)}`, { method: "DELETE" })
  if (!response.ok) throw await failed(response)
}
