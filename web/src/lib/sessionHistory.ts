import { apiFetch } from "./api"

// One conversation of the session history: a Claude Code session, a Codex
// thread or a Chimera session an agent had on this Legato, with how it
// stands and whether it can be restored (GET /api/sessions).
export interface SessionEntry {
  id: number
  agent_kind: string
  provider_session_id: string
  task_id: string
  task_title: string
  task_exists: boolean
  task_archived: boolean
  slot: string
  ephemeral: boolean
  reeve: boolean
  cwd: string
  repo?: string
  branch?: string
  // running, interrupted, ended (with end_reason) or away.
  state: string
  end_reason?: string
  first_message?: string
  last_message?: string
  last_message_at?: string
  first_seen_at: string
  last_seen_at: string
  run_count: number
  // recorded, or recovered: found by folder and time.
  source: string
  transcript_state: string
  restorable: boolean
  new_task_only?: boolean
  reason?: string
  can_start_fresh: boolean
}

export interface SessionRun {
  execution_id: string
  started_at: string
  ended_at?: string
  end_reason?: string
  sandboxed?: boolean
  engram?: string
  resources: string[]
}

export interface SessionDetail extends SessionEntry {
  runs: SessionRun[]
  transcript_path?: string
  // The newest messages of its transcript, when that is there.
  preview?: Array<{ role: string; text: string; at: string }>
}

export interface SessionPage {
  sessions: SessionEntry[]
  next_cursor?: string
}

export interface SessionFilters {
  q?: string
  agent_kind?: string
  state?: string
  task?: string
  repo?: string
  cursor?: string
  limit?: number
}

// A restore the server refused, and why; nothing was started.
export class RestoreRefused extends Error {
  readonly newTaskPossible: boolean
  readonly canStartFresh: boolean
  constructor(reason: string, newTaskPossible = false, canStartFresh = false) {
    super(reason)
    this.name = "RestoreRefused"
    this.newTaskPossible = newTaskPossible
    this.canStartFresh = canStartFresh
  }
}

export async function fetchSessions(baseUrl: string, filters: SessionFilters): Promise<SessionPage> {
  const params = new URLSearchParams()
  for (const [key, value] of Object.entries(filters)) {
    if (value !== undefined && value !== "") params.set(key, String(value))
  }
  const query = params.toString()
  const response = await apiFetch(baseUrl, "/api/sessions" + (query ? `?${query}` : ""))
  if (!response.ok) throw new Error(await reasonOf(response))
  const body = await response.json() as SessionPage
  return { sessions: Array.isArray(body?.sessions) ? body.sessions : [], next_cursor: body?.next_cursor }
}

export async function fetchSession(baseUrl: string, id: number): Promise<SessionDetail> {
  const response = await apiFetch(baseUrl, `/api/sessions/${id}`)
  if (!response.ok) throw new Error(await reasonOf(response))
  return response.json()
}

// Starts a conversation again: in its own task, or a new one. Resolves to
// the task it runs in.
export async function restoreSession(baseUrl: string, id: number, target: "task" | "new_task" = "task"): Promise<string> {
  const response = await apiFetch(baseUrl, `/api/sessions/${id}/restore`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ target }),
  })
  if (response.ok) {
    const body = await response.json().catch(() => ({})) as { task_id?: string }
    return body.task_id ?? ""
  }
  if (response.status === 409) {
    const body = await response.json().catch(() => ({})) as { error?: string; new_task_possible?: boolean; can_start_fresh?: boolean }
    throw new RestoreRefused(body.error || "It cannot be restored", Boolean(body.new_task_possible), Boolean(body.can_start_fresh))
  }
  throw new Error(await reasonOf(response))
}

// Forgets a conversation: its entry and runs. The tool's transcript and
// the task's own Resume are left as they are.
export async function forgetSession(baseUrl: string, id: number): Promise<void> {
  const response = await apiFetch(baseUrl, `/api/sessions/${id}`, { method: "DELETE" })
  if (!response.ok) throw new Error(await reasonOf(response))
}

async function reasonOf(response: Response): Promise<string> {
  const text = await response.text()
  try {
    return (JSON.parse(text) as { error?: string }).error || text || "The request failed"
  } catch {
    return text.trim() || "The request failed"
  }
}

// How an entry stands, in words: "Running", "Interrupted", "Ended · stopped".
export function sessionStateLabel(entry: Pick<SessionEntry, "state" | "end_reason">): string {
  switch (entry.state) {
    case "running": return "Running"
    case "interrupted": return "Interrupted"
    case "away": return "Away on a worker"
    default: return entry.end_reason ? `Ended · ${entry.end_reason}` : "Ended"
  }
}
