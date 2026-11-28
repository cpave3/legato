import { apiFetch } from "./api"

// Where a server's updates stand (GET /api/updates, service.UpdateStatus).
export interface UpdateStatus {
  version: string
  // A release updates itself; a build from source ("dev") does not.
  release: boolean
  mode: UpdateMode
  state: "unchecked" | "up_to_date" | "available" | "waiting" | "installing" | "restarting" | "installed" | "failed"
  // A release newer than what this server has is out.
  available: boolean
  latest?: string
  latest_url?: string
  checked_at?: string
  error?: string
  // Command-line tools kept current beside Legato (st, chimera and Claude Code).
  tools?: ToolStatus[]
}

// A tool Legato keeps current (service.ToolStatus).
export interface ToolStatus {
  name: string
  path?: string
  // A release build on Legato's PATH, which updates itself.
  managed: boolean
  version?: string
  latest?: string
  available: boolean
  state: "missing" | "unmanaged" | "unchecked" | "up_to_date" | "available" | "installing" | "failed"
  error?: string
  checked_at?: string
  // Claude Code: the release channel followed (stable or latest).
  channel?: string
  // The version a rollback returns to.
  last_good?: string
  // A rollback to last_good is possible now.
  can_rollback?: boolean
  // A version rolled back from, not offered again until a newer one is out.
  held?: string
  // What updating does, or whose the tool is when Legato leaves it alone.
  detail?: string
}

export type UpdateMode = "auto" | "ask" | "off"

async function failed(response: Response): Promise<Error> {
  const body = await response.json().catch(() => null) as { error?: string } | null
  return new Error(body?.error || response.statusText)
}

export async function checkForUpdates(baseUrl: string): Promise<UpdateStatus> {
  const response = await apiFetch(baseUrl, "/api/updates/check", { method: "POST" })
  if (!response.ok) throw await failed(response)
  return response.json()
}

// applyUpdate starts the update; update_changed says how it goes.
export async function applyUpdate(baseUrl: string): Promise<void> {
  const response = await apiFetch(baseUrl, "/api/updates/apply", { method: "POST" })
  if (!response.ok) throw await failed(response)
}

export async function setUpdateMode(baseUrl: string, mode: UpdateMode): Promise<UpdateStatus> {
  const response = await apiFetch(baseUrl, "/api/settings/updates", {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ mode }),
  })
  if (!response.ok) throw await failed(response)
  return response.json()
}

// applyToolUpdate starts a tool's update (st update or chimera update); update_changed says how it goes.
export async function applyToolUpdate(baseUrl: string, name: string): Promise<void> {
  const response = await apiFetch(baseUrl, `/api/updates/tools/${encodeURIComponent(name)}/apply`, { method: "POST" })
  if (!response.ok) throw await failed(response)
}

// rollbackTool starts switching a tool back to its last known good version (Claude Code); update_changed says how it goes.
export async function rollbackTool(baseUrl: string, name: string): Promise<void> {
  const response = await apiFetch(baseUrl, `/api/updates/tools/${encodeURIComponent(name)}/rollback`, { method: "POST" })
  if (!response.ok) throw await failed(response)
}
