import type { HandOffRequest, ReviewOverride, ReviewPassesAdvert } from "./away"
import { advertCanClone, type Worker, type WorkerAdvert } from "./workers"

// The Hand off form's derivations, kept apart from its markup so each can be
// tested alone. See components/board/HandOffModal.tsx.

// localToolOrder lists the agent tools this Legato drives, its default
// first. A handed-off agent runs unattended, which a plain shell cannot, so
// the shell is never offered.
export function localToolOrder(data: { adapters?: string[]; default?: string }): string[] {
  const adapters = (data.adapters ?? []).filter((adapter) => adapter && adapter !== "shell")
  const first = data.default && data.default !== "shell" ? data.default : adapters[0] ?? ""
  if (!first) return adapters
  return [first, ...adapters.filter((adapter) => adapter !== first)]
}

// chosenWorkerView is what the form shows of the chosen worker: its advert,
// its name, and whether it can clone the repository typed or chosen.
export function chosenWorkerView(workers: Worker[] | null, worker: string, repo: string): { advert: WorkerAdvert | undefined; name: string; repoBlocked: boolean } {
  const chosen = workers?.find((w) => w.instance_id === worker)
  const advert = chosen?.advert
  return { advert, name: chosen?.name ?? "the worker", repoBlocked: repo.trim() !== "" && !advertCanClone(advert, repo) }
}

export interface HandOffDraft {
  worker: string
  purpose: HandOffRequest["purpose"]
  kind: HandOffRequest["kind"]
  repo: string
  branch: string
  tool: string
  // The worker's review passes, when it has them, and this handoff's
  // changes to them.
  advert: ReviewPassesAdvert | null
  review: ReviewOverride
}

// handOffReady says whether the form can be sent: a worker, an
// owner/name repository the worker can clone, and a branch's name when the
// work starts from a pushed branch.
export function handOffReady(draft: Pick<HandOffDraft, "worker" | "repo" | "kind" | "branch">, repoBlocked: boolean): boolean {
  if (draft.worker === "" || repoBlocked) return false
  if (!draft.repo.trim().includes("/")) return false
  return draft.kind !== "pushed-branch" || draft.branch.trim() !== ""
}

// handOffRequest is what the form sends. Review changes go only with a
// handoff that makes a change, to a worker that has review passes, and only
// when the person changed something.
export function handOffRequest(draft: HandOffDraft): HandOffRequest {
  const request: HandOffRequest = { worker: draft.worker, purpose: draft.purpose, kind: draft.kind, repo: draft.repo.trim(), agent_kind: draft.tool }
  if (draft.advert && draft.purpose === "make-change" && Object.keys(draft.review).length > 0) request.review = draft.review
  if (draft.kind === "pushed-branch") request.branch = draft.branch.trim()
  return request
}

// repositoryOptionLabel names a repository by its name and GitHub name, or
// once when they are the same.
export function repositoryOptionLabel(name: string, repo: string): string {
  return name === repo ? repo : `${name} · ${repo}`
}

export interface ReviewFieldValues {
  enabled: boolean
  agent: string
  tiers: string[]
  tier: string
  model: string
  skill: string
  maxPasses: number
}

// reviewFieldValues is what each review-pass field shows: the handoff's
// change, else the worker's own setting. The default tier and model belong
// to the default reviewer, so another reviewer starts with neither.
export function reviewFieldValues(advert: ReviewPassesAdvert, review: ReviewOverride): ReviewFieldValues {
  const d = advert.defaults
  return {
    enabled: review.enabled ?? d.enabled,
    ...reviewerValues(advert, review),
    skill: review.skill ?? d.skill ?? "",
    maxPasses: review.max_passes ?? d.max_passes,
  }
}

// reviewerValues is the reviewer's tool, the tiers it offers, and its tier
// and model.
function reviewerValues(advert: ReviewPassesAdvert, review: ReviewOverride): Pick<ReviewFieldValues, "agent" | "tiers" | "tier" | "model"> {
  const d = advert.defaults
  const agent = review.agent ?? d.agent
  const own: { tier?: string; model?: string } = agent === d.agent ? d : {}
  return {
    agent,
    tiers: advert.adapters.find((a) => a.name === agent)?.tiers ?? [],
    tier: review.tier ?? own.tier ?? "",
    model: review.model ?? own.model ?? "",
  }
}
