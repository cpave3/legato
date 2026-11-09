import { apiFetch } from "./api"

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
