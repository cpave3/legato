import { apiFetch } from "./api"
import type { ReviewLoopAction, ReviewRunLayer, ReviewStewardView } from "./workflow"
import { ago } from "./workflowView"

// The Shipping page's list (GET /api/shipping, LEG-706): every review loop
// Legato looks after, whichever caller opened it. internal/service
// review_shipping.go says what each field holds.

export type ShippingPhase = "authoring" | "reviewing" | "changes_requested" | "approved" | "watching" | "held" | "stuck" | "waiting_for_merge" | "ended"

export type ShippingLayer = ReviewRunLayer & { failing_checks?: string[] }

export type ShippingLoop = {
  loop_id: string
  kind: "run" | "child_run" | "handoff"
  caller: string
  caller_title?: string
  run_id?: string
  step?: string
  link: string
  task?: { id: string; title?: string }
  repo: string
  layers: ShippingLayer[]
  phase: ShippingPhase
  summary: string
  reason?: string
  pass: number
  max_passes: number
  checks?: "passing" | "failing" | "pending"
  findings?: { total: number; blocking: number; nits: number; open: number }
  steward?: ReviewStewardView
  approval_stands?: string
  author?: { task: string; title?: string; state: string }
  reviewer?: string
  reviewer_model?: string
  actions?: ReviewLoopAction[]
  needs_you: boolean
  open: boolean
  outcome?: string
  updated_at?: string
  closed_at?: string
}

export const fetchShipping = (baseUrl: string) => apiFetch(baseUrl, "/api/shipping").then(async response => {
  if (!response.ok) throw new Error(await response.text())
  return response.json() as Promise<{ loops: ShippingLoop[] }>
})

export type ShippingFilter = "needs" | "active" | "ended"

// inFilter says whether a loop is shown under a filter: Needs you (held,
// stuck, failing after approval, an author waiting on the operator), Active
// (open, or approved and waiting for the merge), Recently ended.
export function inFilter(loop: ShippingLoop, filter: ShippingFilter): boolean {
  const active = loop.open || loop.phase === "waiting_for_merge"
  switch (filter) {
    case "needs": return active && loop.needs_you
    case "active": return active
    case "ended": return !active
  }
}

// phaseWords says where a loop stands, in a few words, and, for a loop
// that keeps its pull requests drafts until approved (LEG-712), what
// GitHub shows: "Reviewing · pass 2 · draft", "Approved · ready for review".
export function phaseWords(loop: ShippingLoop): string {
  const words = loopPhaseWords(loop)
  const shown = githubWords(loop)
  return shown ? `${words} · ${shown}` : words
}

// githubWords says what GitHub shows of a loop Legato keeps drafts: draft
// while it is under review, ready for review once approved; "" for any
// other loop, or when GitHub shows something else.
function githubWords(loop: ShippingLoop): string {
  const open = loop.layers.filter(layer => !layer.state || layer.state === "open")
  if (open.length === 0 || !loop.layers.some(layer => layer.drafted_by_legato)) return ""
  switch (loop.phase) {
    case "authoring":
    case "reviewing":
    case "changes_requested":
      return open.every(layer => layer.draft) ? "draft" : ""
    case "approved":
    case "watching":
    case "waiting_for_merge":
      return open.every(layer => !layer.draft) ? "ready for review" : ""
  }
  return ""
}

// draftProblemWords says why a loop could not turn its pull requests into
// drafts; "" when nothing stood in the way.
export function draftProblemWords(loop: ShippingLoop): string {
  const problems = [...new Set(loop.layers.map(layer => layer.draft_problem).filter(Boolean))]
  return problems.length > 0 ? `Not a draft: ${problems.join("; ")}` : ""
}

function loopPhaseWords(loop: ShippingLoop): string {
  switch (loop.phase) {
    case "authoring": return "Author working"
    case "reviewing": return loop.max_passes > 0 ? `Reviewing · pass ${Math.max(loop.pass, 1)} of ${loop.max_passes}` : `Reviewing · pass ${Math.max(loop.pass, 1)}`
    case "changes_requested": return loop.findings?.open ? `Changes requested · ${loop.findings.open} blocking` : "Changes requested"
    case "approved": return "Approved"
    case "watching": return "Steward watching"
    case "held": return "Steward held"
    case "stuck":
      if (loop.steward && loop.steward.max_reentries > 0 && loop.steward.reentries >= loop.steward.max_reentries) return "Stuck at the re-entry limit"
      return loop.summary || "Stuck"
    case "waiting_for_merge": return "Approved · waiting for the merge"
    case "ended": return loop.summary || "Ended"
  }
}

const mergeWords: Record<string, string> = { CONFLICTING: "Conflicts", BEHIND: "Behind its base" }

// ciWords says how a loop's checks and mergeability stand: "1 check
// failing: go · Also failing on its base: docs · Behind its base". Empty
// before the checks were read.
export function ciWords(loop: ShippingLoop): string {
  const failing = loop.layers.flatMap(layer => layer.failing_checks ?? [])
  const parts: string[] = []
  if (failing.length > 0) parts.push(`${failing.length} ${failing.length === 1 ? "check" : "checks"} failing: ${failing.join(", ")}`)
  else if (loop.checks === "pending") parts.push("Checks running")
  else if (loop.checks === "passing") parts.push("Checks passing")
  const onBase = [...new Set(loop.layers.flatMap(layer => layer.checks_on_base ?? []))]
  if (onBase.length > 0) parts.push(`Also failing on its base: ${onBase.join(", ")}`)
  const merge = [...new Set(loop.layers.filter(layer => !layer.state || layer.state === "open").map(layer => mergeWords[layer.mergeable ?? ""]).filter(Boolean))]
  parts.push(...merge)
  return parts.join(" · ")
}

// ciTrouble says the checks fail or a layer cannot merge as it is.
export function ciTrouble(loop: ShippingLoop): boolean {
  return loop.checks === "failing" || loop.layers.some(layer => layer.mergeable === "CONFLICTING" || layer.mergeable === "BEHIND")
}

const wakeWords: Record<string, string> = {
  pushed: "a push", disputed: "a disputed finding", merged: "a merge", closed: "a close", idle: "idleness", ended: "End",
}

// stewardWords says what the steward last did and when: "Last woke: a push
// · 5m ago". Empty when no steward watches or it never woke.
export function stewardWords(loop: ShippingLoop, now: number): string {
  const steward = loop.steward
  if (!steward?.last_wake) return ""
  const wake = wakeWords[steward.last_wake] ?? steward.last_wake
  if (!steward.last_wake_at) return `Last woke: ${wake}`
  const when = ago(steward.last_wake_at, now)
  return `Last woke: ${wake} · ${when === "now" ? "just now" : `${when} ago`}`
}

const authorWords: Record<string, string> = {
  working: "working", background: "working in the background", waiting: "waiting for you", idle: "idle", error: "error", stopped: "not running",
}

export function authorState(state: string): string {
  return authorWords[state] ?? state
}

const outcomeWords: Record<string, string> = {
  merged: "Merged", closed: "Closed, not merged", ended: "Ended", idle: "Ended idle", approved: "Approved", stuck: "Ended while stuck", cancelled: "Cancelled",
}

export function outcomeWord(outcome?: string): string {
  return outcomeWords[outcome ?? ""] ?? "Ended"
}
