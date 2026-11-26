import { apiFetch } from "./api"
import type { WorkflowGraphModel } from "./workflowGraph"

export type WorkflowInput = { type: "string"|"integer"|"boolean"|"json"|"task"|"repository"; required?: boolean; default?: unknown; description?: string; secret?: boolean; choices?: unknown[] }
export type WorkflowOutput = { type: string; path?: string; media_type?: string; description?: string }
// WorkflowRunOutput is one of a workflow's own outputs (schema version 2,
// LEG-489): what its run gives back, from an expression.
export type WorkflowRunOutput = { type:string; value?:unknown; description?:string }
export type WorkflowStepStart = "automatic"|"manual"|"agent_ended"
// start is how a step starts; trigger is a schema version 1 file's key, which
// the server reads into start but a file being edited may still say.
export type WorkflowStepDefinition = { id:string; title:string; type:string; start?:WorkflowStepStart; trigger?:WorkflowStepStart; with:Record<string,unknown>; outputs?:Record<string,WorkflowOutput> }
export const stepStart=(step:Pick<WorkflowStepDefinition,"start"|"trigger">):WorkflowStepStart=>step.start??step.trigger??"automatic"
export type WorkflowDefinition = { schema_version:number; name:string; title:string; run_title?:string; description?:string; after_start?:"workflow"|"agent"|"stay"; inputs?:Record<string,WorkflowInput>; outputs?:Record<string,WorkflowRunOutput> | null; on?:Record<string,unknown>; steps:WorkflowStepDefinition[]; source:string; graph?:WorkflowGraphModel | null; folder?:string; editable?:boolean; read_only_reason?:string; shadows?:string[] | null; warnings?:{severity:string;message:string;path?:string;line:number;column:number}[] | null }
export type WorkflowRepositoryPreset = { name:string; repo:string; primary_dir:string; base_branch?:string }
// A trigger as this Legato runs it: on or paused here (enabled), what its
// file says (file_enabled), and whether this Legato switched it (overridden).
export type ScheduleFire = { due_time:string; status:string; run_id?:string; error?:string }
export type ScheduleState = { description:string; timezone:string; next:string[]; last?:ScheduleFire | null; missed?:ScheduleFire[] | null; catch_up?:string }
export type WorkflowTriggerState = { trigger:string; file_enabled:boolean; enabled:boolean; overridden:boolean; max_runs_per_day?:number; schedule?:ScheduleState | null }
export type WorkflowDiscovery = { definitions:WorkflowDefinition[] | null; errors:{path:string;error:string}[] | null; repositories?:WorkflowRepositoryPreset[] | null; trigger_states?:Record<string,WorkflowTriggerState[]> | null; folders?:WorkflowFolder[] | null }
export type WorkflowRunStepSummary = { step_id:string; title:string; status:string; trigger?:string }
export type WorkflowRun = { id:string; workflow_name:string; workflow_title:string; definition_source:string; definition_digest:string; definition_json?:string; inputs_json:string; status:string; current_step_id:string; task_id?:string; related_task_id?:string; related_task_title?:string; steps?:WorkflowRunStepSummary[]; error?:string; needs_attention?:boolean; attention_reason?:string; output_warning?:string; parent_run_id?:string; parent_step_path?:string; outputs_json?:string; created_at:string; started_at?:string; completed_at?:string }
// path keys a step instance in its run (each/2/build in a loop's pass, LEG-490); step_id is its ID alone.
export type WorkflowStepRun = { run_id:string; step_id:string; path?:string; parent_path?:string; pass?:number|null; step_order:number; title:string; executor_type:string; definition_json?:string; resolved_inputs_json:string; outputs_json:string; status:string; latest_attempt:number; error?:string; error_code?:string; error_link?:string; output_warning?:string; child_run_id?:string; started_at?:string; completed_at?:string }
/** GateDetails is what a gate step asks (a run view's details, by step). */
export type GateDetails = { title:string; body?:string; options:string[]; choice?:string }
/** WorkflowCallDetails is a workflow.run step's child run (LEG-489). */
export type WorkflowCallDetails = { run_id:string; workflow:string; title:string; status:string }
export type WorkflowAttempt = { id:string; run_id:string; step_id:string; path?:string; attempt_number:number; executor_type:string; agent_session_id?:number; task_id?:string; agent_id?:string; tmux_session?:string; agent_kind?:string; tier?:string; working_dir?:string; status:string; error?:string; agent_activity?:string; needs_attention?:boolean; attention_reason?:string; resumable?:boolean; started_at:string; ended_at?:string }
export type WorkflowArtifact = { id:string; run_id:string; step_id:string; path?:string; attempt_id:string; output_key:string; original_path?:string; media_type:string; size_bytes:number; digest:string; created_at:string }
// needs_you is the server's word on whether the loop waits on the operator:
// stuck, or waiting for an author that will not move it alone. An agent
// author at work is the agent's wait (LEG-543).
export type ReviewRunDetails = { phase:string; pass:number; summary:string; blocking_open:number; reason?:string; needs_you?:boolean; reviews:{pass:number;pr:number;url:string}[] | null }
// graph is the workflow graph of the run's own copy of its definition;
// definition_changed and definition_missing compare that copy with the file.
export type WorkflowRunView = { run:WorkflowRun; steps:WorkflowStepRun[] | null; attempts:WorkflowAttempt[] | null; artifacts:WorkflowArtifact[] | null; details?:Record<string,unknown> | null; trigger_kind?:string; resume_task_id?:string; resume_agent_state?:"" | "running" | "waiting"; can_resume_agent?:boolean; graph?:WorkflowGraphModel | null; definition_changed?:boolean; definition_missing?:boolean; output_warnings?:string[] | null }

async function decode<T>(response:Response):Promise<T> { if (!response.ok) throw new Error(await response.text()); return response.json() }
export const fetchWorkflows=(baseUrl:string)=>apiFetch(baseUrl,"/api/workflows").then(decode<WorkflowDiscovery>)
export const fetchWorkflowRuns=(baseUrl:string)=>apiFetch(baseUrl,"/api/workflow-runs").then(decode<WorkflowRun[]>)
// fetchWorkflowRunsOf is one workflow's newest runs, filtered and limited by
// the server: filtering the whole list here would miss them once other
// workflows had made the server's default limit of newer runs.
export const fetchWorkflowRunsOf=(baseUrl:string,name:string,limit:number)=>apiFetch(baseUrl,`/api/workflow-runs?${new URLSearchParams({workflow_name:name,limit:String(limit)})}`).then(decode<WorkflowRun[]|null>).then(runs=>runs??[])
export const fetchWorkflowRun=(baseUrl:string,id:string)=>apiFetch(baseUrl,`/api/workflow-runs/${encodeURIComponent(id)}`).then(decode<WorkflowRunView>)
export const startWorkflow=(baseUrl:string,name:string,inputs:Record<string,string>)=>apiFetch(baseUrl,`/api/workflows/${encodeURIComponent(name)}/runs`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({inputs})}).then(decode<WorkflowRunView>)
// setWorkflowTrigger switches a workflow's trigger on or off in this Legato
// only; its file is never changed.
export const setWorkflowTrigger=(baseUrl:string,name:string,trigger:string,enabled:boolean)=>apiFetch(baseUrl,`/api/workflows/${encodeURIComponent(name)}/triggers/${encodeURIComponent(trigger)}`,{method:"PUT",headers:{"Content-Type":"application/json"},body:JSON.stringify({enabled})}).then(decode<{status:string}>)
export type WorkflowTemplate = { name:string; title:string; description?:string; yaml:string }
export type WorkflowFolder = { id:string; label:string; path:string }
export const fetchWorkflowTemplates=(baseUrl:string)=>apiFetch(baseUrl,"/api/workflow-templates").then(decode<{templates:WorkflowTemplate[];folders:WorkflowFolder[]}>)
export const cancelWorkflow=(baseUrl:string,id:string)=>apiFetch(baseUrl,`/api/workflow-runs/${encodeURIComponent(id)}/cancel`,{method:"POST"}).then(decode<WorkflowRunView>)
const forceBody=(force:boolean)=>({method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({force})})
export const triggerWorkflowStep=(baseUrl:string,id:string,step:string,force=false)=>apiFetch(baseUrl,`/api/workflow-runs/${encodeURIComponent(id)}/steps/${encodeURIComponent(step)}/trigger`,forceBody(force)).then(decode<WorkflowRunView>)
export const retryWorkflowStep=(baseUrl:string,id:string,step:string,force=false)=>apiFetch(baseUrl,`/api/workflow-runs/${encodeURIComponent(id)}/steps/${encodeURIComponent(step)}/retry`,forceBody(force)).then(decode<WorkflowRunView>)
// Resumes the interrupted agent of a suspended step, which goes on with the same attempt.
export const resumeWorkflowStep=(baseUrl:string,id:string,step:string)=>apiFetch(baseUrl,`/api/workflow-runs/${encodeURIComponent(id)}/steps/${encodeURIComponent(step)}/resume`,{method:"POST"}).then(decode<WorkflowRunView>)
export const promptWorkflowStep=(baseUrl:string,id:string,step:string)=>apiFetch(baseUrl,`/api/workflow-runs/${encodeURIComponent(id)}/steps/${encodeURIComponent(step)}/prompt`,{method:"POST"}).then(decode<WorkflowRunView>)
export async function fetchWorkflowArtifact(baseUrl:string,id:string):Promise<Blob>{const response=await apiFetch(baseUrl,`/api/workflow-artifacts/${encodeURIComponent(id)}`);if(!response.ok)throw new Error(await response.text());return response.blob()}
// MergeWaitDetails is a waiting github.pr.wait step, as the run's details
// give it (LEG-614).
export type MergeWaitDetails = { repo:string; pr_number:number; url?:string; state?:string; head_branch?:string; base_branch?:string; conflicts?:boolean; check_status?:string; task_id?:string; task_title?:string; agent?:"" | "running" | "waiting"; notify:boolean }
// resumeWorkflowAgent starts the run's card agent with a macro, or sends the
// macro to the one running.
export const resumeWorkflowAgent=(baseUrl:string,id:string,macro:string)=>apiFetch(baseUrl,`/api/workflow-runs/${encodeURIComponent(id)}/resume-agent`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({macro})}).then(decode<{task_id:string;started:boolean;queued:boolean;agent_kind?:string}>)
// workflowStepAction takes a step's action: a gate's choice (choose:<option>),
// or retry, skip or stop on a step paused after a failure.
export const workflowStepAction=(baseUrl:string,id:string,step:string,action:string,text?:string)=>apiFetch(baseUrl,`/api/workflow-runs/${encodeURIComponent(id)}/steps/${encodeURIComponent(step)}/action`,{method:"POST",headers:{"Content-Type":"application/json"},body:JSON.stringify({action,text})}).then(decode<WorkflowRunView>)
export const anotherReviewPass=(baseUrl:string,id:string,step:string)=>apiFetch(baseUrl,`/api/workflow-runs/${encodeURIComponent(id)}/steps/${encodeURIComponent(step)}/another-pass`,{method:"POST"}).then(decode<WorkflowRunView>)
// The review loop already running on a pull request, as a run of
// review-pr-passes, or null.
export async function activeReviewLoop(baseUrl:string,repo:string,pr:number):Promise<WorkflowRun|null>{const query=new URLSearchParams({workflow_name:"review-pr-passes","input.repo":repo,"input.pr_number":String(pr)});const runs=await apiFetch(baseUrl,`/api/workflow-runs?${query}`).then(decode<WorkflowRun[]|null>);return (runs??[]).find(run=>run.status==="running"||run.status==="waiting")??null}
