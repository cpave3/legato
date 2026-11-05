import { apiFetch } from "./api"
import { getToken } from "./auth"
import { shellWorkers } from "./mobileShell"

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

// Add a worker from a server this app already knows: the Legato app's other
// servers (only the app holds their tokens, so it opens the request and
// confirms natively), or in a browser the web UI's own server list. Either
// way the result is the worker's address and a phrase for a main, which
// Add a worker then takes as if they had been typed.

export interface WorkerCandidate {
  // The app's ID for the server, or in a browser its address.
  key: string
  name: string
  url: string
}

export interface WorkerCandidates {
  from: "app" | "browser"
  list: () => Promise<WorkerCandidate[]>
  // null when the person said no.
  open: (candidate: WorkerCandidate, main: string) => Promise<{ address: string; phrase: string } | null>
}

export function workerConfirmText(server: string, main: string): string {
  return `Make ${server} a worker of ${main}? ${main} will be able to hand tasks to ${server}, which runs their agents there.`
}

function sameServer(a: string, b: string): boolean {
  return a.replace(/\/+$/, "") === b.replace(/\/+$/, "")
}

function readBrowserServers(): { name: string; url: string }[] {
  try {
    const list = JSON.parse(localStorage.getItem("legato:servers") ?? "[]")
    return Array.isArray(list) ? list.filter((s) => s && typeof s.url === "string" && typeof s.name === "string") : []
  } catch {
    return []
  }
}

// openMainRequest opens a main-purpose pairing request on a server with the
// token this browser holds for it. A Legato from before handoff ignores the
// purpose and opens a phone's request, which is withdrawn. The words match
// the desktop and Android apps'.
async function openMainRequest(candidate: WorkerCandidate): Promise<{ address: string; phrase: string }> {
  let res: Response
  try {
    res = await apiFetch(candidate.url, "/api/pairing", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ purpose: "main" }),
    })
  } catch (reason) {
    throw new Error(`Could not reach ${candidate.name}: ${reason instanceof Error ? reason.message : String(reason)}`)
  }
  const body = await res.json().catch(() => null)
  if (!res.ok) throw new Error(body?.error ?? `The server answered ${res.status}.`)
  if (body?.purpose !== "main") {
    await apiFetch(candidate.url, "/api/pairing", { method: "DELETE" }).catch(() => undefined)
    throw new Error("This server is too old to be a worker. Update Legato on it, then try again.")
  }
  const phrase = Array.isArray(body.phrase) ? body.phrase.join(" ") : ""
  if (!phrase) throw new Error("The server opened no pairing request.")
  return { address: candidate.url, phrase }
}

/**
 * Where Add a worker finds servers to offer: the Legato app when it can pair
 * workers, else this browser's server list (entries with a token). Never the
 * server in front, baseUrl ("" is the page's own).
 */
export function workerCandidates(baseUrl: string): WorkerCandidates {
  const app = shellWorkers()
  if (app) {
    return {
      from: "app",
      list: () => app.candidates(),
      open: (candidate, main) => app.openPairing(candidate.key, main),
    }
  }
  const front = baseUrl || window.location.origin
  return {
    from: "browser",
    list: async () => readBrowserServers()
      .filter((s) => !sameServer(s.url, front) && getToken(s.url))
      .map((s) => ({ key: s.url, name: s.name, url: s.url })),
    open: async (candidate, main) => {
      if (!window.confirm(workerConfirmText(candidate.name, main))) return null
      return openMainRequest(candidate)
    },
  }
}
