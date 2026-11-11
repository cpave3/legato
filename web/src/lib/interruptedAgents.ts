import { apiFetch } from "./api"

// An agent that was running when Legato last stopped and whose tmux session
// was gone when it started again: the machine or tmux went down.
export interface InterruptedAgent {
  task_id: string
  title: string
  agent_kind: string
  working_dir?: string
  engram?: string
  sandboxed: boolean
  ephemeral: boolean
  resources: string[]
  interrupted_at?: string
  // Its conversation can be resumed where it was.
  resumable: boolean
  // When it cannot be resumed, a new agent can start on its task instead.
  can_start_new: boolean
  // Why it cannot be resumed.
  reason?: string
}

// The interrupted agents; an older server has none.
export async function fetchInterruptedAgents(baseUrl: string): Promise<InterruptedAgent[]> {
  const response = await apiFetch(baseUrl, "/api/agents/interrupted")
  if (!response.ok) return []
  const body = await response.json()
  return Array.isArray(body?.agents) ? body.agents : []
}

// Resumes an interrupted agent, or with fresh starts a new agent on its task.
export async function resumeInterruptedAgent(baseUrl: string, taskId: string, fresh = false): Promise<void> {
  await post(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/resume`, fresh ? { fresh: true } : undefined)
}

export async function dismissInterruptedAgent(baseUrl: string, taskId: string): Promise<void> {
  await post(baseUrl, `/api/agents/${encodeURIComponent(taskId)}/dismiss`)
}

// Resumes every interrupted agent that can be; resolves to the ones that
// failed, with why.
export async function resumeAllInterruptedAgents(baseUrl: string): Promise<Array<{ task_id: string; error: string }>> {
  const answer = await post(baseUrl, "/api/agents/interrupted/resume-all") as { results?: Array<{ task_id: string; error?: string }> }
  return (answer.results ?? []).filter((r) => r.error).map((r) => ({ task_id: r.task_id, error: r.error ?? "" }))
}

async function post(baseUrl: string, path: string, body?: unknown): Promise<unknown> {
  const response = await apiFetch(baseUrl, path, {
    method: "POST",
    ...(body ? { headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) } : {}),
  })
  const text = await response.text()
  if (response.ok) {
    try {
      return JSON.parse(text)
    } catch {
      return {}
    }
  }
  let reason = text.trim()
  try {
    reason = (JSON.parse(text) as { error?: string }).error ?? reason
  } catch {
    // Not JSON: the text is the reason.
  }
  throw new Error(reason || "The request failed")
}
