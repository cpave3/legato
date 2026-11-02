import { apiFetch } from "./api"

// A clone Legato made for an agent in its data folder, which later agents
// can work in again: the next branch of a stack, in the same folder.
export interface JobClone {
  id: number
  // owner/name on GitHub.
  repo: string
  // The folder on the server.
  path: string
  // The branch checked out now; empty when detached.
  branch: string
  // The tasks that worked in it, the one it was made for first.
  task_ids: string[]
  // An agent is working in it now.
  busy: boolean
  last_used_at: string
  // Branch tips that Legato's copy of GitHub lacks.
  unpushed: number
  // Set while it is kept only for unpushed work.
  remove_after?: string
  // Why Legato could not read it.
  problem?: string
}

async function ok(response: Response): Promise<Response> {
  if (response.ok) return response
  let message = response.statusText
  try {
    const body = await response.json()
    if (body && typeof body.error === "string") message = body.error
  } catch {
    // The body was not JSON; the status says enough.
  }
  throw new Error(message || `request failed (${response.status})`)
}

// The live job clones, the most recently used first. A server that keeps
// none, or an older one, lists none.
export async function listJobClones(baseUrl: string): Promise<JobClone[]> {
  const response = await apiFetch(baseUrl, "/api/job-clones")
  if (!response.ok) return []
  const body = await response.json()
  return Array.isArray(body?.clones) ? body.clones : []
}

// Removes a clone now, with whatever is unpushed in it.
export async function removeJobClone(baseUrl: string, id: number): Promise<void> {
  await ok(await apiFetch(baseUrl, `/api/job-clones/${id}`, { method: "DELETE" }))
}

// One choice of the spawn dialog's Clone list: a new clone (id 0, no clone)
// or one that exists.
export interface CloneChoice {
  id: number
  label: string
  clone?: JobClone
}

// The Clone list for a repository: the task's own clones first, then
// "New clone" unless the task has one (its next agent goes back to it: one
// clone of a repository per task) or Legato no longer offers the repository,
// then the other clones, the most recently used first.
export function cloneChoices(clones: JobClone[], repo: string, taskId: string, canMakeNew: boolean): CloneChoice[] {
  const ofRepo = clones.filter((clone) => clone.repo === repo)
  const own = taskId ? ofRepo.filter((clone) => clone.task_ids.includes(taskId)) : []
  const others = ofRepo.filter((clone) => !own.includes(clone))
  const label = (clone: JobClone, mine: boolean) =>
    `${clone.branch || "detached"}${mine ? " · this task's" : ""}${clone.busy ? " · agent working" : ""}`
  return [
    ...own.map((clone) => ({ id: clone.id, label: label(clone, true), clone })),
    ...(own.length === 0 && canMakeNew ? [{ id: 0, label: "New clone" }] : []),
    ...others.map((clone) => ({ id: clone.id, label: label(clone, false), clone })),
  ]
}
