import { apiFetch } from "./api"

export interface SwarmStartBody {
  parent_task_id: string
  working_dir: string
}

export interface SwarmWorkerActionBody {
  subtask_id: string
}

export interface SwarmMessageBody {
  task_id: string
  text: string
}

export interface SwarmFinishBody {
  parent_task_id: string
  summary: string
}

export interface InboxEntry {
  id: number
  subtask_id?: string
  kind: string
  worker?: string
  payload: string
  created_at: string
}

export interface PendingPlanData {
  parent_task_id: string
  plan_path: string
  reply_socket: string
}

export interface SwarmStatusData {
  parent: {
    id: string
    title: string
    status: string
    working_dir?: string
  }
  subtasks: {
    id: string
    title: string
    description?: string
    role: string
    agent?: string
    scope_globs: string[]
    status: string
    worker_agent_id?: number
    started_at?: string
    completed_at?: string
  }[]
}

async function parseError(res: Response): Promise<string> {
  try {
    const body = await res.json()
    if (body.error) return body.error
  } catch { /* fall through */ }
  return `${res.status} ${res.statusText}`
}

export async function startSwarm(baseUrl: string, parentTaskID: string, workingDir: string): Promise<void> {
  const res = await apiFetch(baseUrl, "/api/swarm/start", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ parent_task_id: parentTaskID, working_dir: workingDir } satisfies SwarmStartBody),
  })
  if (!res.ok) throw new Error(await parseError(res))
}

export async function dispatchWorker(baseUrl: string, subtaskID: string): Promise<void> {
  const res = await apiFetch(baseUrl, "/api/swarm/dispatch", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ subtask_id: subtaskID } satisfies SwarmWorkerActionBody),
  })
  if (!res.ok) throw new Error(await parseError(res))
}

export async function messageWorker(baseUrl: string, taskID: string, text: string): Promise<void> {
  const res = await apiFetch(baseUrl, "/api/swarm/message", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ task_id: taskID, text } satisfies SwarmMessageBody),
  })
  if (!res.ok) throw new Error(await parseError(res))
}

export async function closeWorker(baseUrl: string, subtaskID: string): Promise<void> {
  const res = await apiFetch(baseUrl, "/api/swarm/close", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ subtask_id: subtaskID } satisfies SwarmWorkerActionBody),
  })
  if (!res.ok) throw new Error(await parseError(res))
}

export async function finishSwarm(baseUrl: string, parentTaskID: string, summary: string): Promise<void> {
  const res = await apiFetch(baseUrl, "/api/swarm/finish", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ parent_task_id: parentTaskID, summary } satisfies SwarmFinishBody),
  })
  if (!res.ok) throw new Error(await parseError(res))
}

export async function getSwarmStatus(baseUrl: string, parentID: string): Promise<SwarmStatusData> {
  const res = await apiFetch(baseUrl, `/api/swarm/status/${encodeURIComponent(parentID)}`)
  if (!res.ok) throw new Error(await parseError(res))
  return res.json()
}

export async function drainInbox(baseUrl: string, parentID: string): Promise<InboxEntry[]> {
  const res = await apiFetch(baseUrl, `/api/swarm/inbox/${encodeURIComponent(parentID)}`)
  if (!res.ok) throw new Error(await parseError(res))
  const data = await res.json()
  return data.entries ?? []
}

export async function peekInbox(baseUrl: string, parentID: string): Promise<InboxEntry[]> {
  const res = await apiFetch(baseUrl, `/api/swarm/inbox/${encodeURIComponent(parentID)}?peek=true`)
  if (!res.ok) throw new Error(await parseError(res))
  const data = await res.json()
  return data.entries ?? []
}

export async function getPendingPlan(baseUrl: string, parentID: string): Promise<PendingPlanData | null> {
  const res = await apiFetch(baseUrl, `/api/swarm/pending-plan/${encodeURIComponent(parentID)}`)
  if (res.status === 404) return null
  if (!res.ok) throw new Error(await parseError(res))
  return res.json()
}
