import { apiFetch } from "./api"

export interface PlanArtifact {
  plan: { id: string; name: string; title: string; status: string }
  revision: { revision: number }
  origins: Array<{ review_pass_id: string; finding_id: string }>
}

export interface ReviewPassArtifact {
  id: string
  number: number
  status: string
  summary: string
  captured_plan?: { plan_id: string; revision: number; title: string }
}

export interface ReviewArtifact {
  tour: { id: string; name: string; status: string }
  passes: Array<{ pass: ReviewPassArtifact; captured_plan?: { plan_id: string; revision: number; title: string } }>
}

export interface TaskArtifacts {
  task_id: string
  plans: PlanArtifact[]
  review_tours: ReviewArtifact[]
}

export async function fetchTaskArtifacts(baseUrl: string, taskId: string): Promise<TaskArtifacts> {
  const response = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(taskId)}/artifacts`)
  if (!response.ok) throw new Error(await response.text())
  return response.json() as Promise<TaskArtifacts>
}
