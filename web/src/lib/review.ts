import { apiFetch } from "./api"

export type ReviewStatus = "capturing" | "ready" | "reviewed"
export type ReviewRisk = "" | "low" | "medium" | "high" | "unsure"
export type ReviewStepKind = "commit" | "dirty" | "note" | "chapter"

export interface ReviewQueueItem {
  task_id: string
  tour_id: string
  name: string
  title: string
  status: ReviewStatus
  summary: string
  unreviewed: number
  updated_at: string
  ready_at: string | null
  activity_at: string
}

export interface ReviewTour {
  id: string
  task_id: string
  name: string
  status: ReviewStatus
  summary: string
  base_sha: string
  head_sha: string
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

export interface ReviewHunkNote {
  id: string
  task_id: string
  step_id: string
  file_path: string
  hunk_anchor: string
  line_start?: number
  line_end?: number
  line_anchor?: string
  body: string
  created_at: string
}

export interface ReviewFinding {
  id: string
  pass_id: string
  step_id?: string
  file_path?: string
  hunk_anchor?: string
  line_start?: number
  line_end?: number
  body: string
  status: "open" | "resolved"
  resolved_at?: string
}

export interface ReviewPassPlan {
  plan_id: string
  revision_id: string
  revision: number
  title: string
  markdown: string
}

export interface ReviewPass {
  id: string
  number: number
  status: "capturing" | "ready" | "reviewed" | "superseded"
  summary: string
  guidance: string
}

export interface ReviewPassView {
  pass: ReviewPass
  captured_plan?: ReviewPassPlan
  steps: ReviewStep[]
  messages: ReviewMessage[]
  hunk_notes: ReviewHunkNote[]
  findings: ReviewFinding[]
  plan_requests: Array<{ id: string; finding_ids: string[]; delivered_at?: string }>
}

export interface ReviewTourView {
  tour: ReviewTour
  passes?: ReviewPassView[]
  steps: ReviewStep[]
  messages: ReviewMessage[]
  hunk_notes: ReviewHunkNote[]
}

export type DiffLineKind = "ctx" | "add" | "del"

export interface DiffSelection {
  file_path: string
  hunk_anchor: string
  start: number
  end: number
}

export interface DiffLine {
  kind: DiffLineKind
  old_no: number
  new_no: number
  text: string
}

export interface DiffHunk {
  header: string
  anchor: string
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

const tourPath = (tourId: string) => `/api/review/tours/${encodeURIComponent(tourId)}`
const stepPath = (tourId: string, stepId: string) => `${tourPath(tourId)}/steps/${encodeURIComponent(stepId)}`

export async function fetchReviewQueue(baseUrl: string): Promise<ReviewQueueItem[]> {
  return expectJSON<ReviewQueueItem[]>(await apiFetch(baseUrl, "/api/review/queue"))
}

export async function fetchReviewTour(baseUrl: string, tourId: string): Promise<ReviewTourView> {
  return expectJSON<ReviewTourView>(await apiFetch(baseUrl, tourPath(tourId)))
}

export async function fetchStepDiff(baseUrl: string, tourId: string, stepId: string): Promise<FileDiff[]> {
  return expectJSON<FileDiff[]>(await apiFetch(baseUrl, `${stepPath(tourId, stepId)}/diff`))
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

export async function setStepReviewed(baseUrl: string, tourId: string, stepId: string, reviewed: boolean): Promise<void> {
  await expectOK(await postJSON(baseUrl, `${stepPath(tourId, stepId)}/reviewed`, { reviewed }))
}

export async function askReviewQuestion(baseUrl: string, tourId: string, stepId: string, text: string, selection?: DiffSelection | null): Promise<string | undefined> {
  const response = await postJSON(baseUrl, `${stepPath(tourId, stepId)}/question`, {
    text,
    ...(selection ? { selection } : {}),
  })
  if (!response.ok) {
    await expectOK(response)
    return undefined
  }
  const result = await response.json().catch(() => ({})) as { warning?: string }
  return result.warning
}

export async function createReviewFinding(baseUrl: string, tourId: string, body: string, stepId: string, selection?: DiffSelection | null): Promise<ReviewFinding> {
  return expectJSON<ReviewFinding>(await postJSON(baseUrl, `${tourPath(tourId)}/findings`, {
    body,
    selection: {
      step_id: stepId,
      ...(selection ? {
        file_path: selection.file_path,
        hunk_anchor: selection.hunk_anchor,
        line_start: selection.start + 1,
        line_end: selection.end + 1,
      } : {}),
    },
  }))
}

export async function requestFollowUpPlan(baseUrl: string, tourId: string, findingIds: string[]): Promise<string | undefined> {
  const response = await postJSON(baseUrl, `${tourPath(tourId)}/request-plan`, { finding_ids: findingIds })
  const result = await expectJSON<{ warning?: string }>(response)
  return result.warning
}

export async function regenerateReview(baseUrl: string, tourId: string, feedback: string): Promise<void> {
  await expectOK(await postJSON(baseUrl, `${tourPath(tourId)}/regenerate`, { feedback }))
}

export async function completeReview(baseUrl: string, tourId: string): Promise<void> {
  await expectOK(await postJSON(baseUrl, `${tourPath(tourId)}/complete`))
}

export async function deleteReview(baseUrl: string, tourId: string): Promise<void> {
  await expectOK(await apiFetch(baseUrl, tourPath(tourId), { method: "DELETE" }))
}
