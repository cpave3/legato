import { apiFetch } from "./api"

export type ReportPeriodKind = "day" | "week" | "month"

export interface ReportPeriod {
  kind: ReportPeriodKind
  offset: number
  label: string
  start: string
  end: string
}

export interface ReportSummary {
  working_seconds: number
  waiting_seconds: number
  error_seconds: number
  tasks_created: number
  tasks_completed: number
  agent_sessions: number
  prs_merged: number
}

export interface ReportDay { date: string; working_seconds: number; waiting_seconds: number; error_seconds: number }
export interface ReportTask { task_id: string; display_id: string; title: string; workspace_name: string; provider: string; working_seconds: number; waiting_seconds: number; error_seconds: number }
export interface ReportWorkspace { workspace_id: number | null; workspace_name: string; workspace_color: string; working_seconds: number; waiting_seconds: number; error_seconds: number; task_count: number }
export interface ReportDirectory { directory: string; working_seconds: number; waiting_seconds: number; error_seconds: number; task_count: number }
export interface ReportSwarm { parent_task_id: string; title: string; working_seconds: number; waiting_seconds: number; error_seconds: number; wall_clock_seconds: number; parallel_ratio: number; worker_count: number; subtask_count: number }
export interface ReportExecution { id: string; kind: string; status: string; task_id: string; task_title: string; workflow_run_id: string; working_seconds: number; waiting_seconds: number; error_seconds: number; wall_clock_seconds: number; started_at: string; ended_at: string | null }

export interface Report {
  period: ReportPeriod
  activity_epoch: string
  activity_available: boolean
  summary: ReportSummary
  by_day: ReportDay[]
  by_task: ReportTask[]
  by_workspace: ReportWorkspace[]
  by_directory: ReportDirectory[]
  by_swarm: ReportSwarm[]
  by_execution: ReportExecution[]
}

export interface ReportEnvelope {
  report: Report
  comparison: { period: ReportPeriod; activity_available: boolean; summary: ReportSummary }
}

export interface ReportRequest { period: ReportPeriodKind; offset: number; signal?: AbortSignal }

async function responseError(response: Response): Promise<Error> {
  const text = await response.text().catch(() => "")
  try {
    const decoded = JSON.parse(text) as { error?: string }
    return new Error(decoded.error || `Request failed (${response.status})`)
  } catch {
    return new Error(text || `Request failed (${response.status})`)
  }
}

export async function fetchReport(baseUrl: string, request: ReportRequest): Promise<ReportEnvelope> {
  const params = new URLSearchParams({ period: request.period, offset: String(request.offset) })
  const response = await apiFetch(baseUrl, `/api/report?${params}`, { signal: request.signal })
  if (!response.ok) throw await responseError(response)
  return response.json() as Promise<ReportEnvelope>
}

export async function fetchReportMarkdown(baseUrl: string, request: Omit<ReportRequest, "signal">): Promise<string> {
  const params = new URLSearchParams({ period: request.period, offset: String(request.offset), format: "markdown" })
  const response = await apiFetch(baseUrl, `/api/report?${params}`)
  if (!response.ok) throw await responseError(response)
  return response.text()
}
