import { apiFetch } from "./api"

export type ReviewStatus = "capturing" | "ready" | "reviewed"
export type ReviewRisk = "" | "low" | "medium" | "high" | "unsure"
export type ReviewStepKind = "commit" | "dirty" | "note"

export interface ReviewQueueItem {
  task_id: string
  title: string
  status: ReviewStatus
  summary: string
  unreviewed: number
}

export interface ReviewTour {
  task_id: string
  status: ReviewStatus
  summary: string
  base_sha: string
  last_reviewed_sha: string
  ready_at: string | null
  created_at: string
  updated_at: string
}

export interface ReviewStep {
  id: string
  task_id: string
  kind: ReviewStepKind
  commit_sha: string
  files: string
  title: string
  narration: string
  risk: ReviewRisk
  order_hint: number | null
  seq: number
  subtask_id: string
  dirty_fingerprint: string
  reviewed_at: string | null
  orphaned_at: string | null
  created_at: string
  updated_at: string
}

export interface ReviewMessage {
  id: number
  task_id: string
  step_id: string
  kind: "question" | "answer"
  author: "user" | "agent"
  body: string
  delivered_at: string | null
  created_at: string
}

export interface ReviewTourView {
  tour: ReviewTour
  steps: ReviewStep[]
  messages: ReviewMessage[]
}

export type DiffLineKind = "ctx" | "add" | "del"

export interface DiffLine {
  kind: DiffLineKind
  old_no: number
  new_no: number
  text: string
}

export interface DiffHunk {
  header: string
  lines: DiffLine[]
}

export interface FileDiff {
  old_path: string
  new_path: string
  status: "modified" | "added" | "deleted" | "renamed" | "binary"
  hunks: DiffHunk[]
}

async function expectJSON<T>(response: Response): Promise<T> {
  if (!response.ok) {
    const message = await response.text().catch(() => "")
    throw new Error(message || response.statusText || `Request failed (${response.status})`)
  }
  return response.json() as Promise<T>
}

async function expectOK(response: Response): Promise<void> {
  if (!response.ok) {
    const message = await response.text().catch(() => "")
    throw new Error(message || response.statusText || `Request failed (${response.status})`)
  }
}

const taskPath = (taskId: string) => `/api/tasks/${encodeURIComponent(taskId)}/review`
const stepPath = (taskId: string, stepId: string) => `${taskPath(taskId)}/steps/${encodeURIComponent(stepId)}`

export async function fetchReviewQueue(baseUrl: string): Promise<ReviewQueueItem[]> {
  return expectJSON<ReviewQueueItem[]>(await apiFetch(baseUrl, "/api/review/queue"))
}

export async function fetchReviewTour(baseUrl: string, taskId: string): Promise<ReviewTourView> {
  return expectJSON<ReviewTourView>(await apiFetch(baseUrl, taskPath(taskId)))
}

export async function fetchStepDiff(baseUrl: string, taskId: string, stepId: string): Promise<FileDiff[]> {
  return expectJSON<FileDiff[]>(await apiFetch(baseUrl, `${stepPath(taskId, stepId)}/diff`))
}

function postJSON(baseUrl: string, path: string, body?: unknown): Promise<Response> {
  return apiFetch(baseUrl, path, {
    method: "POST",
    ...(body === undefined ? {} : {
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }),
  })
}

export async function setStepReviewed(baseUrl: string, taskId: string, stepId: string, reviewed: boolean): Promise<void> {
  await expectOK(await postJSON(baseUrl, `${stepPath(taskId, stepId)}/reviewed`, { reviewed }))
}

export async function askReviewQuestion(baseUrl: string, taskId: string, stepId: string, text: string): Promise<string | undefined> {
  const response = await postJSON(baseUrl, `${stepPath(taskId, stepId)}/question`, { text })
  if (!response.ok) {
    await expectOK(response)
    return undefined
  }
  const result = await response.json().catch(() => ({})) as { warning?: string }
  return result.warning
}

export async function completeReview(baseUrl: string, taskId: string): Promise<void> {
  await expectOK(await postJSON(baseUrl, `${taskPath(taskId)}/complete`))
}
