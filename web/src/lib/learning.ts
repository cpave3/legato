import { apiFetch } from "./api"

export interface Learning {
  id: string
  scope: "user" | "project"
  project_key?: string
  kind: string
  category?: string
  tags: string[]
  claim: string
  rationale?: string
  status: string
  confidence: number
  prompt_enabled: boolean
  prompt_decided: boolean
  revision: number
  created_at: string
  updated_at: string
}

export interface LearningEvidence {
  id: string
  source_kind: string
  source_ref: string
  content: string
  content_digest: string
  author: string
  task_id?: string
  workflow_run_id?: string
  project_key?: string
  project_label?: string
  created_at: string
}

export interface LearningIncorporation {
  id: string
  destination: string
  target?: string
  action?: string
  status: string
  revision: number
  references_json: string
  verification_kind?: string
  verification_result?: string
  verified_at?: string
  created_at: string
  updated_at: string
}

export interface LearningView {
  learning: Learning
  evidence: LearningEvidence[]
  incorporations: LearningIncorporation[]
}

async function json<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string } | null
    throw new Error(body?.error || `Request failed (${response.status})`)
  }
  return response.json() as Promise<T>
}

export interface LearningSearchResult {
  items: Learning[]
  mode: "lexical" | "hybrid"
  semantic_status: "disabled" | "available" | "failed"
  fallback?: string
}

export function fetchLearnings(baseUrl: string, filters: { query?: string; status?: string; scope?: string; projectKey?: string; category?: string; tags?: string[] } = {}): Promise<LearningSearchResult> {
  const params = new URLSearchParams()
  if (filters.status) params.set("status", filters.status)
  if (filters.scope) params.set("scope", filters.scope)
  if (filters.projectKey) params.set("project_key", filters.projectKey)
  if (filters.category) params.set("category", filters.category)
  filters.tags?.forEach(tag => params.append("tag", tag))
  if (filters.query) params.set("q", filters.query)
  const endpoint = filters.query ? "/api/learnings/search" : "/api/learnings"
  const suffix = params.size ? `?${params}` : ""
  return apiFetch(baseUrl, endpoint + suffix).then(async response => {
    if (filters.query) return json<LearningSearchResult>(response)
    return { items: await json<Learning[]>(response), mode: "lexical", semantic_status: "disabled" }
  })
}

export function fetchLearning(baseUrl: string, id: string) {
  return apiFetch(baseUrl, `/api/learnings/${encodeURIComponent(id)}`).then(json<LearningView>)
}

export function setLearningPromptEnabled(baseUrl: string, learningId: string, revision: number, enabled: boolean) {
  return apiFetch(baseUrl, `/api/learnings/${encodeURIComponent(learningId)}/prompt`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ revision, enabled }),
  }).then(json<LearningView>)
}

export interface LearningPromptPreview {
  rendered: string
  project_key?: string
  included: Array<{ id: string; revision: number }>
  excluded: Array<{ id: string; reason: string }>
}

export function fetchLearningPromptPreview(baseUrl: string, taskId: string) {
  const params = new URLSearchParams({ task_id: taskId })
  return apiFetch(baseUrl, `/api/learnings/prompt-preview?${params}`).then(json<LearningPromptPreview>)
}

export function proposeIncorporation(baseUrl: string, learningId: string, body: { revision: number; destination: string; target: string; action?: string }) {
  return apiFetch(baseUrl, `/api/learnings/${encodeURIComponent(learningId)}/incorporations`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  }).then(json<LearningIncorporation>)
}

export function submitIncorporation(baseUrl: string, id: string, body: { revision: number; references: string[]; verification_kind: string; result?: string }) {
  return apiFetch(baseUrl, `/api/learning-incorporations/${encodeURIComponent(id)}/submit`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body),
  }).then(json<LearningIncorporation>)
}

export function verifyIncorporationTarget(baseUrl: string, id: string, revision: number) {
  return apiFetch(baseUrl, `/api/learning-incorporations/${encodeURIComponent(id)}/verify-target`, {
    method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ revision, repository_root: "." }),
  }).then(json<LearningIncorporation>)
}

export function decideLearning(baseUrl: string, id: string, action: "accept" | "edit" | "reject", body: { revision: number; claim?: string; rationale?: string; scope?: string; project_key?: string; category?: string; tags?: string[] }) {
  return apiFetch(baseUrl, `/api/learnings/${encodeURIComponent(id)}/${action}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }).then(json<LearningView>)
}
