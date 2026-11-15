import { apiFetch } from "./api"
import type {
  BoardResponse,
  BoardCard,
  CardDetail,
  Workspace,
  RemoteSearchResult,
  RepoDetectResponse,
  PRPreviewResponse,
  ArchiveDoneResponse,
  TaskWorktree,
} from "./board-types"

export type ProviderSyncSourceResult = {
  status: "completed" | "failed" | "unavailable"
  error?: string
  tasks_synced?: number
}

// Each provider's outcome by its name: jira and github always, burnlite
// when it is registered, and any other the server adds.
export type ProviderSyncResult = Record<string, ProviderSyncSourceResult>

export class ProviderSyncConflictError extends Error {}

export async function syncBoard(baseUrl: string): Promise<ProviderSyncResult> {
  const res = await apiFetch(baseUrl, "/api/board/sync", { method: "POST" })
  if (res.status === 409) throw new ProviderSyncConflictError("Provider sync is already running")
  if (!res.ok) {
    const message = await res.text().catch(() => "")
    throw new Error(message || `syncBoard failed: ${res.status}`)
  }
  return res.json() as Promise<ProviderSyncResult>
}

export async function fetchBoard(baseUrl: string, workspace?: string): Promise<BoardResponse> {
  const params = workspace ? `?workspace=${encodeURIComponent(workspace)}` : ""
  const res = await apiFetch(baseUrl, `/api/board${params}`)
  if (!res.ok) throw new Error(`fetchBoard failed: ${res.status}`)
  return res.json() as Promise<BoardResponse>
}

export async function fetchCard(baseUrl: string, id: string): Promise<CardDetail> {
  const res = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(id)}`)
  if (!res.ok) throw new Error(`fetchCard failed: ${res.status}`)
  return res.json() as Promise<CardDetail>
}

export async function fetchCardFull(baseUrl: string, id: string): Promise<string> {
  const res = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(id)}?format=full`)
  if (!res.ok) throw new Error(`fetchCardFull failed: ${res.status}`)
  return res.text()
}

export async function fetchWorkspaces(baseUrl: string): Promise<Workspace[]> {
  const res = await apiFetch(baseUrl, "/api/workspaces")
  if (!res.ok) throw new Error(`fetchWorkspaces failed: ${res.status}`)
  return res.json() as Promise<Workspace[]>
}

export async function createTask(
  baseUrl: string,
  body: {
    title: string
    description?: string
    column: string
    priority?: string
    workspace_id?: number | null
  }
): Promise<{ id: string }> {
  const res = await apiFetch(baseUrl, "/api/tasks", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`createTask failed: ${res.status}`)
  return res.json() as Promise<{ id: string }>
}

export type WorkflowTriggerRequest = { id:string; workflow_name:string; status:string; definition_json?:string; missing_inputs_json:string; error?:string }
export type PatchTaskResult = { status:string; triggers?:WorkflowTriggerRequest[] }

export async function patchTask(
  baseUrl: string,
  id: string,
  body: {
    title?: string
    description?: string
    workspace_id?: number | null
    column?: string
    // The card this one goes before in its column (after any move); null
    // for the end.
    before?: string | null
    metadata?: Record<string, unknown>
  }
): Promise<PatchTaskResult> {
  const res = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw await refusedWith(res, "patchTask")
  return res.json() as Promise<PatchTaskResult>
}

export async function createWorktree(baseUrl: string, taskId: string, body: { primary_dir: string; branch: string; base_branch: string }): Promise<TaskWorktree> {
  const response = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(taskId)}/worktree`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  if (!response.ok) throw new Error(await response.text())
  return response.json() as Promise<TaskWorktree>
}

// refusedWith is a failed request's error in the server's words when it
// gave some (a worker refusing to archive or delete a card its main handed
// it), else what failed and the status.
async function refusedWith(res: Response, what: string): Promise<Error> {
  const body = await res.json().catch(() => null)
  return new Error(typeof body?.error === "string" && body.error ? body.error : `${what} failed: ${res.status}`)
}

export async function deleteTask(baseUrl: string, id: string): Promise<void> {
  const res = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(id)}`, {
    method: "DELETE",
  })
  if (!res.ok) throw await refusedWith(res, "deleteTask")
}

export async function archiveTask(baseUrl: string, id: string): Promise<void> {
  const res = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(id)}/archive`, {
    method: "POST",
  })
  if (!res.ok) throw await refusedWith(res, "archiveTask")
}

export async function archiveDone(baseUrl: string): Promise<ArchiveDoneResponse> {
  const res = await apiFetch(baseUrl, "/api/board/archive-done", {
    method: "POST",
  })
  if (!res.ok) throw new Error(`archiveDone failed: ${res.status}`)
  return res.json() as Promise<ArchiveDoneResponse>
}

export async function fetchArchivedTasks(baseUrl: string): Promise<BoardCard[]> {
  const res = await apiFetch(baseUrl, "/api/tasks/archived")
  if (!res.ok) throw new Error(`fetchArchivedTasks failed: ${res.status}`)
  return res.json() as Promise<BoardCard[]>
}

export async function restoreTask(baseUrl: string, id: string): Promise<void> {
  const res = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(id)}/restore`, { method: "POST" })
  if (!res.ok) throw new Error(`restoreTask failed: ${res.status}`)
}

export async function searchTasks(baseUrl: string, query: string, signal?: AbortSignal): Promise<BoardCard[]> {
  const res = await apiFetch(baseUrl, `/api/tasks/search?q=${encodeURIComponent(query)}`, { signal })
  if (!res.ok) throw new Error(`searchTasks failed: ${res.status}`)
  return res.json() as Promise<BoardCard[]>
}

export async function prPreview(
  baseUrl: string,
  taskId: string,
  owner: string,
  repo: string,
  number: number
): Promise<PRPreviewResponse> {
  const res = await apiFetch(
    baseUrl,
    `/api/tasks/${encodeURIComponent(taskId)}/pr-preview?owner=${encodeURIComponent(owner)}&repo=${encodeURIComponent(repo)}&number=${number}`
  )
  if (!res.ok) throw new Error(`prPreview failed: ${res.status}`)
  return res.json() as Promise<PRPreviewResponse>
}

export async function linkPR(
  baseUrl: string,
  taskId: string,
  body: { owner: string; repo: string; pr_number: number }
): Promise<void> {
  const res = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(taskId)}/link-pr`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`linkPR failed: ${res.status}`)
}

export async function unlinkPR(baseUrl: string, taskId: string): Promise<void> {
  const res = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(taskId)}/unlink-pr`, {
    method: "POST",
  })
  if (!res.ok) throw new Error(`unlinkPR failed: ${res.status}`)
}

// The server's own words for a refusal ({error}), else the fallback.
async function serverError(res: Response, fallback: string): Promise<Error> {
  try {
    const body = await res.json()
    if (body && typeof body.error === "string" && body.error) return new Error(body.error)
  } catch {
    // Not JSON: the fallback says enough.
  }
  return new Error(fallback)
}

export async function remoteSearch(baseUrl: string, query: string): Promise<RemoteSearchResult[]> {
  const res = await apiFetch(baseUrl, `/api/remote/search?q=${encodeURIComponent(query)}`)
  if (!res.ok) throw await serverError(res, `remoteSearch failed: ${res.status}`)
  return res.json() as Promise<RemoteSearchResult[]>
}

// Imports a ticket; provider names the source a search result came from, so
// the server need not guess when several are connected.
// column is where the task starts; omitted, the provider's status mapping
// decides. Sync may move the task when the ticket changes remotely.
export async function remoteImport(
  baseUrl: string,
  body: { ticket_id: string; provider?: string; workspace_id?: number | null; column?: string }
): Promise<ImportedTask> {
  const res = await apiFetch(baseUrl, "/api/remote/import", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw await serverError(res, `remoteImport failed: ${res.status}`)
  return res.json() as Promise<ImportedTask>
}

/** Where an imported ticket's task is: its column and workspace. */
export interface ImportedTask {
  id: string
  status?: string
  workspace_id?: number | null
  workspace_name?: string
}

/** A connected ticket provider, from GET /api/ticket-providers. */
export interface TicketProvider {
  name: string
  label: string
}

// The connected ticket providers. Throws when the server cannot say.
export async function fetchTicketProviders(baseUrl: string): Promise<TicketProvider[]> {
  const res = await apiFetch(baseUrl, "/api/ticket-providers")
  if (!res.ok) throw new Error(`ticket providers: ${res.status}`)
  const body = (await res.json()) as { providers?: TicketProvider[] }
  return body.providers ?? []
}

export async function detectRepo(baseUrl: string): Promise<RepoDetectResponse> {
  const res = await apiFetch(baseUrl, "/api/repo/detect")
  if (!res.ok) throw new Error(`detectRepo failed: ${res.status}`)
  return res.json() as Promise<RepoDetectResponse>
}
