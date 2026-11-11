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
    if (review.phase === "stuck") return { glyph: "?", label: `Review stuck: ${review.reason ?? ""}`.trim(), tone: "text-lw-needs", tint: "bg-lw-needs-soft" }
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
      return { glyph: "●", label: "Working", tone: "text-lw-live", tint: "bg-lw-live-soft" }
    case "needs_you":
      return { glyph: "?", label: "Needs you", tone: "text-lw-needs", tint: "bg-lw-needs-soft" }
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
      return { glyph: "●", label: "Working", tone: "text-lw-live", tint: "bg-lw-live-soft" }
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

// What a review block says in a few words, as the worker's summary does.
export function reviewLabel(review: AwayReview): string {
  switch (review.phase) {
    case "reviewing":
      return `Reviewing · pass ${review.pass}`
    case "waiting_for_reviewer":
      return `Waiting for a reviewer · pass ${review.pass + 1}`
    case "waiting_for_author": {
      const parts: string[] = []
      if (review.blocking_open) parts.push(`${review.blocking_open} blocking`)
      if (review.comments_open) parts.push(`${review.comments_open} ${review.comments_open === 1 ? "comment" : "comments"}`)
      if (review.checks_open) parts.push(`${review.checks_open} ${review.checks_open === 1 ? "check" : "checks"}`)
      return parts.length ? `Author fixing · ${parts.join(", ")}` : "Author fixing"
    }
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
