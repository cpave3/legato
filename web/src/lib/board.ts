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
} from "./board-types"

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

export async function patchTask(
  baseUrl: string,
  id: string,
  body: {
    title?: string
    description?: string
    workspace_id?: number | null
    column?: string
  }
): Promise<void> {
  const res = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(id)}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`patchTask failed: ${res.status}`)
}

export async function deleteTask(baseUrl: string, id: string): Promise<void> {
  const res = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(id)}`, {
    method: "DELETE",
  })
  if (!res.ok) throw new Error(`deleteTask failed: ${res.status}`)
}

export async function archiveTask(baseUrl: string, id: string): Promise<void> {
  const res = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(id)}/archive`, {
    method: "POST",
  })
  if (!res.ok) throw new Error(`archiveTask failed: ${res.status}`)
}

export async function archiveDone(baseUrl: string): Promise<ArchiveDoneResponse> {
  const res = await apiFetch(baseUrl, "/api/board/archive-done", {
    method: "POST",
  })
  if (!res.ok) throw new Error(`archiveDone failed: ${res.status}`)
  return res.json() as Promise<ArchiveDoneResponse>
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

export async function remoteSearch(baseUrl: string, query: string): Promise<RemoteSearchResult[]> {
  const res = await apiFetch(baseUrl, `/api/remote/search?q=${encodeURIComponent(query)}`)
  if (!res.ok) throw new Error(`remoteSearch failed: ${res.status}`)
  return res.json() as Promise<RemoteSearchResult[]>
}

export async function remoteImport(
  baseUrl: string,
  body: { ticket_id: string; workspace_id?: number }
): Promise<{ id: string }> {
  const res = await apiFetch(baseUrl, "/api/remote/import", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  })
  if (!res.ok) throw new Error(`remoteImport failed: ${res.status}`)
  return res.json() as Promise<{ id: string }>
}

export async function detectRepo(baseUrl: string): Promise<RepoDetectResponse> {
  const res = await apiFetch(baseUrl, "/api/repo/detect")
  if (!res.ok) throw new Error(`detectRepo failed: ${res.status}`)
  return res.json() as Promise<RepoDetectResponse>
}
