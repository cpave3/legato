import type { BoardCard } from "./board-types"
import { agentToolLabel } from "./agentKind"
import { providerLabel } from "./providers"
import { reeveUnanswered } from "./agentStage"
import { reeveName, type ReeveName } from "./reeveName"

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
  // What the agent needs from the operator, first: Working, Your turn,
  // Needs you, Error, Finished.
  label: string
  time: number
  tint: string
  tone: string
  // Whether the strip names the stage (its track and word) after the label:
  // not under Finished, which the label says.
  stage: boolean
  // Why the agent is at its stage, the first part of the strip to be cut.
  reason: string
  // The agent tool, for the strip's tooltip: the strip leaves it out.
  tool: string
}

// The Reeve's violet, for an agent waiting on it.
const reeveTint = "bg-[var(--theme-keyword,#8b5cf6)]/15"
const reeveTone = "text-[var(--theme-keyword,#a78bfa)]"

// secondsSince is how long ago an RFC 3339 time was, 0 for none.
function secondsSince(when: string | undefined): number {
  const at = when ? Date.parse(when) : NaN
  return Number.isNaN(at) ? 0 : Math.max(0, Math.floor((Date.now() - at) / 1000))
}

// cardGlance is the main agent's glance group; a server that sends none is
// read from the agent's state and stage.
function cardGlance(card: BoardCard): string {
  if (card.glance) return card.glance
  if (card.agent_state === "waiting" || card.agent_state === "error") return "needs_you"
  if (card.agent_state === "working" || card.agent_state === "background") return "working"
  return card.stage === "finished" ? "finished" : "your_turn"
}

// What an agent in each state looks like on a card, leading with what it
// needs from the operator (the glance), then its stage and why; the agent
// tool is in the tooltip.
export function agentStripLook(card: BoardCard, reeve: ReeveName = reeveName()): StripLook {
  const tool = card.agent_kind ? agentToolLabel(card.agent_kind, card.agent_engram) : ""
  const reason = card.stage_reason ?? ""
  const quiet = { stage: true, reason, tool }
  switch (card.agent_state) {
    case "working":
      return { glyph: "●", label: "Working", time: card.working_seconds, tint: "bg-lw-live-soft", tone: "text-lw-live", ...quiet }
    case "waiting":
      return { glyph: "◆", label: "Needs you", time: card.waiting_seconds, tint: "bg-lw-needs-soft", tone: "text-lw-needs", stage: true, reason: "", tool }
    case "error":
      return { glyph: "✕", label: "Error", time: card.error_seconds, tint: "bg-lw-error-soft", tone: "text-lw-error", stage: true, reason: "", tool }
    case "background":
      // Waiting on work it started: working, in green, in its own words.
      return { glyph: "◌", label: "Working · background", time: 0, tint: "bg-lw-live-soft", tone: "text-lw-live", ...quiet }
    case "mixed": {
      // Of several agents, those that work, once the one that waits has a
      // strip of its own.
      const working = card.needs_you?.working_agents ?? 0
      if (working > 0) return { glyph: "●", label: `${working} working`, time: card.working_seconds, tint: "bg-lw-live-soft", tone: "text-lw-live", ...quiet }
      return { glyph: "◐", label: "Agents disagree", time: 0, tint: "bg-lw-idle-soft", tone: "text-lw-text-2", ...quiet }
    }
  }
  const spent = card.working_seconds + card.waiting_seconds + card.error_seconds
  if (card.waiting_on === "reeve") {
    // Its turn ended on a question to the Reeve: working on the operator's
    // behalf, until the Reeve has not answered for too long.
    const waited = secondsSince(card.waiting_since)
    if (cardGlance(card) === "needs_you") {
      return { glyph: "◆", label: "Needs you", time: waited, tint: "bg-lw-needs-soft", tone: "text-lw-needs", stage: true, reason: reeveUnanswered(reeve), tool }
    }
    if (cardGlance(card) === "working") {
      return { glyph: "◈", label: `Waiting on ${reeve.mid}`, time: waited, tint: reeveTint, tone: reeveTone, ...quiet }
    }
  }
  switch (cardGlance(card)) {
    case "finished": {
      const note = (card.needs_you?.items?.done ?? 0) > 0 ? "a note for you" : ""
      return { glyph: "✓", label: "Finished", time: spent, tint: "bg-lw-done-soft", tone: "text-lw-done", stage: false, reason: [reason, note].filter(Boolean).join(" · "), tool }
    }
    case "needs_you":
      return { glyph: "◆", label: "Needs you", time: spent, tint: "bg-lw-needs-soft", tone: "text-lw-needs", stage: true, reason: "", tool }
    default:
      return { glyph: "◇", label: "Your turn", time: spent, tint: "bg-lw-idle-soft", tone: "text-lw-text", ...quiet }
  }
}

// What a named slot's chip says of its agent: what it is doing, not its
// stage, since a reviewer slot is Reviewing until its pass is posted:
// "reviewer ● working", "reviewer ◆ needs you", "reviewer ✓ done".
export function slotChipLabel(slot: string, state: string, stage?: string): string {
  if (stage === "finished") return `${slot} ✓ done`
  switch (state) {
    case "working":
    case "background":
      return `${slot} ● working`
    case "waiting":
      return `${slot} ◆ needs you`
    case "error":
      return `${slot} ✕ error`
    default:
      return `${slot} ○ idle`
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

// Where a strip's action goes: the agent's operator items in the rail, an
// approval, a plan.
export type StripAction =
  | { label: string; kind: "items"; taskId: string }
  | { label: string; kind: "resume"; taskId: string }
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

// needsYouStrip is the strip that says why a card's main agent needs the
// operator, and the way there: an approval first (it blocks the agent), then
// the agent's operator items, then a plan to approve; null when nothing
// waits.
export function needsYouStrip(card: BoardCard): NeedsYouStrip | null {
  const needs = card.needs_you
  if (!needs) return null
  if (needs.approvals && needs.approvals.count > 0) {
    const more = needs.approvals.count > 1 ? ` and ${needs.approvals.count - 1} more` : ""
    return { glyph: "◆", text: `Needs you · ${needs.approvals.label}${more}`, action: { label: "Answer", kind: "approval", approvalId: needs.approvals.id } }
  }
  const items = needs.items ?? {}
  // A finished agent's done note is no reason to need the operator: its
  // strip says Finished and that it left a note.
  const finished = card.agent_active && cardGlance(card) === "finished"
  const words = itemWords.filter(([kind]) => (items[kind] ?? 0) > 0 && !(finished && kind === "done")).map(([kind, one, many]) => {
    const n = items[kind] ?? 0
    return `${n} ${n === 1 ? one : many}`
  })
  if (words.length > 0) return { glyph: "◆", text: `Needs you · ${words.join(", ")}`, action: { label: "Open", kind: "items", taskId: card.id } }
  if (needs.plans && needs.plans.count > 0) {
    return { glyph: "◆", text: needs.plans.count === 1 ? "Plan to approve" : `${needs.plans.count} plans to approve`, action: { label: "Review plan", kind: "plan", planId: needs.plans.plan_id } }
  }
  return null
}

const capitalize = (word: string) => word.charAt(0).toUpperCase() + word.slice(1)

// needsYouStrips are every strip of what waits on the operator, top first:
// the main agent's (needsYouStrip), each named slot that waits ("Reviewer
// needs you · question"), one of several agents that waits, and an
// interrupted agent to resume.
export function needsYouStrips(card: BoardCard): NeedsYouStrip[] {
  const needs = card.needs_you
  if (!needs) return []
  const strips: NeedsYouStrip[] = []
  const main = needsYouStrip(card)
  if (main) strips.push(main)
  for (const slot of needs.slots ?? []) {
    const text = `${capitalize(slot.slot)} needs you${slot.label ? ` · ${slot.label}` : ""}`
    strips.push({ glyph: "◆", text, action: slot.approval_id ? { label: "Answer", kind: "approval", approvalId: slot.approval_id } : { label: "Open", kind: "items", taskId: card.id } })
  }
  if (!main && needs.agents && needs.waiting_agents) {
    strips.push({ glyph: "◆", text: `${needs.waiting_agents} of ${needs.agents} agents ${needs.waiting_agents === 1 ? "needs" : "need"} you`, action: { label: "Open", kind: "items", taskId: card.id } })
  }
  if (needs.interrupted?.resumable && !card.agent_active) {
    const kind = needs.interrupted.agent_kind ? agentToolLabel(needs.interrupted.agent_kind, undefined) : ""
    strips.push({ glyph: "‖", text: `Interrupted${kind ? ` · ${kind}` : ""}`, action: { label: "Resume", kind: "resume", taskId: card.id } })
  }
  return strips
}

// Where a card's task comes from, as its header says it: the source by name
// and the ticket's key ("Burnlite · LEG-386", "local · f41p6uf0"); a
// worker's copy adds the main it came from ("← erebus"), an away task the
// worker it is on ("→ vm-1").
export function cardSource(card: Pick<BoardCard, "id" | "provider" | "remote_id" | "handoff_from" | "away">): string {
  const key = card.remote_id || card.id
  let text = card.provider ? `${providerLabel(card.provider)} · ${key}` : `local · ${card.id}`
  if (card.handoff_from) text += ` ← ${card.handoff_from.main}`
  if (card.away) text += ` → ${card.away.worker}`
  return text
}

// A task imported from a Burnlite Milestone.
export function isMilestone(card: Pick<BoardCard, "issue_type">): boolean {
  return card.issue_type === "Milestone"
}
