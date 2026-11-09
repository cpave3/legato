import { apiFetch } from "./api"

// One session of the Reeve (store.ReeveSession).
export interface ReeveSession {
  task_id: string
  state: "running" | "suspended" | "ended"
  agent_kind: string
  model?: string
  title: string
  started_by?: string
  created_at: string
  updated_at: string
  active_at: string
}

// The Reeve's current session and its history (GET /api/reeve).
export interface ReeveOverview {
  current?: ReeveSession
  history: ReeveSession[]
  folder: string
}

// How a session starts: an override of the agent tool and model, a brief.
export interface ReeveStart {
  agent?: string
  model?: string
  brief?: string
}

async function failed(response: Response): Promise<Error> {
  const body = await response.json().catch(() => null) as { error?: string } | null
  return new Error(body?.error || response.statusText)
}

export async function fetchReeve(baseUrl: string): Promise<ReeveOverview> {
  const response = await apiFetch(baseUrl, "/api/reeve")
  if (!response.ok) throw await failed(response)
  return response.json()
}

async function post(baseUrl: string, path: string, body?: unknown, method = "POST"): Promise<unknown> {
  const response = await apiFetch(baseUrl, path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!response.ok) throw await failed(response)
  return response.json()
}

export const startReeve = (baseUrl: string, start: ReeveStart) => post(baseUrl, "/api/reeve/sessions", start) as Promise<ReeveSession>
export const endReeve = (baseUrl: string, taskId: string) => post(baseUrl, `/api/reeve/sessions/${encodeURIComponent(taskId)}/end`)
export const resumeReeve = (baseUrl: string, taskId: string, start: ReeveStart = {}) => post(baseUrl, `/api/reeve/sessions/${encodeURIComponent(taskId)}/resume`, start) as Promise<ReeveSession>
export const deleteReeve = (baseUrl: string, taskId: string) => post(baseUrl, `/api/reeve/sessions/${encodeURIComponent(taskId)}`, undefined, "DELETE")
