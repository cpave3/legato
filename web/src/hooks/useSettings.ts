import { useQuery } from "@tanstack/react-query"
import { apiFetch } from "../lib/api"
import { queryKeys } from "../lib/query"
import { useServer } from "./useServer"

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
  working_dir: string
  repositories: RepositorySetting[]
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
