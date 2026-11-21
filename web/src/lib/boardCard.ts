import type { BoardCard } from "./board-types"
import { agentToolLabel } from "./agentKind"

export function formatDuration(seconds: number): string {
  if (seconds <= 0) return ""
  const minutes = Math.floor(seconds / 60)
  if (minutes < 1) return "<1m"
  const hours = Math.floor(minutes / 60)
  const remaining = minutes % 60
  if (hours === 0) return `${remaining}m`
  if (remaining === 0) return `${hours}h`
  return `${hours}h ${remaining}m`
}

export interface StripLook {
  glyph: string
  label: string
  time: number
  tint: string
  tone: string
}

// What an agent in each state looks like on a card: the same vocabulary the
// rest of the app uses (running, needs you, error, idle).
export function agentStripLook(card: BoardCard): StripLook {
  const kind = card.agent_kind ? agentToolLabel(card.agent_kind, card.agent_engram) : ""
  switch (card.agent_state) {
    case "working":
      return { glyph: "●", label: kind ? `Running · ${kind}` : "Running", time: card.working_seconds, tint: "bg-lw-live-soft", tone: "text-lw-live" }
    case "waiting":
      return { glyph: "◆", label: "Needs you", time: card.waiting_seconds, tint: "bg-lw-needs-soft", tone: "text-lw-needs" }
    case "error":
      return { glyph: "✕", label: "Error", time: card.error_seconds, tint: "bg-lw-error-soft", tone: "text-lw-error" }
    case "background":
      return { glyph: "◌", label: "Background", time: 0, tint: "bg-lw-idle-soft", tone: "text-lw-text-2" }
    case "mixed":
      return { glyph: "◐", label: "Agents disagree", time: 0, tint: "bg-lw-idle-soft", tone: "text-lw-text-2" }
    default:
      return { glyph: "○", label: kind ? `Idle · ${kind}` : "Idle", time: card.working_seconds + card.waiting_seconds + card.error_seconds, tint: "bg-lw-idle-soft", tone: "text-lw-text-2" }
  }
}

// What a named slot's chip says of its agent: "reviewer · running".
export function slotChipLabel(slot: string, state: string): string {
  const text = state === "working" ? "running" : state === "waiting" ? "needs you" : state === "" ? "idle" : state
  return `${slot} · ${text}`
}

// The color a task's priority is written in: red for high, amber for medium.
export function priorityTone(priority: string): string {
  switch (priority.toLowerCase()) {
    case "highest":
    case "high":
      return "text-lw-error"
    case "medium":
      return "text-lw-needs"
    default:
      return ""
  }
}

// Where a strip's action goes: the agent's operator items in the rail, an
// approval, a plan.
export type StripAction =
  | { label: string; kind: "items"; taskId: string }
  | { label: string; kind: "approval"; approvalId: string }
  | { label: string; kind: "plan"; planId: string }

export interface NeedsYouStrip {
  glyph: string
  text: string
  action: StripAction
}

const itemWords: [keyof NonNullable<NonNullable<BoardCard["needs_you"]>["items"]>, string, string][] = [
  ["question", "question", "questions"],
  ["action", "action", "actions"],
  ["done", "to confirm done", "to confirm done"],
  ["other", "note", "notes"],
]

// needsYouStrip is the strip that says why a card needs the operator, and
// the way there: an approval first (it blocks the agent), then the agent's
// operator items, then a plan to approve; null when nothing waits.
export function needsYouStrip(card: BoardCard): NeedsYouStrip | null {
  const needs = card.needs_you
  if (!needs) return null
  if (needs.approvals && needs.approvals.count > 0) {
    const more = needs.approvals.count > 1 ? ` and ${needs.approvals.count - 1} more` : ""
    return { glyph: "◆", text: `Needs you · ${needs.approvals.label}${more}`, action: { label: "Answer", kind: "approval", approvalId: needs.approvals.id } }
  }
  const items = needs.items ?? {}
  const words = itemWords.filter(([kind]) => (items[kind] ?? 0) > 0).map(([kind, one, many]) => {
    const n = items[kind] ?? 0
    return `${n} ${n === 1 ? one : many}`
  })
  if (words.length > 0) return { glyph: "◆", text: `Needs you · ${words.join(", ")}`, action: { label: "Open", kind: "items", taskId: card.id } }
  if (needs.plans && needs.plans.count > 0) {
    return { glyph: "◆", text: needs.plans.count === 1 ? "Plan to approve" : `${needs.plans.count} plans to approve`, action: { label: "Review plan", kind: "plan", planId: needs.plans.plan_id } }
  }
  return null
}
