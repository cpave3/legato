import { apiFetch } from "./api"
import type { AwayOn, BoardCard, BoardColumn, BoardWorker } from "./board-types"

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

// How an away task's state as last heard is shown: the pill on its row in
// the Away lane and the strip in its task panel.
export function awayLook(away: AwayOn): AwayLook {
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
