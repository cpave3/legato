import { apiFetch } from "./api"

// Handoff between Legatos: this server as a main instance pairs with workers
// (Settings › Workers), and as a worker lists the mains paired with it
// (Settings › Main instances). See docs/ai/integrations.md § Main instances
// and workers.

export interface Worker {
  instance_id: string
  address: string
  name: string
  // This main's name on the worker.
  main_name: string
  sandbox: boolean
  handoff_version: number
  paired_at: string
  // When this main last heard from the worker on its connection.
  last_pull_at?: string
  // When it went down, once down past the grace; when it stopped
  // accepting this main (pair again).
  unreachable_since?: string
  refused_at?: string
  // How many of this main's tasks it holds, which Remove warns of.
  away?: number
}

// RemovedWorker is what removing a worker did: the tasks recalled with
// their results, those taken back without them, and whether the worker
// was told to unpair this main (else it is told when next reached).
export interface RemovedWorker {
  worker: string
  recalled: { task_id: string }[]
  taken_back: { task_id: string }[]
  left: boolean
}

export interface WorkerList {
  workers: Worker[]
  // This main's instance ID, and the name it offers for itself.
  instance_id: string
  main_name: string
}

export interface PairedMain {
  name: string
  instance_id: string
  paired_at: string
  last_pull_at?: string
  open_handoffs: number
}

async function failure(res: Response): Promise<Error> {
  if (res.status === 404) return new Error("This server's Legato cannot pair with other Legatos. Update it.")
  const body = await res.json().catch(() => ({}))
  return new Error(body.error ?? `The server answered ${res.status}.`)
}

export async function listWorkers(baseUrl: string): Promise<WorkerList> {
  const res = await apiFetch(baseUrl, "/api/settings/workers")
  if (!res.ok) throw await failure(res)
  return res.json()
}

export async function pairWorker(baseUrl: string, request: { address: string; phrase: string; name: string }): Promise<Worker> {
  const res = await apiFetch(baseUrl, "/api/settings/workers", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  })
  if (!res.ok) throw await failure(res)
  return res.json()
}

// removeWorker recalls what it can of the worker's away tasks, takes back
// the rest, forgets the worker and tells it to unpair this main.
export async function removeWorker(baseUrl: string, instanceId: string): Promise<RemovedWorker> {
  const res = await apiFetch(baseUrl, `/api/settings/workers/${encodeURIComponent(instanceId)}`, { method: "DELETE" })
  if (!res.ok) throw await failure(res)
  return (await res.json()).removed
}

// removedText says what removing a worker did.
export function removedText(removed: RemovedWorker): string {
  const parts = []
  if (removed.recalled.length > 0) parts.push(`${removed.recalled.length} recalled`)
  if (removed.taken_back.length > 0) parts.push(`${removed.taken_back.length} taken back`)
  let text = `Removed ${removed.worker}` + (parts.length > 0 ? `: ${parts.join(", ")}` : "")
  if (!removed.left) text += `. It is told to unpair this main when it can be reached.`
  return text
}

export async function listMains(baseUrl: string): Promise<PairedMain[]> {
  const res = await apiFetch(baseUrl, "/api/settings/mains")
  if (!res.ok) throw await failure(res)
  return (await res.json()).mains ?? []
}

export async function unpairMain(baseUrl: string, instanceId: string): Promise<void> {
  const res = await apiFetch(baseUrl, `/api/settings/mains/${encodeURIComponent(instanceId)}`, { method: "DELETE" })
  if (!res.ok) throw await failure(res)
}

// removeWarning says what removing a worker does to the tasks it holds.
export function removeWarning(name: string, held: number): string {
  const after = `${name} is told to unpair this main, now or when it can next be reached.`
  if (held === 0) return `${name} holds no away tasks. Removing it forgets it here. ${after}`
  return `${name} holds ${held} away ${held === 1 ? "task" : "tasks"}. Removing it recalls what it can, with its results, and takes back the rest without them. ${after}`
}
