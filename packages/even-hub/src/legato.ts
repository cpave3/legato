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

// An approval request, as GET /api/approvals gives it (service.Approval in
// internal/service/approval.go). Only the fields the glasses use.
export type Approval = {
  id: string
  source: string
  task_id?: string
  title: string
  summary?: string
  options: { id: string; label: string; kind: "allow" | "deny" | string; description?: string }[]
  // Only a decision signed on an enrolled device may allow it.
  signed?: boolean
  details?: Record<string, unknown>
}

// Where the app reads Legato: the Legato Android app's glasses link on this
// phone, which holds each paired Legato's address and token and passes the
// app's calls on to the one it names. `code` is the pairing code the
// operator copied from the Legato app ("" when none is saved); the dev
// server's fake phone asks for none (`false`).
export type Phone = {
  url: string
  code: string | false
}

// Where the glasses link listens. Fixed, because the packed app's network
// whitelist is fixed when it is made (app.json).
export const PHONE_URL = "http://127.0.0.1:8792"

// A Legato saved in the Legato app, as GET /v1/instances gives it.
export type Instance = { id: string; name: string }

// `gone` marks the Legato app's answer that it no longer has the instance.
export type Problem = { ok: false; problem: string; gone?: true }
export type InstancesResult = { ok: true; instances: Instance[] } | Problem
export type AgentsResult = { ok: true; agents: Agent[] } | Problem
export type ConversationResult = { ok: true; conversation: Conversation } | Problem
export type TranscriptResult = { ok: true; text: string } | Problem
export type SendResult = { ok: true } | Problem
export type ApprovalsResult = { ok: true; approvals: Approval[] } | Problem

// How many conversation entries the agent page asks for, and the Reeve's
// chat, which looks back past a turn's tool calls to the message that
// started it (the Legato app allows up to 50).
export const CONVERSATION_ENTRIES = 20
export const REEVE_ENTRIES = 50

// How long a call may take before the app gives up on it: a read or a send,
// and a transcription, which carries up to a minute of sound. Without a limit
// one call that never answers would hold every press behind it. The Legato
// app gives Legato 10 s (30 s for a transcription) and then answers 502
// itself, so the app waits a little longer than that to hear why.
export const CALL_TIMEOUT_MS = 15_000
export const TRANSCRIBE_TIMEOUT_MS = 35_000

const NOT_PAIRED: Problem = { ok: false, problem: "Not paired with the Legato app" }

type Answer = { response: Response } | Problem

async function call(phone: Phone, path: string, fetchFn: typeof fetch, timeoutMs: number, body?: unknown): Promise<Answer> {
  if (phone.code === "") return NOT_PAIRED
  const headers: Record<string, string> = { Accept: "application/json" }
  if (phone.code) headers.Authorization = `Bearer ${phone.code}`
  if (body !== undefined) headers["Content-Type"] = "application/json"
  let response: Response
  try {
    response = await fetchFn(`${phone.url.replace(/\/+$/, "")}${path}`, {
      method: body === undefined ? "GET" : "POST",
      headers,
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(timeoutMs),
    })
  } catch (err) {
    if (err instanceof DOMException && err.name === "TimeoutError") return { ok: false, problem: "The Legato app took too long to answer" }
    // The Legato app isn't running, or its glasses link is off.
    return { ok: false, problem: "Can't reach the Legato app on this phone" }
  }
  // The Legato app's own refusals. Legato's own 401 reaches the glasses as a
  // 502, so a 401 is always the pairing code.
  if (response.status === 401) return { ok: false, problem: "The Legato app refused the pairing code" }
  if (response.status === 404) {
    const said = await errorOf(response.clone())
    if (said === "unknown instance") return { ok: false, problem: "That Legato is no longer in the app", gone: true }
    // An older Legato app, which does not pass this call on yet.
    if (said === "not served to the glasses") return { ok: false, problem: "Update the Legato app on this phone: it can't pass this on yet." }
  }
  if (response.status === 502) return { ok: false, problem: (await errorOf(response)) || "Can't reach Legato" }
  return { response }
}

// The {"error": "…"} of a refusal, or "".
async function errorOf(response: Response): Promise<string> {
  const body = (await response.json().catch(() => undefined)) as { error?: unknown } | undefined
  return typeof body?.error === "string" ? body.error.trim() : ""
}

// A failed answer in Legato's words when it gave them as {"error": "…"}, as
// most refusals are (browser-extension/src/legato.ts does the same).
async function failed(response: Response): Promise<Problem> {
  const said = await errorOf(response)
  return { ok: false, problem: said ? `Legato: ${said}` : `Legato answered ${response.status}` }
}

const instancePath = (instance: string) => `/v1/instances/${encodeURIComponent(instance)}`
const agentPath = (instance: string, taskId: string) => `${instancePath(instance)}/api/agents/${encodeURIComponent(taskId)}`

// The Legato instances saved in the Legato app, in its order.
export async function fetchInstances(phone: Phone, fetchFn: typeof fetch = fetch): Promise<InstancesResult> {
  const answer = await call(phone, "/v1/instances", fetchFn, CALL_TIMEOUT_MS)
  if (!("response" in answer)) return answer
  if (!answer.response.ok) return { ok: false, problem: `The Legato app answered ${answer.response.status}` }
  const body: unknown = await answer.response.json().catch(() => undefined)
  const valid =
    Array.isArray(body) &&
    body.every((i) => typeof (i as Instance)?.id === "string" && typeof (i as Instance)?.name === "string")
  if (!valid) return { ok: false, problem: "The Legato app's answer was not a list of instances" }
  return { ok: true, instances: (body as Instance[]).map(({ id, name }) => ({ id, name })) }
}

// The agents running now.
export async function fetchAgents(
  phone: Phone,
  instance: string,
  fetchFn: typeof fetch = fetch,
  timeoutMs = CALL_TIMEOUT_MS,
): Promise<AgentsResult> {
  const answer = await call(phone, `${instancePath(instance)}/api/agents?view=compact&status=running`, fetchFn, timeoutMs)
  if (!("response" in answer)) return answer
  if (!answer.response.ok) return await failed(answer.response)
  const body: unknown = await answer.response.json().catch(() => undefined)
  if (!Array.isArray(body)) return { ok: false, problem: "Legato's answer was not a list of agents" }
  return { ok: true, agents: body as Agent[] }
}

// The newest things an agent said and did.
export async function fetchConversation(
  phone: Phone,
  instance: string,
  taskId: string,
  fetchFn: typeof fetch = fetch,
  limit = CONVERSATION_ENTRIES,
): Promise<ConversationResult> {
  const answer = await call(phone, `${agentPath(instance, taskId)}/conversation?limit=${limit}`, fetchFn, CALL_TIMEOUT_MS)
  if (!("response" in answer)) return answer
  const { response } = answer
  if (response.status === 409)
    return { ok: false, problem: "This agent runs in a terminal, so Legato can't read its conversation." }
  if (response.status === 404) return { ok: false, problem: "The agent is no longer running." }
  if (!response.ok) return await failed(response)
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
// deliver the text anywhere itself (and the Legato app rebuilds the body, so
// nothing else reaches Legato).
export async function transcribe(
  phone: Phone,
  instance: string,
  pcm: Uint8Array,
  fetchFn: typeof fetch = fetch,
): Promise<TranscriptResult> {
  const answer = await call(phone, `${instancePath(instance)}/api/voice/transcribe`, fetchFn, TRANSCRIBE_TIMEOUT_MS, {
    transcription_only: true,
    pcm: base64(pcm),
  })
  if (!("response" in answer)) return answer
  const { response } = answer
  if (response.status === 404) return { ok: false, problem: "Transcription is off in Legato." }
  if (!response.ok) return await failed(response)
  const body = (await response.json().catch(() => ({}))) as { text?: string; error?: string }
  if (body.error) return { ok: false, problem: `Transcription failed: ${body.error}` }
  const text = body.text?.trim() ?? ""
  return text ? { ok: true, text } : { ok: false, problem: "Heard nothing." }
}

// Sends the operator's words to an agent's native session. It reaches only an
// agent that is running natively; it never starts one.
export async function sendMessage(
  phone: Phone,
  instance: string,
  taskId: string,
  text: string,
  fetchFn: typeof fetch = fetch,
): Promise<SendResult> {
  const answer = await call(phone, `${agentPath(instance, taskId)}/chimera/send`, fetchFn, CALL_TIMEOUT_MS, { content: text })
  if (!("response" in answer)) return answer
  const { response } = answer
  if (response.status === 409) return { ok: false, problem: "The agent is busy. Try again when it stops." }
  if (response.status === 503) return { ok: false, problem: "This agent can't take messages from the glasses." }
  if (!response.ok) return await failed(response)
  return { ok: true }
}

// Sends the operator's words to the instance's Reeve, as dictated on the
// glasses, so it keeps its reply to what they show. Legato starts or resumes
// the Reeve for it when none is running.
export async function messageReeve(phone: Phone, instance: string, text: string, fetchFn: typeof fetch = fetch): Promise<SendResult> {
  const answer = await call(phone, `${instancePath(instance)}/api/reeve/message`, fetchFn, CALL_TIMEOUT_MS, { text, from: "glasses" })
  if (!("response" in answer)) return answer
  const { response } = answer
  if (response.status === 503) return { ok: false, problem: "This Legato has no Reeve." }
  if (!response.ok) return await failed(response)
  return { ok: true }
}

// The approval requests waiting for the operator.
export async function fetchApprovals(phone: Phone, instance: string, fetchFn: typeof fetch = fetch): Promise<ApprovalsResult> {
  const answer = await call(phone, `${instancePath(instance)}/api/approvals?status=pending`, fetchFn, CALL_TIMEOUT_MS)
  if (!("response" in answer)) return answer
  if (!answer.response.ok) return await failed(answer.response)
  const body = (await answer.response.json().catch(() => undefined)) as { approvals?: unknown } | undefined
  if (!Array.isArray(body?.approvals)) return { ok: false, problem: "Legato's answer was not a list of approvals" }
  return { ok: true, approvals: body.approvals as Approval[] }
}

// Allows an approval (with one of its options) or denies it. Legato refuses
// to allow one that needs a signature; the glasses never offer to.
export async function decideApproval(
  phone: Phone,
  instance: string,
  id: string,
  decision: "allow" | "deny",
  option: string | undefined,
  fetchFn: typeof fetch = fetch,
): Promise<SendResult> {
  const body = option ? { decision, option } : { decision }
  const answer = await call(phone, `${instancePath(instance)}/api/approvals/${encodeURIComponent(id)}/decision`, fetchFn, CALL_TIMEOUT_MS, body)
  if (!("response" in answer)) return answer
  const { response } = answer
  if (response.status === 409) return { ok: false, problem: "It was already decided somewhere else." }
  if (!response.ok) return await failed(response)
  return { ok: true }
}
