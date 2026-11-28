import { cloneChoices, type CloneChoice, type JobClone } from "./jobClones"

// The spawn dialog's clone choice, the parts that are worked out: which
// repositories a clone can be of, which is picked first, the clone picked,
// and what the dialog says of it.

// A repository as the settings offer it.
export interface CloneableRepository {
  name: string
  repo: string
  base_branch?: string
}

// A repository a clone can be of. offered is false for one Legato no longer
// offers, of which only clones made before can be worked in.
export interface CloneRepository {
  name: string
  repo: string
  baseBranch: string
  offered: boolean
}

// The repository and base branch a clone starts with.
export interface CloneStart {
  name: string
  baseBranch: string
}

// The first repository that can be cloned: one with a GitHub name.
export function firstCloneable(repositories?: CloneableRepository[] | null): CloneStart {
  const first = (repositories ?? []).find((repository) => repository.repo)
  return { name: first?.name ?? "", baseBranch: first?.base_branch ?? "" }
}

// The task's own clone is where its next agent most likely goes: its
// repository, under the name the settings give it.
export function ownCloneStart(clones: JobClone[], taskId: string, repositories?: CloneableRepository[] | null): CloneStart | undefined {
  const own = taskId ? clones.find((clone) => clone.task_ids.includes(taskId)) : undefined
  if (!own) return undefined
  const preset = (repositories ?? []).find((repository) => repository.repo === own.repo)
  return { name: preset?.name ?? own.repo, baseBranch: preset?.base_branch ?? "" }
}

// The repositories a clone can be of: those offered with a GitHub name,
// then those of clones made before that are no longer offered.
export function cloneRepositoriesOf(freshClones: boolean, repositories: CloneableRepository[] | null | undefined, clones: JobClone[]): CloneRepository[] {
  const offered = freshClones ? (repositories ?? []).filter((repository) => repository.repo) : []
  return [
    ...offered.map((repository) => ({ name: repository.name, repo: repository.repo, baseBranch: repository.base_branch ?? "", offered: true })),
    ...[...new Set(clones.map((clone) => clone.repo))]
      .filter((repo) => !offered.some((repository) => repository.repo === repo))
      .map((repo) => ({ name: repo, repo, baseBranch: "", offered: false })),
  ]
}

// What is picked: the repository named (else the first), the clone choices
// for it, and the one picked (null for the repository's first choice).
export interface PickedClone {
  repository?: CloneRepository
  choices: CloneChoice[]
  choice?: CloneChoice
  // The clone picked, when it is one that exists.
  jobClone?: JobClone
}

export function pickedClone(repositories: CloneRepository[], name: string, clones: JobClone[], cloneId: number | null, taskId: string): PickedClone {
  const repository = repositories.find((candidate) => candidate.name === name) ?? repositories[0]
  const choices = repository ? cloneChoices(clones, repository.repo, taskId, repository.offered) : []
  const choice = choices.find((option) => option.id === cloneId) ?? choices[0]
  return { repository, choices, choice, jobClone: choice?.clone }
}

// What starting in the clone picked does.
export function cloneNote(jobClone: JobClone | undefined, repo: string): string {
  if (!jobClone) return `A new clone of ${repo}. A branch that exists on GitHub is checked out; otherwise it is made from the base branch.`
  if (jobClone.busy) return `An agent is working in this clone, so this one joins it on ${jobClone.branch || "its commit"}. The two can message each other.`
  return `Continues in ${jobClone.path} as it was left, with its build output. A branch named here is made at its current commit, stacked on ${jobClone.branch || "it"}.`
}

// Why the clones are not listed.
export function cloneLoadError(reason: unknown): string {
  return `Could not load clones: ${reason instanceof Error ? reason.message : String(reason)}`
}
