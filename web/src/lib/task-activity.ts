import { apiFetch } from "./api"

export type TaskReview={id:string;task_id:string;name:string;status:string;updated_at:string}
export type TaskPlan={id:string;task_id:string;name:string;title:string;summary:string;status:string;updated_at:string}
export type TaskWorkflowArtifact={id:string;output_key:string;media_type:string;size_bytes:number;created_at:string}
export type TaskWorkflowRun={run:{id:string;workflow_title:string;workflow_name:string;status:string;created_at:string;completed_at?:string};artifacts:TaskWorkflowArtifact[]}
export type TaskActivity={reviews:TaskReview[]|null;plans:TaskPlan[]|null;workflow_runs:TaskWorkflowRun[]|null}
export async function fetchTaskActivity(baseUrl:string,taskId:string):Promise<TaskActivity>{const response=await apiFetch(baseUrl,`/api/tasks/${encodeURIComponent(taskId)}/activity`);if(!response.ok)throw new Error(await response.text());return response.json()}
