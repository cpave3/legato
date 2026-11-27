import { apiFetch, ok } from "./api"

// Guidance Legato adds to the launch prompt of every agent that works in a
// repository, whichever checkout or worktree it is in. That of config.yaml
// is listed but changed there; the rest is saved in Settings.
export interface RepositoryGuidance {
  // host/owner/name, as the server normalises it.
  repo: string
  text: string
  configured: boolean
}

const path = "/api/settings/repository-guidance"

export async function listRepositoryGuidance(baseUrl: string): Promise<RepositoryGuidance[]> {
  const response = await ok(await apiFetch(baseUrl, path))
  const body = await response.json()
  return Array.isArray(body?.guidance) ? body.guidance : []
}

export async function saveRepositoryGuidance(baseUrl: string, repo: string, text: string): Promise<RepositoryGuidance> {
  const response = await ok(await apiFetch(baseUrl, path, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ repo, text }),
  }))
  return response.json()
}

export async function removeRepositoryGuidance(baseUrl: string, repo: string): Promise<void> {
  await ok(await apiFetch(baseUrl, `${path}?repo=${encodeURIComponent(repo)}`, { method: "DELETE" }))
}
