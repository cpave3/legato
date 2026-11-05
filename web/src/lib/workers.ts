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

export async function listMains(baseUrl: string): Promise<PairedMain[]> {
  const res = await apiFetch(baseUrl, "/api/settings/mains")
  if (!res.ok) throw await failure(res)
  return (await res.json()).mains ?? []
}

export async function unpairMain(baseUrl: string, instanceId: string): Promise<void> {
  const res = await apiFetch(baseUrl, `/api/settings/mains/${encodeURIComponent(instanceId)}`, { method: "DELETE" })
  if (!res.ok) throw await failure(res)
}
