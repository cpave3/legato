import { apiFetch } from "./api"

export type PlanStatus = "proposed" | "changes_requested" | "approved" | "rejected"

export interface PlanQueueItem { plan_id:string; task_id:string; name:string; title:string; summary:string; status:PlanStatus; revision:number; unanswered_required:number; updated_at:string }
export interface Plan { id:string; task_id:string; name:string; title:string; summary:string; status:PlanStatus; latest_revision:number; approved_at?:string; rejected_at?:string; created_at:string; updated_at:string }
export interface PlanRevision { id:string; plan_id:string; revision:number; markdown:string; manifest_json:string; created_at:string }
export interface PlanQuestion { id:string; key:string; kind:"single_choice"|"multiple_choice"|"free_text"; prompt:string; rationale:string; required:boolean; options_json:string; recommended_json:string }
export interface PlanResponse { id:string; question_id:string; values_json:string; text:string }
export interface PlanComment { id:string; revision_id:string; body:string; selection_start?:number; selection_end?:number; selected_text:string; prefix:string; suffix:string; submitted_at?:string; created_at:string }
export interface PlanMessage { id:number; thread_id:string; kind:"question"|"answer"; author:"user"|"agent"; body:string; delivered_at?:string; created_at:string }
export interface PlanView { plan:Plan; revision:PlanRevision; questions:PlanQuestion[]; responses:PlanResponse[]; comments:PlanComment[]; messages:PlanMessage[] }
export interface PlanOption { id:string; label:string; description?:string }

async function json<T>(response:Response):Promise<T> { if (!response.ok) throw new Error(await response.text() || response.statusText); return response.json() as Promise<T> }
async function ok(response:Response):Promise<void> { if (!response.ok) throw new Error(await response.text() || response.statusText) }
const path = (id:string) => `/api/plans/${encodeURIComponent(id)}`
const send = (baseUrl:string, target:string, body?:unknown, method="POST") => apiFetch(baseUrl, target, { method, ...(body === undefined ? {} : { headers:{"Content-Type":"application/json"}, body:JSON.stringify(body) }) })

export const fetchPlanQueue = async (baseUrl:string) => json<PlanQueueItem[]>(await apiFetch(baseUrl, "/api/plans/queue"))
export const fetchPlan = async (baseUrl:string, id:string) => json<PlanView>(await apiFetch(baseUrl, path(id)))
export const respondToPlanQuestion = async (baseUrl:string,id:string,key:string,body:{values?:string[];text?:string}) => ok(await send(baseUrl,`${path(id)}/responses/${encodeURIComponent(key)}`,body,"PUT"))
export const addPlanComment = async (baseUrl:string,id:string,body:{body:string;selection_start?:number;selection_end?:number;selected_text?:string;prefix?:string;suffix?:string}) => json<PlanComment>(await send(baseUrl,`${path(id)}/comments`,body))
export const askPlanQuestion = async (baseUrl:string,id:string,text:string) => json<{warning?:string;thread_id:string}>(await send(baseUrl,`${path(id)}/questions`,{text}))
export const planAction = async (baseUrl:string,id:string,action:"request-changes"|"approve"|"reject"|"reopen") => ok(await send(baseUrl,`${path(id)}/${action}`))
