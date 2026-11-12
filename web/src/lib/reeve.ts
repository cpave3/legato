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
  // The engram the current session's agent runs as, absent without one.
  engram?: string
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
  // auto: it ran with no approval asked.
  auto?: boolean
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
  const lines = [`The operator asks you about ${ask.what}.`]
  for (const [label, id] of Object.entries(ask.ids ?? {})) {
    if (id) lines.push(`- ${label}: ${id}`)
  }
  const error = ask.error?.split("\n").find((line) => line.trim())?.trim()
  if (error) lines.push(`- Error: ${error}`)
  lines.push("", "Find out what is wrong and what to do about it. Read first; any change goes to the operator as a change set.")
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

// The Reeve's engram (GET /api/reeve/engram): its seed, memories and open
// threads, in a store of its own.
export interface ReeveMemory {
  id: string
  kind: string
  scope: string
  source: string
  created: string
  tags?: string[]
  body: string
}

export interface ReeveThread {
  id: string
  title: string
  scope: string
  opened: string
  body?: string
}

export interface ReeveEngram {
  enabled: boolean
  error?: string
  name: string
  store: string
  // False until the Reeve's first session issues it.
  exists: boolean
  seed: {
    text: string
    // None of Legato's built-in seeds.
    edited: boolean
    // This version's built-in seed.
    template: string
    // The built-in seed changed since the seed was written.
    newer: boolean
  }
  memories: ReeveMemory[]
  threads: ReeveThread[]
}

export async function fetchReeveEngram(baseUrl: string): Promise<ReeveEngram> {
  const response = await apiFetch(baseUrl, "/api/reeve/engram")
  if (!response.ok) throw await failed(response)
  return response.json()
}

// saveReeveSeed asks for a signed approval of a new seed (one of the
// Reeve's own rules); the seed changes once it is signed.
export const saveReeveSeed = (baseUrl: string, seed: string) =>
  post(baseUrl, "/api/reeve/engram/seed", { seed }, "PUT") as Promise<{ status: string; approval_id: string }>
export const removeReeveMemory = (baseUrl: string, id: string) => post(baseUrl, `/api/reeve/engram/memories/${encodeURIComponent(id)}`, undefined, "DELETE")
export const removeReeveThread = (baseUrl: string, id: string) => post(baseUrl, `/api/reeve/engram/threads/${encodeURIComponent(id)}`, undefined, "DELETE")

// A change set that ran with no approval asked (the auto level), and what
// allowed it ("auto: reeve.levels.change", "auto: rule …").
export interface AutoRun {
  id: string
  approval_id: string
  task_id: string
  reason: string
  state: string
  by: string
  at: string
}

// What the Reeve runs without asking (GET /api/reeve/auto): the class
// levels, host auto, the rules at auto, what config.yaml pins of them, and
// the newest sets that ran so.
export interface ReeveAuto {
  on: boolean
  change: string
  risky: string
  host_auto: boolean
  rules: string[]
  pinned: string[]
  recent: AutoRun[]
}

export async function fetchReeveAuto(baseUrl: string): Promise<ReeveAuto> {
  const response = await apiFetch(baseUrl, "/api/reeve/auto")
  if (!response.ok) throw await failed(response)
  return await response.json() as ReeveAuto
}

// turnReeveAutoOff is the off switch: everything at auto back to approve.
export const turnReeveAutoOff = (baseUrl: string) =>
  post(baseUrl, "/api/reeve/auto/off") as Promise<{ changed: string[]; pinned: string[] }>
