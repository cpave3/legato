import { useQuery } from "@tanstack/react-query"
import { apiFetch } from "../lib/api"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"
import type { UpdateStatus } from "../lib/updates"

export interface RepositorySetting {
  name: string
  repo: string
  primary_dir: string
  base_branch?: string
}

export interface Settings {
  ca_cert_available: boolean
  ntfy_configured: boolean
  voice_enabled: boolean
  voice_auto_send: boolean
  worktrees_enabled: boolean
  // Agents run in Legato's sandbox unless their spawn opts out.
  sandbox_enabled?: boolean
  // The spawn dialog offers a fresh clone of a repository (repository_cache.enabled).
  fresh_clones_enabled?: boolean
  hot_reload_enabled: boolean
  // Swarm features are on (swarm.enabled); off by default and absent from an older server.
  swarm_enabled?: boolean
  working_dir: string
  default_workspace_id: number | null
  default_import_workspace_id: number | null
  repositories: RepositorySetting[]
  // This server's tint (lib/tints), "" for none; absent from an older server.
  tint?: string
  // Where this server's updates stand; absent when it has no updater.
  updates?: UpdateStatus
}

export class SettingsRequestError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export async function fetchSettings(baseUrl: string): Promise<Settings> {
  const response = await apiFetch(baseUrl, "/api/settings")
  if (!response.ok) {
    throw new SettingsRequestError(response.status, await response.text().catch(() => response.statusText))
  }
  return response.json()
}

export function useSettings(baseUrlOverride?: string) {
  const server = useServer()
  const baseUrl = baseUrlOverride ?? server.baseUrl
  return useQuery({ queryKey: queryKeys.settings(baseUrl), queryFn: () => fetchSettings(baseUrl) })
}
