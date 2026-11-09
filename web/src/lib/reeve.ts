import { apiFetch } from "./api"

// One session of the Reeve (store.ReeveSession).
export interface ReeveSession {
  task_id: string
  state: "running" | "suspended" | "ended"
  agent_kind: string
  model?: string
  title: string
  started_by?: string
  created_at: string
  updated_at: string
  active_at: string
}

// The Reeve's current session and its history (GET /api/reeve).
export interface ReeveOverview {
  current?: ReeveSession
  history: ReeveSession[]
  folder: string
  // This instance's name and role (main, worker, main and worker, standalone).
  instance: string
  role: string
  // Approvals the Reeve's sessions wait on.
  to_approve: number
}

// How a session starts: an override of the agent tool and model, a brief.
export interface ReeveStart {
  agent?: string
  model?: string
  brief?: string
}

async function failed(response: Response): Promise<Error> {
  const body = await response.json().catch(() => null) as { error?: string } | null
  return new Error(body?.error || response.statusText)
}

export async function fetchReeve(baseUrl: string): Promise<ReeveOverview> {
  const response = await apiFetch(baseUrl, "/api/reeve")
  if (!response.ok) throw await failed(response)
  return response.json()
}

async function post(baseUrl: string, path: string, body?: unknown, method = "POST"): Promise<unknown> {
  const response = await apiFetch(baseUrl, path, {
    method,
    headers: { "Content-Type": "application/json" },
    body: body === undefined ? undefined : JSON.stringify(body),
  })
  if (!response.ok) throw await failed(response)
  return response.json()
}

export const startReeve = (baseUrl: string, start: ReeveStart) => post(baseUrl, "/api/reeve/sessions", start) as Promise<ReeveSession>
export const endReeve = (baseUrl: string, taskId: string) => post(baseUrl, `/api/reeve/sessions/${encodeURIComponent(taskId)}/end`)
export const resumeReeve = (baseUrl: string, taskId: string, start: ReeveStart = {}) => post(baseUrl, `/api/reeve/sessions/${encodeURIComponent(taskId)}/resume`, start) as Promise<ReeveSession>
export const deleteReeve = (baseUrl: string, taskId: string) => post(baseUrl, `/api/reeve/sessions/${encodeURIComponent(taskId)}`, undefined, "DELETE")

// A change set as the conversation shows it (GET
// /api/reeve/sessions/{task_id}/change-sets).
export interface ChangeSetLine {
  id: string
  reason: string
  state: "pending" | "queued" | "running" | "done" | "failed" | "denied" | "expired" | "restarting"
  calls: number
  ran: number
}

export async function fetchChangeSetLines(baseUrl: string, taskId: string): Promise<ChangeSetLine[]> {
  const response = await apiFetch(baseUrl, `/api/reeve/sessions/${encodeURIComponent(taskId)}/change-sets`)
  if (!response.ok) throw await failed(response)
  const body = await response.json() as { change_sets: ChangeSetLine[] }
  return body.change_sets ?? []
}

// storeReeveSecret stores the value the person pasted for the Reeve's
// request; the Reeve gets only its name.
export async function storeReeveSecret(baseUrl: string, approvalId: string, value: string): Promise<string> {
  const response = await apiFetch(baseUrl, `/api/reeve/secrets/${encodeURIComponent(approvalId)}`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ value }),
  })
  if (!response.ok) throw await failed(response)
  const body = await response.json() as { ref: string }
  return body.ref
}

// An instance whose Reeve this one can message (service.Peer): a worker, a
// main, or one paired in Settings › Reeve.
export interface ReevePeer {
  instance_id: string
  name: string
  via: "worker" | "main" | "pairing" | "prime"
  address?: string
}

export async function listReevePeers(baseUrl: string): Promise<ReevePeer[]> {
  const response = await apiFetch(baseUrl, "/api/reeve/peers")
  if (!response.ok) throw await failed(response)
  return ((await response.json()) as { peers?: ReevePeer[] }).peers ?? []
}

export const pairReevePeer = (baseUrl: string, pairing: { address: string; phrase: string }) => post(baseUrl, "/api/reeve/peers", pairing) as Promise<ReevePeer>
export const removeReevePeer = (baseUrl: string, instanceId: string) => post(baseUrl, `/api/reeve/peers/${encodeURIComponent(instanceId)}`, undefined, "DELETE")

// Another instance's Reeve change set waiting for its person
// (service.InstanceApproval): decided there.
export interface PeerApproval {
  id: string
  title: string
  summary?: string
  signed?: boolean
  verified?: boolean
  created_at: string
  instance_id: string
  instance: string
  // url opens that instance's page on the approval; instance_url its page.
  url: string
  instance_url: string
}

export async function fetchPeerApprovals(baseUrl: string): Promise<PeerApproval[]> {
  const response = await apiFetch(baseUrl, "/api/reeve/peers/approvals")
  if (!response.ok) throw await failed(response)
  return ((await response.json()) as { approvals?: PeerApproval[] }).approvals ?? []
}

// messageReeve sends the running session a message, resuming a suspended one
// (POST /api/reeve/message).
export const messageReeve = (baseUrl: string, text: string) => post(baseUrl, "/api/reeve/message", { text })

// What "Ask the Reeve" is about: what it is, its IDs, and its error line.
export interface ReeveAsk {
  what: string
  ids?: Record<string, string | undefined>
  error?: string
}

// reeveBrief is the brief a session starts with, or the message added to
// the running one.
export function reeveBrief(ask: ReeveAsk): string {
  const lines = [`The person asks you about ${ask.what}.`]
  for (const [label, id] of Object.entries(ask.ids ?? {})) {
    if (id) lines.push(`- ${label}: ${id}`)
  }
  const error = ask.error?.split("\n").find((line) => line.trim())?.trim()
  if (error) lines.push(`- Error: ${error}`)
  lines.push("", "Find out what is wrong and what to do about it. Read first; any change goes to the person as a change set.")
  return lines.join("\n")
}

// A message another instance's Reeve sent (service.PeerFrame).
export interface ReeveMessage {
  instance: string
  role: string
  body: string
}

const peerFrame = /^\[from the Reeve on (.+?) · (.+?)\] ?([\s\S]*)$/

export function parseReeveMessage(text: string): ReeveMessage | null {
  const match = peerFrame.exec(text)
  return match ? { instance: match[1], role: match[2], body: match[3] } : null
}
