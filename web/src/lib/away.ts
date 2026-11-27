import { apiFetch } from "./api"
import type { AwayOn, AwayReview, BoardCard, BoardColumn, BoardWorker } from "./board-types"

// Handing tasks off from this main instance to a worker instance, and the
// tasks away on workers as the web board shows them: out of their columns,
// in an Away lane at the board's end, grouped by worker. See
// docs/ai/web-ui.md § Handing off a task.

// The name of the Away lane among the board's columns.
export const awayLaneName = "Away"

// The board's columns with every away task taken out of the column it left
// and put in an Away lane after them, ordered as the lane's groups are
// drawn. The lane is there once a worker is paired, or while a task is away.
export function withAwayLane(columns: BoardColumn[], workers: BoardWorker[]): BoardColumn[] {
  const away: BoardCard[] = []
  const home = columns.map((column) => {
    const cards = column.cards.filter((card) => {
      if (!card.away) return true
      away.push(card)
      return false
    })
    return cards.length === column.cards.length ? column : { ...column, cards }
  })
  if (workers.length === 0 && away.length === 0) return columns
  const ordered = awayGroups(workers, away).flatMap((group) => group.cards)
  return [...home, { name: awayLaneName, cards: ordered, away: true }]
}

export interface AwayGroup {
  worker: BoardWorker
  cards: BoardCard[]
}

// The Away lane's groups: one per paired worker, in the order the server
// lists them, then one for each worker an away task names that is no longer
// listed.
export function awayGroups(workers: BoardWorker[], cards: BoardCard[]): AwayGroup[] {
  const groups: AwayGroup[] = workers.map((worker) => ({ worker, cards: [] }))
  for (const card of cards) {
    const away = card.away
    if (!away) continue
    let group = groups.find((g) => g.worker.instance_id === away.worker_instance_id)
    if (!group) {
      group = { worker: { instance_id: away.worker_instance_id, name: away.worker || away.worker_instance_id, address: "" }, cards: [] }
      groups.push(group)
    }
    group.cards.push(card)
  }
  return groups
}

export interface AwayLook {
  glyph: string
  label: string
  // The text color of the state, and the tint of its pill.
  tone: string
  tint: string
}

// How an away task is shown: the pill on its row in the Away lane and the
// strip in its task panel. The server's view comes first (the main's own
// view of the worker, then the worker's state and its agent's activity);
// an older server sends only the state.
export function awayLook(away: AwayOn): AwayLook {
  const review = away.review
  if (review && away.view !== "unreachable" && away.view !== "lost" && away.view !== "not_accepted") {
    if (review.phase === "stuck") return { glyph: "◆", label: `Review stuck: ${review.reason ?? ""}`.trim(), tone: "text-lw-needs", tint: "bg-lw-needs-soft" }
    if (away.view === "in_review" || away.view === "done") {
      const label = reviewLabel(review)
      const approved = review.phase === "clean" && !review.checks_pending
      if (label) return { glyph: approved ? "✓" : "◐", label, tone: approved ? "text-lw-done" : "text-lw-live", tint: approved ? "bg-lw-done-soft" : "bg-lw-live-soft" }
    }
  }
  switch (away.view) {
    case "starting":
      return { glyph: "◌", label: "Starting", tone: "text-lw-text-2", tint: "bg-lw-idle-soft" }
    case "working":
      return { glyph: "●", label: "Running", tone: "text-lw-live", tint: "bg-lw-live-soft" }
    case "needs_you":
      return { glyph: "◆", label: "Needs you", tone: "text-lw-needs", tint: "bg-lw-needs-soft" }
    case "idle":
      return { glyph: "○", label: "Idle", tone: "text-lw-text-2", tint: "bg-lw-idle-soft" }
    case "ended":
      return { glyph: "■", label: "Agent ended", tone: "text-lw-text-2", tint: "bg-lw-idle-soft" }
    case "interrupted":
      return { glyph: "‖", label: "Interrupted on worker", tone: "text-lw-needs", tint: "bg-lw-needs-soft" }
    case "done":
      return { glyph: "✓", label: "Done", tone: "text-lw-done", tint: "bg-lw-done-soft" }
    case "failed":
      return { glyph: "✕", label: away.failure ? `Failed: ${away.failure}` : "Failed", tone: "text-lw-error", tint: "bg-lw-error-soft" }
    case "coming_back":
      return { glyph: "↩", label: "Coming back", tone: "text-lw-text-2", tint: "bg-lw-idle-soft" }
    case "unreachable":
      return { glyph: "⚠", label: "Unreachable", tone: "text-lw-error", tint: "bg-lw-error-soft" }
    case "lost":
      return { glyph: "✕", label: "Lost on worker", tone: "text-lw-error", tint: "bg-lw-error-soft" }
    case "not_accepted":
      return { glyph: "⚠", label: "Not accepted", tone: "text-lw-error", tint: "bg-lw-error-soft" }
  }
  switch (away.state) {
    case "pending":
      return { glyph: "◌", label: "Starting", tone: "text-lw-text-2", tint: "bg-lw-idle-soft" }
    case "in_progress":
      return { glyph: "●", label: "Running", tone: "text-lw-live", tint: "bg-lw-live-soft" }
    case "done":
      return { glyph: "✓", label: "Done", tone: "text-lw-done", tint: "bg-lw-done-soft" }
    case "failed":
      return { glyph: "✕", label: away.failure ? `Failed: ${away.failure}` : "Failed", tone: "text-lw-error", tint: "bg-lw-error-soft" }
    case "returning":
      return { glyph: "↩", label: "Coming back", tone: "text-lw-text-2", tint: "bg-lw-idle-soft" }
    default:
      return { glyph: "→", label: "Handed off", tone: "text-lw-text-2", tint: "bg-lw-idle-soft" }
  }
}

export interface AwayStrip {
  glyph: string
  text: string
  // On the right: the review's open counts, else when it was handed off.
  detail?: string
  tone: string
  tint: string
  // The card's steady edge: needs the operator, or cannot go on.
  edge: "needs" | "error" | ""
  // A review pass runs: the strip adds only the stage's track, named by
  // its word, since the text says what the review is doing.
  review?: boolean
}

// The phrase for what a review pass is doing, after "Review pass N".
function reviewPhrase(review: AwayReview): string {
  switch (review.waiting_on) {
    case "author_working": return "author fixing"
    case "author_waiting": return "author waits for an answer"
    case "author_push": return "waiting for a push"
    case "github": return "waiting for GitHub"
    case "start": return "starting the next pass"
    case "reviewer_slot": return "waiting for a reviewer"
  }
  switch (review.phase) {
    case "reviewing": return "reviewing"
    case "waiting_for_reviewer": return "waiting for a reviewer"
    case "waiting_for_author": return "author fixing"
  }
  return ""
}

// The open counts of a review: "3 blocking · 1 comment · 1 check".
export function reviewCounts(review: AwayReview): string {
  const parts: string[] = []
  if (review.blocking_open) parts.push(`${review.blocking_open} blocking`)
  if (review.comments_open) parts.push(`${review.comments_open} ${review.comments_open === 1 ? "comment" : "comments"}`)
  if (review.checks_open) parts.push(`${review.checks_open} ${review.checks_open === 1 ? "check" : "checks"}`)
  return parts.join(" · ")
}

// awayStrip is the strip at the foot of an away task's card: the review
// while one runs, else the away state, in the card's words and glyphs.
export function awayStrip(away: AwayOn): AwayStrip {
  const review = away.review
  const handed = `from ${away.left_column} · ${awayTime(away.handed_off_at)}`
  switch (away.view) {
    case "unreachable":
      return { glyph: "⚠", text: away.worker_unreachable_since ? `${away.worker} unreachable since ${awayTime(away.worker_unreachable_since)}` : `${away.worker} unreachable`, tone: "text-lw-error", tint: "bg-lw-error-soft", edge: "error" }
    case "lost":
      return { glyph: "✕", text: away.lost ? `Lost on ${away.worker}: ${away.lost}` : `Lost on ${away.worker}`, tone: "text-lw-error", tint: "bg-lw-error-soft", edge: "error" }
    case "not_accepted":
      return { glyph: "⚠", text: `${away.worker} no longer accepts this main`, tone: "text-lw-error", tint: "bg-lw-error-soft", edge: "error" }
  }
  if (review) {
    if (review.phase === "stuck" && away.view === "needs_you") {
      return { glyph: "◆", text: `Review stuck after pass ${review.pass}${review.reason ? `: ${review.reason}` : ""}`, tone: "text-lw-needs", tint: "bg-lw-needs-soft", edge: "needs" }
    }
    if (away.view === "in_review" || away.view === "done") {
      if (review.phase === "clean") {
        if (review.checks_pending) return { glyph: "◐", text: `Review approved · waiting for ${review.checks_pending} ${review.checks_pending === 1 ? "check" : "checks"}`, tone: "text-lw-live", tint: "bg-lw-live-soft", edge: "" }
        return { glyph: "✓", text: "Review done · approved", tone: "text-lw-done", tint: "bg-lw-done-soft", edge: "" }
      }
      const phrase = reviewPhrase(review)
      if (phrase) {
        const pass = review.phase === "waiting_for_reviewer" || review.waiting_on === "start" ? review.pass + 1 : review.pass
        return { glyph: "◐", text: `Review pass ${pass} · ${phrase}`, detail: reviewCounts(review) || undefined, tone: "text-lw-live", tint: "bg-lw-live-soft", edge: "", review: true }
      }
    }
  }
  // An idle task reads like the others: where it is, then its stage.
  if (away.view === "idle") return { glyph: "⇢", text: `On ${away.worker}`, detail: handed, tone: "text-lw-text-2", tint: "bg-lw-idle-soft", edge: "", review: false }
  const look = awayLook(away)
  const edge = away.view === "needs_you" || away.view === "interrupted" ? "needs" : away.view === "failed" || (!away.view && away.state === "failed") ? "error" : ""
  return { glyph: look.glyph, text: look.label, detail: handed, tone: look.tone, tint: look.tint, edge, review: false }
}

// What a review block says in a few words, as the worker's summary does.
export function reviewLabel(review: AwayReview): string {
  const parts: string[] = []
  if (review.blocking_open) parts.push(`${review.blocking_open} blocking`)
  if (review.comments_open) parts.push(`${review.comments_open} ${review.comments_open === 1 ? "comment" : "comments"}`)
  if (review.checks_open) parts.push(`${review.checks_open} ${review.checks_open === 1 ? "check" : "checks"}`)
  const counts = parts.join(", ")
  // What the loop waits on, with what is still open beside it.
  const waiting = {
    author_working: "Waiting for the author · working",
    author_waiting: "Waiting for the author · it waits for an answer",
    author_push: "Waiting for the author to push",
    github: "Waiting for GitHub",
    start: "Starting the next pass",
  }[review.waiting_on ?? ""]
  if (waiting) return counts ? `${waiting} · ${counts}` : waiting
  switch (review.phase) {
    case "reviewing":
      return `Reviewing · pass ${review.pass}`
    case "waiting_for_reviewer":
      return `Waiting for a reviewer · pass ${review.pass + 1}`
    case "waiting_for_author":
      return counts ? `Author fixing · ${counts}` : "Author fixing"
    case "clean": {
      let label = review.checks_pending ? `Approved · waiting for ${review.checks_pending} ${review.checks_pending === 1 ? "check" : "checks"}` : "Done · approved"
      if (review.checks_replied) label += ` · ${review.checks_replied} ${review.checks_replied === 1 ? "check" : "checks"} answered by reply`
      return label
    }
  }
  return ""
}

// Why an away task shows what it shows, for its pill's tooltip: why it
// was lost, since when its worker is unreachable, that the worker no longer
// accepts this main.
export function awayDetail(away: AwayOn): string | undefined {
  // A stuck review's reason can be long: the pill cuts it short.
  if (away.review?.phase === "stuck" && away.review.reason && away.view === "needs_you") return `Review stuck: ${away.review.reason}`
  switch (away.view) {
    case "lost":
      return away.lost
    case "unreachable":
      return away.worker_unreachable_since ? `${away.worker} unreachable since ${awayTime(away.worker_unreachable_since)}` : undefined
    case "not_accepted":
      return `${away.worker} no longer accepts this main; pair again`
    case "failed":
      return away.failure
  }
  return undefined
}

export interface WorkerReachLook {
  text: string
  // ok: reachable; quiet: nothing heard yet; bad: unreachable or refusing.
  tone: "ok" | "quiet" | "bad"
}

// How a worker group's header says whether the worker can be reached:
// "pulled 2 min ago", "unreachable since 21:40", or that it no longer
// accepts this main.
export function workerReach(worker: BoardWorker, now = new Date()): WorkerReachLook {
  if (worker.refused_at) return { text: "no longer accepts this main; pair again", tone: "bad" }
  if (worker.unreachable_since) return { text: `unreachable since ${awayTime(worker.unreachable_since, now)}`, tone: "bad" }
  if (!worker.last_pull_at) return { text: "not pulled yet", tone: "quiet" }
  return { text: `pulled ${sinceText(worker.last_pull_at, now)}`, tone: "ok" }
}

function sinceText(iso: string, now: Date): string {
  const seconds = Math.max(0, Math.round((now.getTime() - new Date(iso).getTime()) / 1000))
  if (Number.isNaN(seconds) || seconds < 60) return "just now"
  const minutes = Math.floor(seconds / 60)
  if (minutes < 60) return `${minutes} min ago`
  const hours = Math.floor(minutes / 60)
  if (hours < 48) return `${hours} h ago`
  return `${Math.floor(hours / 24)} days ago`
}

export function purposeLabel(purpose: string): string {
  switch (purpose) {
    case "make-change":
      return "Make a change"
    case "find-out":
      return "Find out"
    default:
      return purpose
  }
}

export function kindLabel(kind: string): string {
  switch (kind) {
    case "new-branch":
      return "A new branch"
    case "pushed-branch":
      return "A pushed branch"
    default:
      return kind
  }
}

// When a handoff happened, as the Away lane shows it: the time today, else
// the day and the time.
export function awayTime(iso: string, now = new Date()): string {
  const date = new Date(iso)
  if (Number.isNaN(date.getTime())) return iso
  const time = date.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" })
  if (date.toDateString() === now.toDateString()) return time
  return `${date.toLocaleDateString(undefined, { day: "numeric", month: "short" })} ${time}`
}

// HandOffRequest is the Hand off form: the worker's instance ID, the
// purpose, where the work starts (a new branch, or a branch already pushed
// to GitHub, named in branch), the repository (owner/name) and the agent
// tool (empty for the worker's default).
export interface HandOffRequest {
  worker: string
  purpose: "make-change" | "find-out"
  kind: "new-branch" | "pushed-branch"
  branch?: string
  repo: string
  agent_kind: string
  account?: string
  // Changes to the worker's review passes for this handoff.
  review?: ReviewOverride
}

// A worker's review passes, as its /api/meta says them: its defaults, and
// the tools, tiers and skills a handoff may choose.
export interface ReviewPassesAdvert {
  defaults: { enabled: boolean; agent: string; model?: string; tier?: string; skill?: string; max_passes: number }
  adapters: { name: string; tiers?: string[] }[]
  skills: string[]
}

// What the Hand off dialog changes of a worker's review passes.
export interface ReviewOverride {
  enabled?: boolean
  agent?: string
  model?: string
  tier?: string
  skill?: string
  max_passes?: number
}

// workerReviewPasses is what a worker says of its review passes, or null
// when it runs none (or is too old to say).
export async function workerReviewPasses(baseUrl: string, workerId: string): Promise<ReviewPassesAdvert | null> {
  try {
    const res = await apiFetch(baseUrl, `/api/settings/workers/${encodeURIComponent(workerId)}/review-passes`)
    if (!res.ok) return null
    const body = await res.json()
    return (body.review_passes as ReviewPassesAdvert | null) ?? null
  } catch {
    return null
  }
}

// handOffTask hands a task to a worker. A refusal throws the server's words:
// an unreachable worker, one without the sandbox, an agent tool it does not
// know, an agent running here.
export async function handOffTask(baseUrl: string, taskId: string, request: HandOffRequest): Promise<AwayOn> {
  const res = await apiFetch(baseUrl, `/api/tasks/${encodeURIComponent(taskId)}/handoff`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(request),
  })
  const body = await res.json().catch(() => ({}))
  if (!res.ok) {
    if (res.status === 404 && !body.error) throw new Error("This server's Legato cannot hand tasks off. Update it.")
    throw new Error(body.error ?? `The server answered ${res.status}.`)
  }
  return body.away
}

// workerHistoryURL is the session history of an away task on its worker,
// filtered to the worker's copy of the task, from the address the handoff
// summary gives; undefined while that is not known.
export function workerHistoryURL(away: Pick<AwayOn, "url" | "worker_task_id">): string | undefined {
  if (!away.url || !away.worker_task_id) return undefined
  try {
    const origin = new URL(away.url).origin
    if (origin === "null") return undefined
    return `${origin}/agents/history?task=${encodeURIComponent(away.worker_task_id)}`
  } catch {
    return undefined
  }
}
