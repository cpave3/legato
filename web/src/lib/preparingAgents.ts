import { apiFetch } from "./api"

// An agent the server is starting in the background while its clone is
// made. One that failed stays, with why, until it is dismissed.
export interface PreparingAgent {
  task_id: string
  title: string
  agent_kind?: string
  // The repository cloned for it, or the job clone it joins.
  repo?: string
  clone_id?: number
  started_at: string
  error?: string
}

// How a repository's copy in the server's cache stands, with git's
// progress while it is copied or fetched.
export interface RepositoryCacheStatus {
  repo: string
  state: "missing" | "copying" | "fetching" | "ready" | "failed"
  phase?: string
  percent?: number
  received?: string
  rate?: string
  error?: string
  fetched_at?: string
}

// The agents being started; an older server has none.
export async function fetchPreparingAgents(baseUrl: string): Promise<PreparingAgent[]> {
  const response = await apiFetch(baseUrl, "/api/preparing-agents")
  if (!response.ok) return []
  const body = await response.json()
  return Array.isArray(body) ? body : []
}

export async function dismissPreparingAgent(baseUrl: string, taskId: string): Promise<void> {
  const response = await apiFetch(baseUrl, `/api/preparing-agents/${encodeURIComponent(taskId)}`, { method: "DELETE" })
  if (!response.ok && response.status !== 404) throw new Error(await response.text())
}

// Each repository's copy, by owner/name, from the repository list.
export async function fetchRepositoryCache(baseUrl: string): Promise<Record<string, RepositoryCacheStatus>> {
  const response = await apiFetch(baseUrl, "/api/repositories")
  if (!response.ok) return {}
  const body = await response.json()
  const out: Record<string, RepositoryCacheStatus> = {}
  for (const repository of Array.isArray(body?.repositories) ? body.repositories : []) {
    if (repository?.cache?.repo) out[repository.cache.repo] = repository.cache
  }
  return out
}

// git's progress in a few words: "Receiving objects · 38% · 310.00 MiB · 3.00 MiB/s".
export function progressDetail(status: RepositoryCacheStatus): string {
  return [status.phase, status.percent != null ? `${status.percent}%` : "", status.received, status.rate].filter(Boolean).join(" · ")
}

// What an agent being started is waiting for, and how far along it is.
export function describePreparing(agent: PreparingAgent, cache: RepositoryCacheStatus | undefined): {
  line: string
  detail: string
  percent?: number
  failed: boolean
} {
  if (agent.error) return { line: agent.error, detail: "", failed: true }
  if (agent.repo && cache && (cache.state === "copying" || cache.state === "fetching")) {
    return {
      line: `${cache.state === "copying" ? "Copying" : "Fetching"} ${agent.repo}`,
      detail: progressDetail(cache),
      percent: cache.percent,
      failed: false,
    }
  }
  return { line: "Getting its clone ready…", detail: "", failed: false }
}
