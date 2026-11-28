import { awayStrip, type AwayStrip } from "./away"
import type { BoardCard } from "./board-types"
import { isMilestone, needsYouStrip, needsYouStrips, priorityTone, type NeedsYouStrip } from "./boardCard"

// What frames a board card, apart from how it is drawn: the strips at its
// foot and its steady edge. See components/board/BoardCard.tsx.

export type CardEdge = AwayStrip["edge"]

export interface CardFrame {
  // An away task's state is on its worker: one strip says it.
  away: AwayStrip | null
  // What waits on the operator, drawn above what runs.
  needs: NeedsYouStrip[]
  // Whether the agent's own strip shows: it does while an agent runs,
  // unless what waits stands in for it while the agent only waits or idles.
  agentStrip: boolean
  edge: CardEdge
}

export function cardFrame(card: BoardCard): CardFrame {
  if (card.away) {
    const away = awayStrip(card.away)
    return { away, needs: [], agentStrip: false, edge: away.edge }
  }
  const needs = needsYouStrips(card)
  const quietAgent = ["waiting", "idle", ""].includes(card.agent_state)
  const standsIn = needsYouStrip(card) !== null && quietAgent
  return { away: null, needs, agentStrip: card.agent_active && !standsIn, edge: agentEdge(card, needs.length > 0) }
}

// A card in error has a red edge; one that needs the operator, or whose
// agent waits, an amber one.
function agentEdge(card: BoardCard, needsYou: boolean): CardEdge {
  if (card.agent_active && card.agent_state === "error") return "error"
  if (needsYou || (card.agent_active && card.agent_state === "waiting")) return "needs"
  return ""
}

export interface MetaPart {
  key: string
  text: string
  className?: string
}

// cardMetaParts is the line under a card's title: what the task is, then
// how it is going.
export function cardMetaParts(card: BoardCard, showWorkspace: boolean): MetaPart[] {
  return [...taskParts(card, showWorkspace), ...progressParts(card)]
}

function taskParts(card: BoardCard, showWorkspace: boolean): MetaPart[] {
  const parts: MetaPart[] = []
  if (card.priority) parts.push({ key: "priority", text: card.priority, className: priorityTone(card.priority) })
  // A Milestone is a tag in the header.
  if (card.issue_type && !isMilestone(card)) parts.push({ key: "type", text: card.issue_type })
  // On a worker, a workspace named after the main says what the header does.
  if (showWorkspace && card.workspace_name && card.workspace_name !== card.handoff_from?.main) parts.push({ key: "workspace", text: card.workspace_name })
  return parts
}

function progressParts(card: BoardCard): MetaPart[] {
  const parts: MetaPart[] = []
  const swarm = card.swarm_stats
  if (swarm && swarm.total > 0) parts.push({ key: "swarm", text: `${swarm.done}/${swarm.total} workers`, className: "text-lw-link" })
  if (card.blocked_by && card.blocked_by.length > 0) parts.push({ key: "blocked", text: `Blocked by ${card.blocked_by.join(", ")}`, className: "text-lw-error" })
  if (card.milestone) parts.push({ key: "milestone", text: card.milestone, className: "font-lw-mono" })
  for (const label of card.labels ?? []) parts.push({ key: `label-${label}`, text: label })
  return parts
}
