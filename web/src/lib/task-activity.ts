import { apiFetch } from "./api"
import { legatoHref, type LegatoNavigationKind } from "./navigation"

export type TaskReview={id:string;task_id:string;name:string;status:string;updated_at:string}
export type TaskPlan={id:string;task_id:string;name:string;title:string;summary:string;status:string;updated_at:string}
export type TaskWorkflowArtifact={id:string;output_key:string;original_path?:string;media_type:string;size_bytes:number;created_at:string}
export type TaskWorkflowRun={run:{id:string;workflow_title:string;workflow_name:string;status:string;created_at:string;completed_at?:string};artifacts:TaskWorkflowArtifact[]}
export type TaskActivity={reviews:TaskReview[]|null;plans:TaskPlan[]|null;workflow_runs:TaskWorkflowRun[]|null}

export async function fetchTaskActivity(baseUrl:string,taskId:string):Promise<TaskActivity>{
  const response=await apiFetch(baseUrl,`/api/tasks/${encodeURIComponent(taskId)}/activity`)
  if(!response.ok)throw new Error(await response.text())
  return response.json()
}

/** A normalized, domain-tagged item the related-work strip can render. */
export interface RelatedWorkItem {
  kind: LegatoNavigationKind
  id: string
  title: string
  status: string
  href: string
}

const emptyActivity = (): TaskActivity => ({ reviews: [], plans: [], workflow_runs: [] })

/** Projects the raw task-activity response into a flat list of linkable items. */
export function toRelatedWork(activity: TaskActivity): RelatedWorkItem[] {
  const items: RelatedWorkItem[] = []
  for (const plan of activity.plans ?? []) {
    const href = legatoHref({ kind: "plan", target: plan.id })
    if (href) items.push({ kind: "plan", id: plan.id, title: plan.title || plan.name, status: plan.status, href })
  }
  for (const review of activity.reviews ?? []) {
    const href = legatoHref({ kind: "review", target: review.id })
    if (href) items.push({ kind: "review", id: review.id, title: review.name || "Default review", status: review.status, href })
  }
  for (const run of activity.workflow_runs ?? []) {
    const href = legatoHref({ kind: "workflow_run", target: run.run.id })
    if (href) items.push({ kind: "workflow_run", id: run.run.id, title: run.run.workflow_title, status: run.run.status, href })
  }
  return items
}

export { emptyActivity }
