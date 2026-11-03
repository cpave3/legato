import { apiFetch } from "./api"

/** One branch of a stack an agent built with Staccato. */
export type TaskStackBranch = {
  name: string
  parent: string
  base_sha: string
  head_sha?: string
  state: "pushed" | "unpushed" | "missing" | "invalid"
  problem?: string
}

/** The stack of branches in one of a task's job clones, as Legato last read it. */
export type TaskStack = {
  clone_id: number
  repo: string
  root: string
  read_at: string
  /** False once the clone's folder is removed; the record stays. */
  live: boolean
  problem?: string
  importable: boolean
  reason?: string
  document_name?: string
  branches: TaskStackBranch[]
}

export async function fetchTaskStacks(baseUrl: string, taskId: string): Promise<TaskStack[]> {
  const response = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(taskId)}/stacks`)
  if (!response.ok) throw new Error(await response.text())
  const body = (await response.json()) as { stacks: TaskStack[] | null }
  return body.stacks ?? []
}

/** The path of a stack's st graph import document. */
export function taskStackDocumentPath(taskId: string, cloneId: number): string {
  return `/api/tasks/${encodeURIComponent(taskId)}/stacks/${cloneId}/document`
}

/** Each branch with its depth from the stack's root (1 for a branch on the root). */
export function branchDepths(stack: TaskStack): { branch: TaskStackBranch; depth: number }[] {
  const byName = new Map(stack.branches.map((branch) => [branch.name, branch]))
  return stack.branches.map((branch) => {
    let depth = 1
    let parent = byName.get(branch.parent)
    const seen = new Set([branch.name])
    while (parent && !seen.has(parent.name)) {
      seen.add(parent.name)
      depth++
      parent = byName.get(parent.parent)
    }
    return { branch, depth }
  })
}
