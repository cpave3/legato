// An agent as GET /api/agents?view=compact gives it (AgentCompact in
// internal/server/agents_filter.go). Only the fields the glasses use.
export type Agent = {
  task_id: string
  title: string
  status: string
  activity: string
  sandboxed: boolean
  started_at: string
  card_task_id?: string
  slot?: string
  group?: string
  reeve?: boolean
}

// The end of a native agent's conversation, as GET
// /api/agents/{task_id}/conversation gives it (internal/server/agent_conversation.go).
export type Conversation = {
  task_id: string
  status: string
  entries: { kind: "user" | "assistant" | "tool" | string; text: string; tool?: string; command?: string; state?: string }[]
  permission: { tool: string; command?: string; reason?: string; plan?: string } | null
  question: { text: string; options: string[] } | null
  context_used: number
  context_window: number
}

// Where the app reads Legato. `url` is the Legato web server's address, or
// the dev server's /legato path, which adds the token itself.
export type Source = {
  url: string
  token?: string
}

export type Problem = { ok: false; problem: string }
export type AgentsResult = { ok: true; agents: Agent[] } | Problem
export type ConversationResult = { ok: true; conversation: Conversation } | Problem
export type TranscriptResult = { ok: true; text: string } | Problem
export type SendResult = { ok: true } | Problem

// How many conversation entries the agent page asks for.
export const CONVERSATION_ENTRIES = 20

type Answer = { response: Response } | Problem

async function call(
  source: Source,
  path: string,
  fetchFn: typeof fetch,
  body?: unknown,
): Promise<Answer> {
  const headers: Record<string, string> = { Accept: "application/json" }
  if (source.token) headers.Authorization = `Bearer ${source.token}`
  if (body !== undefined) headers["Content-Type"] = "application/json"
  try {
    const response = await fetchFn(`${source.url.replace(/\/+$/, "")}${path}`, {
      method: body === undefined ? "GET" : "POST",
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
    })
    if (response.status === 401) return { ok: false, problem: "Legato refused the token" }
    return { response }
  } catch {
    return { ok: false, problem: "Can't reach Legato" }
  }
}

const failed = (response: Response): Problem => ({ ok: false, problem: `Legato answered ${response.status}` })

const agentPath = (taskId: string) => `/api/agents/${encodeURIComponent(taskId)}`

// The agents running now.
export async function fetchAgents(source: Source, fetchFn: typeof fetch = fetch): Promise<AgentsResult> {
  const answer = await call(source, "/api/agents?view=compact&status=running", fetchFn)
  if (!("response" in answer)) return answer
  if (!answer.response.ok) return failed(answer.response)
  const body: unknown = await answer.response.json().catch(() => undefined)
  if (!Array.isArray(body)) return { ok: false, problem: "Legato's answer was not a list of agents" }
  return { ok: true, agents: body as Agent[] }
}

// The newest things an agent said and did.
export async function fetchConversation(
  source: Source,
  taskId: string,
  fetchFn: typeof fetch = fetch,
): Promise<ConversationResult> {
  const answer = await call(source, `${agentPath(taskId)}/conversation?limit=${CONVERSATION_ENTRIES}`, fetchFn)
  if (!("response" in answer)) return answer
  const { response } = answer
  if (response.status === 409)
    return { ok: false, problem: "This agent runs in a terminal, so Legato can't read its conversation." }
  if (response.status === 404) return { ok: false, problem: "The agent is no longer running." }
  if (!response.ok) return failed(response)
  const body = (await response.json().catch(() => undefined)) as Conversation | undefined
  if (!body || !Array.isArray(body.entries)) return { ok: false, problem: "Legato's answer was not a conversation" }
  return { ok: true, conversation: body }
}

function base64(bytes: Uint8Array): string {
  let binary = ""
  for (let i = 0; i < bytes.length; i += 0x8000) {
    binary += String.fromCharCode(...bytes.subarray(i, i + 0x8000))
  }
  return btoa(binary)
}

// The words in a recording: 16 kHz mono signed 16-bit little-endian PCM, as
// the glasses' microphone gives it. Transcription only: Legato does not
// deliver the text anywhere itself.
export async function transcribe(source: Source, pcm: Uint8Array, fetchFn: typeof fetch = fetch): Promise<TranscriptResult> {
  const answer = await call(source, "/api/voice/transcribe", fetchFn, { transcription_only: true, pcm: base64(pcm) })
  if (!("response" in answer)) return answer
  const { response } = answer
  if (response.status === 404) return { ok: false, problem: "Transcription is off in Legato." }
  if (!response.ok) return failed(response)
  const body = (await response.json().catch(() => ({}))) as { text?: string; error?: string }
  if (body.error) return { ok: false, problem: `Transcription failed: ${body.error}` }
  const text = body.text?.trim() ?? ""
  return text ? { ok: true, text } : { ok: false, problem: "Heard nothing." }
}

// Sends the operator's words to an agent's native session. It reaches only an
// agent that is running natively; it never starts one.
export async function sendMessage(
  source: Source,
  taskId: string,
  text: string,
  fetchFn: typeof fetch = fetch,
): Promise<SendResult> {
  const answer = await call(source, `${agentPath(taskId)}/chimera/send`, fetchFn, { content: text })
  if (!("response" in answer)) return answer
  const { response } = answer
  if (response.status === 409) return { ok: false, problem: "The agent is busy. Try again when it stops." }
  if (response.status === 503) return { ok: false, problem: "This agent can't take messages from the glasses." }
  // The dev server (dev/legato-dev.ts) refuses sends unless told to pass them on.
  if (response.status === 403) return { ok: false, problem: "Sending is off. Set LEGATO_ALLOW_SEND=1 for the dev server." }
  if (!response.ok) return failed(response)
  return { ok: true }
}
