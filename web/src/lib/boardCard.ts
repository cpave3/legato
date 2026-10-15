import type { BoardCard } from "./board-types"
import { agentKindLabel } from "./agentKind"

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
  const kind = card.agent_kind ? agentKindLabel(card.agent_kind) : ""
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
      return { glyph: "○", label: "Idle", time: 0, tint: "bg-lw-idle-soft", tone: "text-lw-text-2" }
  }
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
