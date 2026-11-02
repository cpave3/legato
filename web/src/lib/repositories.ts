import type { RepositoryCacheStatus } from "./preparingAgents"
import { apiFetch } from "./api"

// A repository Legato offers: a GitHub repository the spawn dialog can clone
// fresh, a checkout on the server, or both. Those of config.yaml are listed
// but changed there; others are added in Settings.
export interface Repository {
  // Absent for a repository of the configuration.
  id?: number
  name: string
  // owner/name on GitHub; empty for a folder only.
  repo: string
  primary_dir: string
  base_branch?: string
  configured: boolean
  // Why Legato cannot clone repo (its GitHub credential does not list it),
  // absent when it can.
  clone_problem?: string
  // How repo's copy in Legato's cache stands; absent with no repo.
  cache?: RepositoryCacheStatus
}

export interface NewRepository {
  name: string
  repo: string
  base_branch: string
  primary_dir: string
}

async function ok(response: Response): Promise<Response> {
  if (response.ok) return response
  let message = response.statusText
  try {
    const body = await response.json()
    if (body && typeof body.error === "string") message = body.error
  } catch {
    // The body was not JSON; the status says enough.
  }
  throw new Error(message || `request failed (${response.status})`)
}

export async function listRepositories(baseUrl: string): Promise<Repository[]> {
  const response = await ok(await apiFetch(baseUrl, "/api/repositories"))
  const body = await response.json()
  return Array.isArray(body?.repositories) ? body.repositories : []
}

export async function addRepository(baseUrl: string, repository: NewRepository): Promise<Repository> {
  const response = await ok(await apiFetch(baseUrl, "/api/repositories", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(repository),
  }))
  return response.json()
}

export async function removeRepository(baseUrl: string, id: number): Promise<void> {
  await ok(await apiFetch(baseUrl, `/api/repositories/${id}`, { method: "DELETE" }))
}
