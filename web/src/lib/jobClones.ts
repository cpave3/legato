import { apiFetch } from "./api"
import { z } from "zod"

// A clone Legato made for an agent in its data folder, which later agents
// can work in again: the next branch of a stack, in the same folder.
const jobCloneSchema = z.object({
  id: z.number().int().positive(),
  // owner/name on GitHub.
  repo: z.string(),
  // The folder on the server.
  path: z.string(),
  // The branch checked out now; empty when detached.
  branch: z.string(),
  // The tasks that worked in it, the one it was made for first.
  task_ids: z.array(z.string()),
  // An agent is working in it now.
  busy: z.boolean(),
  last_used_at: z.string(),
  // Branch tips that Legato's copy of GitHub lacks.
  unpushed: z.number().int().nonnegative(),
  // Set while it is kept only for unpushed work.
  remove_after: z.string().optional(),
  // Why Legato could not read it.
  problem: z.string().optional(),
})

export type JobClone = z.infer<typeof jobCloneSchema>

const jobClonesResponseSchema = z.object({ clones: z.array(jobCloneSchema) })

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

// The live job clones, the most recently used first.
export async function listJobClones(baseUrl: string): Promise<JobClone[]> {
  const response = await ok(await apiFetch(baseUrl, "/api/job-clones"))
  const body: unknown = await response.json().catch(() => { throw new Error("Invalid job clones response: expected JSON") })
  const result = jobClonesResponseSchema.safeParse(body)
  if (!result.success) throw new Error(`Invalid job clones response: ${z.prettifyError(result.error)}`)
  return result.data.clones
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
