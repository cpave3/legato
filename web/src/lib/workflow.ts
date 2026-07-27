import { apiFetch } from "./api"

export type WorkflowInput = { type: "string"|"integer"|"boolean"|"json"; required?: boolean; default?: unknown; description?: string; secret?: boolean; choices?: unknown[] }
export type WorkflowOutput = { type: string; path?: string; media_type?: string; description?: string }
export type WorkflowStepDefinition = { id:string; title:string; type:string; with:Record<string,unknown>; outputs?:Record<string,WorkflowOutput> }
export type WorkflowDefinition = { schema_version:number; name:string; title:string; description?:string; inputs?:Record<string,WorkflowInput>; steps:WorkflowStepDefinition[]; source:string }
export type WorkflowDiscovery = { definitions:WorkflowDefinition[]; errors:{path:string;error:string}[] }
export type WorkflowRun = { id:string; workflow_name:string; workflow_title:string; definition_source:string; definition_digest:string; inputs_json:string; status:string; current_step_id:string; task_id?:string; error?:string; created_at:string; started_at?:string; completed_at?:string }
export type WorkflowStepRun = { run_id:string; step_id:string; step_order:number; title:string; executor_type:string; resolved_inputs_json:string; outputs_json:string; status:string; latest_attempt:number; error?:string; started_at?:string; completed_at?:string }
export type WorkflowAttempt = { id:string; run_id:string; step_id:string; attempt_number:number; executor_type:string; agent_session_id?:number; task_id?:string; tmux_session?:string; agent_kind?:string; tier?:string; working_dir?:string; status:string; error?:string; started_at:string; ended_at?:string }
export type WorkflowArtifact = { id:string; run_id:string; step_id:string; attempt_id:string; output_key:string; media_type:string; size_bytes:number; digest:string; created_at:string }
export type WorkflowRunView = { run:WorkflowRun; steps:WorkflowStepRun[]; attempts:WorkflowAttempt[]; artifacts:WorkflowArtifact[] }

async function decode<T>(response:Response):Promise<T> { if (!response.ok) throw new Error(await response.text()); return response.json() }
export const fetchWorkflows=(baseUrl:string)=>apiFetch(baseUrl,"/api/workflows").then(decode<WorkflowDiscovery>)
export const fetchWorkflowRuns=(baseUrl:string)=>apiFetch(baseUrl,"/api/workflow-runs").then(decode<WorkflowRun[]>)
export const fetchWorkflowRun=(baseUrl:string,id:string)=>apiFetch(baseUrl,`/api/workflow-runs/${encodeURIComponent(id)}`).then(decode<WorkflowRunView>)
export const startWorkflow=(baseUrl:string,name:string,inputs:Record<string,string>)=>apiFetch(baseUrl,`/api/workflows/${encodeURIComponent(name)}/runs`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({inputs})}).then(decode<WorkflowRunView>)
export const cancelWorkflow=(baseUrl:string,id:string)=>apiFetch(baseUrl,`/api/workflow-runs/${encodeURIComponent(id)}/cancel`,{method:"POST"}).then(decode<WorkflowRunView>)
export const retryWorkflowStep=(baseUrl:string,id:string,step:string)=>apiFetch(baseUrl,`/api/workflow-runs/${encodeURIComponent(id)}/steps/${encodeURIComponent(step)}/retry`,{method:"POST"}).then(decode<WorkflowRunView>)
